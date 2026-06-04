# Build Spec — xpayment AI Assistant Brain (file-based / GitOps)

> For an AI coding agent. Self-contained: implement the brain from this spec. Where a detail is
> unspecified, make a reasonable choice and mark it as an *Assumption*. Do not pause to ask.

---

## 1. What you are building

A **stateless Go service — "the brain"** — that drafts replies for the xpayment WhatsApp sales copilot. Given a chat id and an incoming customer message, it reads the recent conversation and the lead profile **from Chatwoot**, drafts a reply (answer text + which media to attach + what it learned about the lead) using an LLM, and writes that draft **back to Chatwoot as a private note** for a human to approve and send. It never sends to WhatsApp itself.

The brain's **config and knowledge live as files in a git repo** (the *content repo*); its **conversation and lead state live in Chatwoot**. The brain therefore needs **no database of its own** — it reads a checked-out folder and calls Chatwoot's REST API.

## 2. Non-negotiable decisions

1. **Chatwoot is the single source of truth** for conversations, messages, contacts, status, callbacks. The brain stores none of these.
2. **The brain is stateless and file-backed.** No database. Its persona, knowledge base, media metadata, and prices are files in the content repo. Conversations and the lead profile are in Chatwoot.
3. **Channel-agnostic:** the entry point takes a `chatID`; the same logic serves any channel because everything funnels through Chatwoot.
4. **LLM-as-selector, no vector DB:** the whole knowledge base and the whole media catalog are loaded into a cached prompt; the model returns the *names* (refs) of the media it wants. No retrieval/embeddings.
5. **Prices are single-sourced.** Real numbers live only in `pricing.json`. Knowledge text contains only tokens like `{{price.growth}}`/`{{limit.growth}}`. The model never writes price/limit numerals; Go renders them **after** the model.
6. **Suggest-only in v1:** the brain returns a draft as a Chatwoot private note; a human sends. A confidence threshold gates a later auto-send phase.
7. **Two memory horizons:** the **window** (last ~15 messages of the conversation, read from Chatwoot) and the **profile** (lead facts stored as Chatwoot contact custom attributes, merged additively — never null a known field).
8. **Hexagonal / ports-and-adapters:** the brain talks to the outside through interfaces so the core is testable with no external services.
9. **Integration:** inbound via a Chatwoot **account-level webhook** (`message_created`); write-back via Chatwoot **REST API**. Chatwoot↔WhatsApp is bridged by Evolution (not the brain's concern).

---

## 3. Storage model — the content repo

The content repo is the **source of truth for the brain's config**, and **git is the versioning, audit, rollback, and review layer**. Layout:

```
xpayment-content/
  assistant.json          persona, guardrails, language policy, model settings
  pricing.json            tariffs (price + cashier limit) + non-tariff placeholders
  knowledge/              topic bodies as markdown with YAML front-matter (easy to edit + diff)
    tariffs.ru.md
    tariffs.kk.md
    onboarding.ru.md
    ...
  media.json              the media catalog metadata (the "menu" the model reads)
  media/                  the actual binaries (Git LFS for video; or large files in object storage)
    tariffs/pricing-ru.png
    onboarding/add-cashier.mp4
    ...
  README.md
```

File shapes the loader must parse:

```jsonc
// assistant.json
{ "display_name": "xpayment assistant",
  "persona": "string", "mission": "string",
  "guardrails": ["string", "..."],
  "language_policy": "Reply in the newest message's language; if mixed, prefer Russian.",
  "reply_max_words": 120,
  "model": "claude-...", "temperature": 0.3 }
```
```jsonc
// pricing.json — money as integers (tenge); the ONLY place numbers live
{ "tariffs": {
    "launch": { "price_tenge": 9900,  "cashier_limit": 1 },
    "growth": { "price_tenge": 19900, "cashier_limit": 5 },
    "scale":  { "price_tenge": 49900, "cashier_limit": 20 } },
  "placeholders": { "support.phone": "+7 700 ...", "settlement.days": "1-2" } }
```
```jsonc
// media.json — one entry per file; "file" is a path inside this repo
[ { "ref": "tariffs_infographic_ru", "kind": "image", "file": "media/tariffs/pricing-ru.png",
    "topic": "tariffs", "language": "ru",
    "description": "Infographic of all tiers, price + cashier limit, RU. For pricing / which-plan." } ]
```
```markdown
<!-- knowledge/tariffs.ru.md — body is markdown; metadata is front-matter; tokens NOT numbers -->
---
slug: tariffs
language: ru
summary: pricing and plan selection
---
Тарифы: Launch — {{price.launch}}/мес (до {{limit.launch}} кассы), Growth — {{price.growth}}/мес ...
```

Notes: keep topic **bodies as markdown** (multi-line text in JSON is painful to edit and diff). Put media binaries in git via **Git LFS** for videos to keep the repo lean; alternatively keep large videos in object storage (MinIO/S3) and reference them by URL in `media.json` while small images stay in git — either way the *metadata* stays in git so `git status` reflects catalog changes.

---

## 4. Loading the repo into an in-memory snapshot

On startup, read a local checkout of the content repo into one immutable in-memory snapshot:

```go
type Snapshot struct {
    Config   AssistantConfig          // from assistant.json
    Prices   PriceBook                // from pricing.json (map[tierKey]Tier + placeholders)
    Topics   []Topic                  // from knowledge/*.md (front-matter + body)
    Assets   []Asset                  // from media.json (ref, kind, url, description, topic, language)
    LoadedAt time.Time
    Commit   string                   // git rev of the checkout, for logging
}
```

**Validate on load and fail loudly** (log + refuse to swap in a bad snapshot): every `media.json` entry's `file` must exist on disk; every price/limit token used in any topic must resolve in `pricing.json`; warn if a topic exists in one language but not the other. Resolve each asset's `file` to a public URL (`MEDIA_BASE_URL + file`, or serve `media/` from the brain).

Hold the snapshot behind an atomic pointer so reads are lock-free:
```go
type Content struct{ snap atomic.Pointer[Snapshot] }
func (c *Content) Get() *Snapshot { return c.snap.Load() }
```

---

## 5. Updating the knowledge base, media, and config — the git flow

The content repo is edited with normal git tooling and **every change is visible via `git status` / `git diff`** — this is the intended admin surface (no separate UI required in v1). Implement the brain side (reload); document the human side (below) in the repo README.

How operators change things:
- **Change the persona / guardrails / model settings** → edit `assistant.json`.
- **Change a price** → edit `pricing.json`.
- **Edit an answer** → edit the topic's markdown in `knowledge/`.
- **Add a media file** → drop the binary in `media/` **and** add an entry to `media.json`.
- **Remove a media file** → delete the binary **and** its `media.json` entry.

After any change, `git status` shows exactly what changed and `git diff` shows the content:

```console
$ git status
On branch main
Changes not staged for commit:
  modified:   assistant.json
  modified:   pricing.json
  modified:   media.json
  deleted:    media/onboarding/old-setup.mp4
Untracked files:
  media/onboarding/new-setup.mp4

$ git diff pricing.json
-    "growth": { "price_tenge": 19900, "cashier_limit": 5 },
+    "growth": { "price_tenge": 22900, "cashier_limit": 5 },
```

The config lifecycle maps directly onto git, so build none of it: a **branch or the working copy is a draft**, **merge-to-`main` + push is publish**, **`git revert` is rollback**, **`git log` / `git blame` is the audit** ("who raised the price, when"), and a **PR is review** before changes go live.

### Reload-on-change (implement this)

The brain reloads when the content repo changes. Implement a **GitHub push webhook**: on push to `main`, the brain runs `git pull` in the checkout, builds a **new** `Snapshot`, validates it, and only then atomically swaps the pointer; if validation fails, it keeps the old snapshot and logs the error. (Also support a `--reload` signal and a poll fallback.) Provide a **Playground / CLI** mode that builds a snapshot from the **working copy — including uncommitted edits** — so a change can be tested before it is committed.

---

## 6. Runtime flow — `HandleMessage`

```go
// chatID lets the brain fetch context from Chatwoot; the brain holds no conversation state.
func (s *Service) HandleMessage(ctx context.Context, chatID string, inbound Message) (Draft, error)
```

Steps:
1. **Read context from Chatwoot by `chatID`** (context-on-read): the **window** (last ~15 messages / 48h) and the contact's **custom attributes** (the existing profile). First-message vs. mid-conversation is **not** a branch — it's just how much comes back. (Fetch attributes via the API, not the webhook payload.)
2. **Build the prompt** from the current `Snapshot`:
   - **Cached system prompt** = `[A]` FRAME (the JSON output contract + rules) · `[B]` identity (`Config.Persona`) · `[C]` guardrails · `[D]` knowledge base (all topic bodies, both languages, tokens intact) · `[E]` media catalog (every asset as `ref · kind · description`).
   - **Dynamic messages** = the profile, the window (as turns), and the current message.
3. **Call the LLM** (`Config.Model`, `temperature`) requiring **structured output**:
   ```json
   { "reply_text": "uses {{price.*}}/{{limit.*}} tokens, never numerals",
     "reply_language": "ru|kk", "asset_refs": ["refs from catalog; [] if none"],
     "profile_patch": { "newly-confident fields only": "..." },
     "suggested_callback": { "due_at": "string|null", "note": "string" },
     "suggested_status": { "stage": "string" },
     "confidence": 0.0, "escalate": false, "escalation_reason": "string" }
   ```
4. **Post-process** (Go does what the model is never trusted with):
   - **Parse defensively** (strip fences, validate shape); on failure → post a "couldn't draft" note and stop.
   - **Escalate gate:** if `escalate`, post a "not covered by KB" note and stop.
   - **Validate + resolve `asset_refs`** against `Snapshot.Assets`: drop unknown refs (log), cap at 3, resolve to `{kind,url}`.
   - **Inject prices:** replace `{{price.*}}`/`{{limit.*}}` from `Snapshot.Prices` in `reply_language`; if any token is unknown or survives → post a "check pricing" note and stop.
   - **Additive-merge `profile_patch`** onto the contact's attributes (drop the `stage` key — it's status; never null a known field).
   - **Apply status** (`suggested_status.stage`) as a Chatwoot label.
   - **Outcome:** post the draft as a **private note** (v1); or if `confidence >= threshold` and all checks passed, call Chatwoot's send API (later phase).

