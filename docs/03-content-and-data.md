# 03 · Content & Data — the embedded SQLite store

This file is the canonical home for the brain's **data schema** (config, knowledge base, prices, media metadata), the **in-memory snapshot** it loads, and the **validation** that gates a publish. The brain keeps this in an **embedded SQLite** database inside `xpayment-crm` (Decision 2), edited through the [admin UI](08-admin-ui.md). Conversations and the lead profile live in Chatwoot. Decisions/architecture are in [README.md](README.md); runtime in [02-assistant-brain.md](02-assistant-brain.md).

> **Why SQLite, embedded.** It keeps the service a **single standalone binary** (the DB is one file at `DB_PATH` — no DB server to run), it's the natural backend for the [admin UI's](08-admin-ui.md) CRUD + draft/publish, and it lets the brain also persist small operational state (e.g. a webhook-dedup table) across restarts. Conversational data still lives only in Chatwoot (Decision 1).

---

## Schema (DDL)

SQLite, applied by a migration on startup (mirror the goose pattern, or a tiny embedded migrator). Money is an **integer** (tenge).

```sql
-- the bot's "brain config" — versioned for draft/publish/rollback
CREATE TABLE assistant_config (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    version         INTEGER NOT NULL,
    status          TEXT    NOT NULL CHECK (status IN ('draft','published','archived')),
    persona         TEXT    NOT NULL,
    mission         TEXT    NOT NULL DEFAULT '',
    guardrails      TEXT    NOT NULL,             -- newline- or JSON-list of rules
    language_policy TEXT    NOT NULL DEFAULT '',
    reply_max_words INTEGER NOT NULL DEFAULT 120,
    enabled_tools   TEXT    NOT NULL DEFAULT '[]',-- JSON array
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    published_at    TEXT
);
-- at most one published config at a time
CREATE UNIQUE INDEX assistant_config_one_published ON assistant_config (status) WHERE status='published';

-- knowledge topics — one row per (slug, language); body uses PRICE TOKENS, never numerals
CREATE TABLE kb_topics (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT NOT NULL,
    language   TEXT NOT NULL CHECK (language IN ('ru','kk')),
    title      TEXT NOT NULL,
    summary    TEXT NOT NULL DEFAULT '',          -- helps the model pick the topic
    body_md    TEXT NOT NULL,                      -- tokens only (Decision 8)
    active     INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (slug, language)
);

-- media catalog (metadata; the "menu" the model selects from). Binaries live elsewhere (see Media).
CREATE TABLE kb_assets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ref         TEXT NOT NULL UNIQUE,              -- the stable slug the model returns
    topic_slug  TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('image','video','screen_recording','gif','link','document')),
    url         TEXT NOT NULL,                     -- served-dir URL or external URL
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL,                      -- WRITTEN FOR THE LLM — the selection menu entry
    language    TEXT NOT NULL DEFAULT 'any' CHECK (language IN ('ru','kk','any')),
    active      INTEGER NOT NULL DEFAULT 1,
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- prices — the SINGLE source of numbers (Decision 8)
CREATE TABLE tariffs (
    key           TEXT PRIMARY KEY,                -- 'launch','growth','scale'
    price_tenge   INTEGER NOT NULL,                -- integer tenge
    cashier_limit INTEGER NOT NULL,
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

-- non-tariff tokens, bilingual
CREATE TABLE placeholders (
    token    TEXT PRIMARY KEY,                     -- e.g. 'support.phone'
    value_ru TEXT NOT NULL,
    value_kk TEXT NOT NULL
);

-- audit + (optional) operational state
CREATE TABLE audit_log (id INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL DEFAULT (datetime('now')),
                        actor TEXT, action TEXT, detail TEXT);
CREATE TABLE processed_messages (chatwoot_message_id TEXT PRIMARY KEY, at TEXT NOT NULL DEFAULT (datetime('now')));
```

`processed_messages` gives the webhook handler **real idempotency across restarts** (dedup redelivered `message_created` events) — something the file-only model couldn't do.

