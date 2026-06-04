# 02 · The Assistant Brain at Runtime

This file describes what the brain *does* on each message. Decisions, architecture, and ownership are in [README.md](README.md); the authored content (KB, media, prices, persona) and **all DDL** are in [03-content-and-data.md](03-content-and-data.md). This file assumes Decisions 2, 6, 7, 8, 9, 10, 11.

---

## The contract

The brain has a single entry point:

```go
// HandleMessage is the brain's only entry point.
// Channel-agnostic (Decision 4): it takes a chatID and never learns the transport.
// Stateless about conversations (Decision 2): all context is read live from Chatwoot.
// It decides WHAT to respond and returns a Draft. It never sends (Decision 6).
func (b *Brain) HandleMessage(ctx context.Context, chatID ChatID, inbound Message) (Draft, error)
```

`Draft` is the fully post-processed result — final reply text (prices already injected), resolved media (URLs, not refs), and the structured side-data:

```go
type Draft struct {
    ReplyText        string           // prices injected, ready for a human to send
    Media            []ResolvedAsset  // validated + resolved from asset_refs
    ProfilePatch     map[string]any   // newly-confident fields, additively merged
    SuggestedStatus  string           // a stage → applied as a Chatwoot label
    SuggestedCallback *Callback        // optional follow-up (due_at, note)
    Confidence       float64
    Escalate         bool
    EscalationReason string
}
```