### Ports (implement these interfaces; mock them in tests)

```go
type ContentSource interface{ Get() *Snapshot }                                   // §4
type ChatwootReader interface {
    RecentMessages(ctx context.Context, chatID string, n int) ([]Message, error)
    ContactAttributes(ctx context.Context, chatID string) (map[string]any, error)
}
type Drafter interface { Draft(ctx context.Context, system string, window []Message, msg Message) (string, error) }
type Prices  interface { Render(text, lang string) (string, error) }              // from Snapshot.Prices
type Catalog interface { Resolve(refs []string) ([]Media, error) }                // from Snapshot.Assets
type ChatwootWriter interface {
    PostPrivateNote(ctx context.Context, chatID, body string) error
    MergeContactAttributes(ctx context.Context, chatID string, attrs map[string]any) error
    SetLabels(ctx context.Context, chatID string, labels []string) error
    SendOutgoing(ctx context.Context, chatID, text string, media []Media) error   // later phase
}
```
Note: each `ChatwootWriter` method maps to one or more REST calls; JSON numbers in `profile_patch` arrive as `float64` — coerce before writing.

---

## 7. Chatwoot integration

- **Inbound:** expose an HTTP endpoint for Chatwoot's **account-level webhook** (`message_created`); verify the signature; **filter** to *incoming customer* messages on the WhatsApp inbox (ignore agent replies and the brain's own private notes → no loops).
- **Read context:** Chatwoot REST API for the window and the contact custom attributes.
- **Write back:** post a private note (`private:true`), merge contact custom attributes, set the status label.
- **Out of scope for the brain:** the Evolution↔Chatwoot↔WhatsApp transport. Treat any uncertain Chatwoot/Evolution behavior as **"verify, don't assume"** and note it.

---

## 8. Conventions and deliverables

- The **JSON contract and the prompt-assembly order are code-owned**; the **persona, KB, prices, and media are file-owned** (edited in the content repo).
- **Never trust the model for facts:** validate `asset_refs` against the catalog and price tokens against the PriceBook; the model only chooses words, picks refs, and extracts profile facts.
- Money as integer; prices only from `pricing.json`. Bilingual KK/RU throughout.
- Keep a short **Open Questions** note for unresolved items (exact window size, auto-send threshold, KK/RU authoring coverage).

**Deliver:** the Go service with the ports + adapters; the content loader + validator + atomic reloader; the GitHub push-webhook reload handler; the prompt builder; the post-processor; the Chatwoot webhook handler and REST client; and a Playground/CLI that runs `HandleMessage` against the working copy with mockable LLM + Chatwoot for tests. No database.