---

## The in-memory Snapshot

The brain never queries SQLite on the hot path. At startup (and on **publish**) it loads the **published** config + active topics/assets/prices into one immutable snapshot behind an atomic pointer:

```go
type Snapshot struct {
    Config AssistantConfig   // the published assistant_config row
    Prices PriceBook         // tariffs + placeholders
    Topics []Topic           // active kb_topics (both languages)
    Assets []Asset           // active kb_assets
    Loaded time.Time
}
type Content struct{ snap atomic.Pointer[Snapshot] }
func (c *Content) Get() *Snapshot { return c.snap.Load() }   // the ContentSource port (02)
```

### Validate on load, fail loudly
Build the new snapshot, **validate it, and only then swap the pointer**; if validation fails, keep the old snapshot and surface the error in the admin UI (the publish is rejected):
- every `kb_assets.url` resolves (served file exists / URL well-formed) — no dead media;
- every `{{price.*}}`/`{{limit.*}}` token used in any topic **resolves** in `tariffs`;
- **warn** if a topic exists in one language but not the other.

---

## Draft → publish → rollback (the config lifecycle)

The admin UI ([08](08-admin-ui.md)) edits a **draft** `assistant_config` (and the KB/price tables); **publish** validates → promotes the draft to `published` (the unique index guarantees one live config) and **hot-reloads the snapshot**; **rollback** re-publishes an earlier version. `audit_log` records who changed what. KB/price rows carry `active` + `updated_at`; the Playground tests a draft against the live data before publishing.

---

## Pricing & tokens (canonical)

A token is `{{namespace.key}}` — the **namespace selects the field**, the **key selects the row**:

| Token | Resolves to | from |
|---|---|---|
| `{{price.growth}}` | `tariffs.growth.price_tenge` → e.g. `19 900 ₸` | `tariffs` |
| `{{limit.growth}}` | `tariffs.growth.cashier_limit` → e.g. `5` | `tariffs` |
| `{{support.phone}}` | the placeholder value | `placeholders` |

```go
// Replace every {{namespace.key}} in text for lang. Error if any token is unknown or any '{{' remains.
func (p *PriceBook) Render(text string, lang string) (string, error)
```
- **Failure path:** an unknown/leftover token → `Render` errors → the brain posts a *"check pricing manually"* note instead of a half-rendered price ([02 · post-processing](02-assistant-brain.md#post-processing-pipeline)).
- **Why:** the model never sees a number, so it can't hallucinate or mangle one; substitution happens **after** the model; one edit to a `tariffs` row updates every topic; money is an integer; `audit_log` records who changed a price and when.

---

## Media

- **Binaries** (images, videos, screen-recordings) live in a served directory (`MEDIA_DIR`, served at `/media/...` by the same binary) **or** object storage; the admin UI uploads to it. Large video → external URL/object storage to keep the binary lean.
- **Metadata** lives in `kb_assets` (the `ref` + LLM-facing `description` the model selects on). `url` points at wherever the binary is served.

---

## The lead profile (lives in Chatwoot, not here)

The profile is computed by the brain and written to **Chatwoot contact custom attributes** (Decision 9) — not stored in SQLite. These attributes must be **pre-defined in Chatwoot** before they can be written ([01](01-infrastructure.md#3-brain--chatwoot)). Expected keys: `business_type`, `monthly_volume_tenge`, `current_payment_method`, `cashiers_needed`, `technical_level`, `urgency`, `interested_tariff`, `preferred_language`, `main_objection`, `fit_tariff`/`fit_score` (computed; a sort key until calibrated), `notes`.

---

## Open questions

- **Config version history depth** — keep all `assistant_config` versions, or last N?
- **Media storage** — served `MEDIA_DIR` (simplest, all-in-one) vs object storage for video; default served-dir for v1.
- **KK/RU authoring coverage** — every topic in both languages, or some RU-only? Pricing is **never** translated on the fly — always rendered from `tariffs` per language.
- **`fit_score` calibration** — a sort key until validated against real conversions.