The caller (the webhook handler) takes this `Draft` and performs the writes through `ChatwootWriter`. `HandleMessage` itself performs **no writes** other than the reads it needs — which keeps it trivially testable (see the worked example and the test note in [README roadmap, Phase 2](README.md#roadmap)).

---

## Context-on-read

The brain holds **no conversation state** (Decision 2). On every call it uses `chatID` to read what it needs from Chatwoot:

- the **window** — the last ~15 messages (or last 48h) of *this* conversation, via `ChatwootReader.Window`;
- the **profile** — the lead's structured facts, stored as **contact custom attributes**, via `ChatwootReader.Profile` (Decision 9).

A direct consequence: **"first message" vs "mid-conversation" is not a code branch.** It is simply how much Chatwoot returns. A brand-new conversation returns a one-message window and an empty profile; the 50th message returns a 15-message window and a rich profile. The same code path handles both.

There is a **third case** worth calling out: a **returning customer starting a new conversation** has a *short window* (the new conversation only) but a *populated profile* — because the profile lives on the **contact**, not the conversation. The brain therefore "remembers" who they are and what they need even though the message history looks empty. This is the main payoff of storing the profile as a contact attribute rather than a conversation summary.

### What the brain ignores

To avoid loops and noise, `HandleMessage` is only invoked for **incoming customer messages**. The webhook handler drops:
- **outgoing agent messages** (a human reply is not something to draft a reply to), and
- **private notes** (including the brain's own drafts).

> See the webhook-classification caveat in [01-infrastructure.md](01-infrastructure.md#2-the-two-webhook-kinds--do-not-conflate).

---

## Prompt assembly

The prompt is a **cached static prefix** `[A]–[E]` plus a small **dynamic suffix**. Mark the **cache breakpoint after `[E]`** (Decision 7; cache caveat in [01](01-infrastructure.md#prompt-cache-caveat)).

```
┌─────────────────────────── CACHED PREFIX (stable across messages) ───────────────────────────┐
│ [A] FRAME      code-owned, never editable: role · the JSON output contract · the hard rules    │
│ [B] IDENTITY   assistant.json persona — who the bot is, tone                                    │
│ [C] GUARDRAILS assistant.json guardrails — what it must/мustn't do                           │
│ [D] KNOWLEDGE  every topic body, both languages, price tokens left intact                       │
│ [E] MEDIA      the whole catalog as `ref | kind | topic | description` — the selection menu     │
├──────────────────────────────  ⟵ cache breakpoint  ──────────────────────────────────────────┤
│     DYNAMIC    the profile · the window (~15 msgs) · the current message                        │
└────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### The FRAME `[A]` — the hard rules (code-owned)

`[A]` is never editable (the code-owned "skeleton"; the editable "soul" is `assistant.json`, see [03](03-content-and-data.md#file-shapes)). It states the role, embeds the **JSON output contract** (below), and enforces:

- Answer **only** from the knowledge base `[D]`. If the answer isn't there, **escalate** instead of inventing.
- **Never write price or limit numerals.** Leave the `{{price.*}}` / `{{limit.*}}` tokens verbatim; Go fills them after the model (Decision 8).
- Return only `asset_refs` that **exist** in the catalog `[E]`; at most **3**.
- Reply **under ~120 words**.
- Reply in the **customer's language**; if the message is genuinely mixed KK/RU, prefer **Russian** (see the language rule in [03](03-content-and-data.md#open-questions)).

### `[B]`–`[E]` — the editable content

`[B]` identity and `[C]` guardrails come from the snapshot's `assistant.json` (`Snapshot.Config`). `[D]` is every topic body from `knowledge/*.md` in both languages (tokens intact). `[E]` is the whole `media.json` catalog rendered as a compact menu the model picks from. All three are authored material — file shapes and lifecycle in [03-content-and-data.md](03-content-and-data.md).

---

## JSON output contract

The model returns **exactly** this shape (structured output):

```json
{
  "reply_text": "string — uses {{price.*}}/{{limit.*}} tokens, never price numerals",
  "reply_language": "ru | kk",
  "asset_refs": ["string refs from the catalog; [] if none"],
  "profile_patch": { "only newly-confident fields": "..." },
  "suggested_callback": { "due_at": "string|null", "note": "string" },
  "suggested_status": { "stage": "string" },
  "confidence": 0.0,
  "escalate": false,
  "escalation_reason": "string"
}
```

---

## Post-processing pipeline

What the brain does with that JSON, **in order**:

1. **Parse defensively.** Strip any code fences, parse JSON, validate the shape and types. A malformed response is treated like a low-confidence escalation, not a crash.
2. **Escalate gate.** If `escalate` is true, post a flag note (with `escalation_reason`) for a human and **stop** — no media, no auto-send.
3. **Validate + resolve `asset_refs`.** For each ref, look it up in the snapshot's catalog via `Catalog.Resolve`; **drop unknown/hallucinated refs and log them**; cap at 3; keep only resolved `{ref → url, kind}`. Wrong-topic or hallucinated media is a correctness error, so media-selection precision is scored separately in evals ([07](07-testing-and-evals.md#golden-set-eval-harness)).
4. **Inject prices.** Run `reply_text` through `Prices.Render(text, lang)` to replace every `{{...}}` token from the PriceBook in the target language. If any token is unknown or left over, do **not** ship a half-rendered price — post a "check pricing manually" note instead (Decision 8; token grammar and failure path in [03](03-content-and-data.md#pricing--tokens-canonical)).
5. **Additively merge `profile_patch`.** Merge onto the contact's custom attributes: **add new fields, and overwrite a known field when the model is newly confident** (leads change — `urgency` `exploring`→`now`); but **never null/blank a field that was already known** (Decision 9). `never-null` ≠ first-write-wins. Drop the `stage` key if present — that is status, handled next.
6. **Apply status.** Map `suggested_status.stage` to a Chatwoot **label** on the conversation.
7. **Outcome.**
   - **v1 (suggest-only):** post the reply as a **private-note draft**.
   - **Later (auto-send):** if `confidence ≥ threshold` **and** every prior check passed (no dropped refs, no leftover tokens, not escalated), call Chatwoot's outgoing-message API (`ChatwootWriter.SendOutgoing`). Still through Chatwoot, never Evolution (Decision 6).

---

## Ports

The brain depends only on these interfaces (Decision 10). Each is mocked in tests, so the whole pipeline runs with **zero external services**.

```go
// Read context from Chatwoot (the hub). Each method is one or more REST calls.
type ChatwootReader interface {
    Window(ctx context.Context, chatID ChatID) ([]Message, error)       // last ~15 msgs / 48h
    Profile(ctx context.Context, chatID ChatID) (map[string]any, error) // contact custom attributes
}

// Write results back to Chatwoot. Each method maps to one or more REST calls.
type ChatwootWriter interface {
    PostPrivateNote(ctx context.Context, chatID ChatID, text string) error
    MergeContactAttributes(ctx context.Context, chatID ChatID, attrs map[string]any) error
    SetLabels(ctx context.Context, chatID ChatID, labels []string) error
    SendOutgoing(ctx context.Context, chatID ChatID, text string, media []ResolvedAsset) error // Phase 3
}

// Call the LLM with the assembled prompt; return the parsed JSON contract.
type Drafter interface {
    Draft(ctx context.Context, prompt Prompt) (RawDraft, error)
}

// The immutable content snapshot (assistant.json + pricing + topics + media),
// loaded from xpayment-content and hot-swapped on reload (03). No DB, no retrieval (Decision 7).
type ContentSource interface {
    Get() *Snapshot                                                     // config + topics + catalog + prices
}

// Resolve the refs the model picked against the snapshot's media catalog.
type Catalog interface {
    Resolve(refs []string) (resolved []ResolvedAsset, unknown []string)
}

// The single source of price numbers (from the snapshot's pricing.json).
type Prices interface {
    Render(text string, lang string) (string, error)                   // replace {{...}} tokens
}
```

Notes:
- **Each `ChatwootWriter`/`ChatwootReader` method is one or more REST calls.** `MergeContactAttributes`, for instance, may read the contact, merge, and patch.
- **JSON numbers arrive as `float64`.** Anything numeric in `profile_patch` (e.g. a monthly volume) unmarshals into `map[string]any` as `float64` — coerce deliberately when writing to Chatwoot attributes, don't assume `int`.

---

## Memory

Two horizons, no summary (Decision 9):

| Horizon | Lives on | Scope | Read via |
|---|---|---|---|
| **Window** | Chatwoot conversation | last ~15 messages / 48h of *this* conversation | `ChatwootReader.Window` |
| **Profile** | Chatwoot contact (custom attributes) | persistent facts about the *person/business* | `ChatwootReader.Profile` |

The profile is updated by **additive merge** (step 5): a new `profile_patch` adds or overwrites confident fields but **never nulls** a previously-known one. There is **no running summary** in v1. If long conversations later need compression, the smallest addition is a single multi-line **conversation** custom attribute holding a rolling summary — added behind `ChatwootReader`/`ChatwootWriter` without touching the core.

---

## Worked example

A customer's **50th message**, in Russian, asking about price and cashier count.

**Dynamic suffix the brain assembles** (prefix `[A]–[E]` is cached and omitted here):

```
PROFILE (from contact custom attributes):
  business_type: "интернет-магазин"
  monthly_volume_tenge: 1500000
  preferred_language: "ru"

WINDOW (last 15 of 50 messages, trimmed):
  ... earlier turns about QR vs payment links ...
  customer: "Окей, понятно про QR."
  agent:    "Отлично! Что-то ещё подсказать?"

CURRENT MESSAGE:
  customer: "А сколько касс можно подключить на тарифе Рост? И сколько он стоит?"
```

**Model returns (tokens intact, no numerals — Decision 8):**

```json
{
  "reply_text": "На тарифе «Рост» можно подключить до {{limit.growth}} касс, стоимость — {{price.growth}} в месяц. Для интернет-магазина с вашим оборотом этого обычно достаточно. Показать, как добавить кассу?",
  "reply_language": "ru",
  "asset_refs": ["pricing_table", "add_cashier_video"],
  "profile_patch": { "interested_tariff": "growth" },
  "suggested_callback": { "due_at": null, "note": "" },
  "suggested_status": { "stage": "qualifying" },
  "confidence": 0.82,
  "escalate": false,
  "escalation_reason": ""
}
```

**After post-processing** (values below are *illustrative* — the real numbers come from the PriceBook, [03](03-content-and-data.md#pricing--tokens-canonical)):

- **Prices injected:** `… до 5 касс, стоимость — 25 000 ₸ в месяц …` (`{{limit.growth}}` and `{{price.growth}}` rendered in Russian).
- **Media resolved:** `pricing_table` and `add_cashier_video` → two `ResolvedAsset` URLs (both existed in the catalog; none dropped).
- **Profile merged:** `interested_tariff: "growth"` added; existing fields untouched.
- **Status:** conversation labeled `qualifying`.
- **Outcome:** the rendered reply + two media posted as a **private note**; a human reviews and sends.

What this illustrates:
- The **window held only the last ~15 messages**, yet the **profile carried the early facts** (business type, volume) — so the bot stayed contextual without the brain storing anything.
- The model **saw the entire media catalog** in `[E]` but **returned only the two relevant refs**.
- **Prices came back as tokens** and were filled by Go, so a stale model could not have misquoted the tariff.

---

## Open questions

- **Exact window size.** Is "last 15 messages" the right cut, or a time-based "last 48h", or both with a cap? Tune against mined conversations — and **re-evaluate before Phase-3 auto-send**, where the window is the only safety net (no human reads the full thread).
- **Confidence threshold for auto-send.** The numeric gate for Phase-3 auto-send is unset; calibrate it on the golden set before enabling.
- **Escalation taxonomy.** Whether `escalation_reason` should be free text or a small enum the team can route on.
