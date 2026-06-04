# 03 · Content & Data — the content repo (files, snapshot, git lifecycle)

This file is the canonical home for the **content-repo file shapes**, the **in-memory snapshot loader + validation**, the **pricing/token rules**, and the **git config lifecycle**. The brain has **no database** (Decision 2): its persona, knowledge, prices, and media metadata are files in `xpayment-content`. Decisions/architecture are in [README.md](README.md); runtime behavior in [02-assistant-brain.md](02-assistant-brain.md); the day-to-day operator workflow in [08-admin-ui.md](08-admin-ui.md).

---

## The content repo (`xpayment-content`)

```
xpayment-content/
  assistant.json          persona, guardrails, language policy, model settings
  pricing.json            tariffs (price + cashier limit) + non-tariff placeholders
  media.json              the media catalog metadata (the "menu" the model reads)
  knowledge/              topic bodies as markdown with YAML front-matter (easy to edit + diff)
    tariffs.ru.md
    tariffs.kk.md
    onboarding.ru.md
    ...
  media/                  the binaries (Git LFS for video; or large files in object storage)
    tariffs/pricing-ru.png
    onboarding/add-cashier.mp4
  README.md               the operator guide (see 08)
```

Topic **bodies are markdown** (multi-line text in JSON is painful to edit and diff). Media **binaries**: keep **small images in git**; put **video / screen-recordings in object storage**, referenced by URL in `media.json` (**recommended default** — keeps the repo and the brain's checkout light). Git LFS is a fallback only if you must keep video in git. Either way the **metadata stays in git** so `git status`/`git diff` reflect catalog changes ([08](08-admin-ui.md)).

---

## File shapes

The loader must parse these.

### `assistant.json` — the persona/“soul” (code never owns this; the FRAME does — see [02 · prompt](02-assistant-brain.md#prompt-assembly))
```jsonc
{ "display_name": "xpayment assistant",
  "persona": "string", "mission": "string",
  "guardrails": ["string", "..."],
  "language_policy": "Reply in the newest message's language; if mixed, prefer Russian.",
  "reply_max_words": 120,
  "model": "claude-...", "temperature": 0.3 }
```

### `pricing.json` — money as integers (tenge); the **only** place numbers live (Decision 8)
```jsonc
{ "tariffs": {
    "launch": { "price_tenge": 9900,  "cashier_limit": 1 },
    "growth": { "price_tenge": 19900, "cashier_limit": 5 },
    "scale":  { "price_tenge": 49900, "cashier_limit": 20 } },
  "placeholders": { "support.phone": "+7 700 ...", "settlement.days": "1-2" } }
```
*(Numbers illustrative — the real values are whatever the file holds.)*

### `media.json` — one entry per file; `file` is a path inside the repo
```jsonc
[ { "ref": "tariffs_infographic_ru", "kind": "image", "file": "media/tariffs/pricing-ru.png",
    "topic": "tariffs", "language": "ru",
    "description": "Infographic of all tiers, price + cashier limit, RU. For pricing / which-plan." } ]
```
`ref` is the stable slug the model returns; `description` is **written for the LLM** — it is the only thing the model reads to choose (LLM-as-selector, Decision 7). `kind ∈ image|video|screen_recording|gif|link|document`.

### `knowledge/<slug>.<lang>.md` — body is markdown; metadata is front-matter; **tokens, not numbers**
```markdown
---
slug: tariffs
language: ru
summary: pricing and plan selection
---
Тарифы: Launch — {{price.launch}}/мес (до {{limit.launch}} кассы), Growth — {{price.growth}}/мес ...
```

---

## The in-memory Snapshot

On startup (and on reload) the brain reads a local checkout of the content repo into **one immutable in-memory snapshot**, held behind an atomic pointer so reads are lock-free:

```go
type Snapshot struct {
    Config   AssistantConfig   // assistant.json
    Prices   PriceBook         // pricing.json (map[tierKey]Tier + placeholders)
    Topics   []Topic           // knowledge/*.md (front-matter + body)
    Assets   []Asset           // media.json (ref, kind, url, description, topic, language)
    LoadedAt time.Time
    Commit   string            // git rev of the checkout, for logging
}

type Content struct{ snap atomic.Pointer[Snapshot] }
func (c *Content) Get() *Snapshot { return c.snap.Load() }   // the ContentSource port (02)
```

### Validate on load, fail loudly
Build the new snapshot, **validate it, and only then swap the pointer**; if validation fails, keep the old snapshot and log the error:
- every `media.json` entry's `file` must **exist on disk** (else the model could attach a dead asset);
- every `{{price.*}}`/`{{limit.*}}` token used in any topic must **resolve** in `pricing.json`;
- **warn** if a topic exists in one language but not the other.

Resolve each asset's `file` to a public URL (`KB_MEDIA_BASE_URL + file`, or serve `media/` from the brain — see [05](05-configuration.md), [04](04-service-and-deployment.md#observability)).

---

## Pricing & tokens (canonical)

A token is `{{namespace.key}}`. The **namespace selects the field**; the **key selects the row**:

| Token | Resolves to | from |
|---|---|---|
| `{{price.growth}}` | `tariffs.growth.price_tenge` → e.g. `19 900 ₸` | `pricing.json` |
| `{{limit.growth}}` | `tariffs.growth.cashier_limit` → e.g. `5` | `pricing.json` |
| `{{support.phone}}` | the placeholder value | `pricing.json` `placeholders` |

```go
// Replace every {{namespace.key}} in text for lang. Error if any token is unknown or any '{{' remains.
func (p *PriceBook) Render(text string, lang string) (string, error)
```
- **Failure path:** an unknown/leftover token → `Render` errors → the brain posts a *"check pricing manually"* note instead of shipping a half-rendered price ([02 · post-processing](02-assistant-brain.md#post-processing-pipeline)).
- **Why:** the model never sees a number, so it can't hallucinate or mangle one; substitution happens **after** the model; one edit to `pricing.json` updates everything; money is an integer; `git blame pricing.json` is the audit of who changed a price and when ([08](08-admin-ui.md)).

---

## The git config lifecycle

`xpayment-content` is edited with normal git tooling; **every change is visible via `git status`/`git diff`**, and the lifecycle maps directly onto git so you build none of it (full operator guide in [08-admin-ui.md](08-admin-ui.md)):

| Lifecycle step | Git |
|---|---|
| Draft | a branch / the working copy |
| Publish | merge to `main` + push |
| Rollback | `git revert` |
| Audit ("who raised the price, when") | `git log` / `git blame` |
| Review | a pull request |

**Reload-on-change:** on push to `main`, the brain pulls, builds a new `Snapshot`, validates it, and atomically swaps it in (keeping the old on failure). See [04 · reload](04-service-and-deployment.md#content-checkout--reload) and [06 · reload webhook](06-api-and-contracts.md#github-reload-webhook).

---

## The lead profile (lives in Chatwoot, not here)

The profile is computed by the brain and written to **Chatwoot contact custom attributes** (Decision 9), not stored in the content repo or any brain DB. These attributes must be **pre-defined in Chatwoot** before they can be written ([01](01-infrastructure.md#3-brain--chatwoot)). Expected keys:

| Attribute | Meaning |
|---|---|
| `business_type` | интернет-магазин, услуги, доставка … |
| `monthly_volume_tenge` | rough monthly turnover (numeric) |
| `current_payment_method` | none / kaspi_manual / acquiring / … |
| `cashiers_needed` | how many cashiers/devices |
| `technical_level` | developer / semi-technical / non-technical |
| `urgency` | now / this_month / exploring |
| `interested_tariff` | tariff the lead leans toward |
| `preferred_language` | ru / kk |
| `main_objection` | security / price / legality / effort / none |
| `fit_tariff`, `fit_score` | qualification output computed by the brain (a sort key until calibrated) |
| `notes` | free-text salient facts |

---

## Open questions

- **Media storage** — **decided default: object storage for video/screen-recordings, git for small images** (Git LFS only if you must keep video in git).
- **KK/RU authoring coverage** — is **every** topic authored in both languages, or some RU-only with on-the-fly translation? Pricing is **never** translated on the fly — always rendered from `pricing.json` per language.
- **Price-edit authority** — who may merge changes to `pricing.json` (git branch protection / CODEOWNERS).
- **`fit_score` calibration** — arbitrary until validated against real conversions; treat as a sort key in month one.
