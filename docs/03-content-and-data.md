# 03 · Content and Data — knowledge base, media, pricing, config

This file is the **canonical home for all DDL** and for everything authored or managed: the knowledge base, the media catalog, prices, the assistant config, and the admin lifecycle. Decisions and architecture are in [README.md](README.md); runtime behavior is in [02-assistant-brain.md](02-assistant-brain.md). This file assumes Decisions 2, 7, 8.

All tables below belong to the **brain's own small Postgres** (Decision 2) — no conversations, no contacts. The lead profile is **not** here; it lives on the Chatwoot contact (see [Data model overview](#data-model-overview)).

---

## Knowledge base

A **topic** is one answerable subject (tariffs, adding a cashier, refunds, QR vs payment link, security/cashier-role, onboarding). Each topic is authored in **both languages** as separate rows.

```sql
CREATE TABLE kb_topics (
    id         BIGSERIAL PRIMARY KEY,
    slug       TEXT        NOT NULL,                         -- 'tariffs', 'add_cashier', 'refunds'
    language   TEXT        NOT NULL CHECK (language IN ('ru','kk')),
    title      TEXT        NOT NULL,
    summary    TEXT        NOT NULL DEFAULT '',              -- short; the model uses it to judge relevance
    body_md    TEXT        NOT NULL,                         -- the answer; PRICE TOKENS ONLY (Decision 8)
    keywords   TEXT[]      NOT NULL DEFAULT '{}',
    active     BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (slug, language)
);
```

- `summary` is a one-liner that helps the model pick the right topic when several are loaded.
- `body_md` is the actual answer text and **must contain only price/limit tokens**, never numerals (Decision 8; grammar in [Pricing & templates](#pricing--templates)).
- **No retrieval (Decision 7):** every `active` topic body, in **both** languages, is loaded into the cached prompt block `[D]` (see [02 · Prompt assembly](02-assistant-brain.md#prompt-assembly)). At a few dozen topics this fits comfortably; the `KnowledgeRetriever` port keeps the graduation path open without changing the schema.

Example `body_md` (Russian, tokens intact):

```markdown
На тарифе «Рост» вы можете подключить до {{limit.growth}} касс,
стоимость — {{price.growth}} в месяц. Подходит интернет-магазинам
со средним оборотом. Платежи приходят сразу на ваш Kaspi.
```

---

## Media

```sql
CREATE TABLE kb_assets (
    id          BIGSERIAL PRIMARY KEY,
    ref         TEXT        NOT NULL UNIQUE,                 -- stable slug the model returns: 'add_cashier_video'
    topic_slug  TEXT        NOT NULL DEFAULT '',             -- the topic this asset belongs to
    kind        TEXT        NOT NULL CHECK (kind IN ('image','video','screen_recording','gif','link','document')),
    url         TEXT        NOT NULL,                        -- points into the served xpayment-content site
    title       TEXT        NOT NULL DEFAULT '',
    description TEXT        NOT NULL,                        -- WRITTEN FOR THE LLM — this is the menu entry
    language    TEXT        NOT NULL DEFAULT 'any' CHECK (language IN ('ru','kk','any')),
    active      BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

> **Assumption.** The build spec named this column `topic_id`. Because `kb_topics` is keyed by `(slug, language)`, a topic is a *concept spanning two rows*, so we associate assets by **`topic_slug`** (the concept) rather than a FK to one language-specific row. The asset's own `language` (or `any`) handles language separately.

**LLM-as-selector (Decision 7).** The model never searches. The whole catalog is rendered into prompt block `[E]` as a menu — `ref | kind | topic | description` — and the model returns the `asset_refs` it wants. Go then resolves each ref with a **key lookup** (`KB.ResolveRefs`) and **drops unknowns** (see [02 · Post-processing](02-assistant-brain.md#post-processing-pipeline)). The `description` is therefore the most important field: it is the only thing the model reads to choose. Write it for the model, e.g.:

> *"30-second screen recording (RU voiceover) showing how to add the Kaspi virtual cashier and enter the OTP. Use when the customer asks how to connect a cashier."*

**Binaries vs metadata.** The image/video/screen-recording **files** live in the separate **`xpayment-content`** repo (git-versioned, served statically by URL); only the **metadata** (this table) is in the DB. Adding media = push the file to `xpayment-content`, then create the `kb_assets` row with its `url` and `description`. Nothing about the binary is stored in the brain's database.

---

## Pricing & templates

Two tables hold every value that may appear in customer-facing text. They are the **single source** of prices (Decision 8).

```sql
CREATE TABLE tariffs (
    key           TEXT        PRIMARY KEY,                   -- 'trial','launch','growth','scale'
    price_tenge   BIGINT      NOT NULL,                      -- integer tenge — no floats for money
    cashier_limit INT         NOT NULL,                      -- max cashiers / devices
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE placeholders (
    token      TEXT        PRIMARY KEY,                      -- full token: 'support.phone', 'trial.days'
    value_ru   TEXT        NOT NULL,
    value_kk   TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Token grammar

A token is `{{namespace.key}}`. The **namespace selects a column**; the **key selects a row**:

| Token | Resolves to | Column | Row |
|---|---|---|---|
| `{{price.growth}}` | `tariffs` | `price_tenge` | `growth` |
| `{{limit.growth}}` | `tariffs` | `cashier_limit` | `growth` |
| `{{support.phone}}` | `placeholders` | `value_ru` / `value_kk` | row `support.phone` |

`price` and `limit` are the two namespaces mapped (in code) to the `tariffs` columns `price_tenge` and `cashier_limit`. Any other namespace falls through to the generic `placeholders` table, keyed by the full token and resolved per language.

### Rendering

```go
// Replace every {{namespace.key}} in text with the real value for lang.
// Returns an error if any token is unknown or any '{{' remains after rendering.
func (p *PriceBook) Render(text string, lang string) (string, error)
```

Worked example:

```
input  (lang=ru): "до {{limit.growth}} касс, стоимость — {{price.growth}} в месяц"
output (lang=ru): "до 5 касс, стоимость — 25 000 ₸ в месяц"
```

*(Numbers illustrative — the real values are whatever the `tariffs` rows hold.)* Money is formatted per locale (grouped thousands, `₸`).

**Failure path.** If a token references a missing row/column, or a `{{` survives rendering, `Render` returns an error and the brain **does not ship a half-rendered price** — it posts a *"check pricing manually"* note instead (see [02 · Post-processing, step 4](02-assistant-brain.md#post-processing-pipeline)).

### Why this design

- The **model never sees a number**, so it cannot hallucinate or mangle one; substitution happens **after** the model.
- **One edit updates everywhere** — change a `tariffs` row and every topic that quotes it is correct on the next message.
- **Money is an integer** (`BIGINT` tenge), avoiding float rounding, with `updated_at` for a basic audit trail. Treat `tariffs` changes as **review-gated** (a deliberate edit, not a casual one) — for a payments product, a wrong price is the worst possible output.

---

## Assistant config

```sql
CREATE TABLE assistant_configs (
    id              BIGSERIAL   PRIMARY KEY,
    version         INT         NOT NULL,
    status          TEXT        NOT NULL CHECK (status IN ('draft','published','archived')),
    persona         TEXT        NOT NULL,                    -- WHO the bot is (the "soul")
    mission         TEXT        NOT NULL DEFAULT '',         -- WHAT it is trying to achieve
    guardrails      TEXT        NOT NULL,                    -- what it must / must not do
    language_policy TEXT        NOT NULL DEFAULT '',         -- KK/RU rule (see Open questions)
    reply_max_words INT         NOT NULL DEFAULT 120,
    model           TEXT        NOT NULL,                    -- which LLM
    temperature     REAL        NOT NULL DEFAULT 0.3,
    enabled_tools   JSONB       NOT NULL DEFAULT '[]',       -- toggled capabilities
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ
);

-- At most one published config at any time.
CREATE UNIQUE INDEX assistant_configs_one_published
    ON assistant_configs (status) WHERE status = 'published';
```

**Soul vs. skeleton.** This table is the **soul** — `persona`, `mission`, `guardrails`, `language_policy` are editable from the admin and shape blocks `[B]`/`[C]` of the prompt. The **skeleton** — the FRAME `[A]`: the JSON output contract, the assembly order, and the hard rules (no price numerals, KB-only, ≤3 media) — is **code-owned and not editable** (see [02 · The FRAME](02-assistant-brain.md#prompt-assembly)). You can change the bot's voice and policy without being able to break its output contract.

---

## Admin & config lifecycle

A **Vue admin** (in the existing `xpayment-frontend`) edits persona, knowledge base, media, and prices. The lifecycle:

- **Draft → publish → rollback.** Edits create or update a `draft` config; **publish** promotes one version to `published` (the unique index guarantees exactly one live config); **rollback** re-publishes an earlier version. KB/media/price rows carry `active` flags and `updated_at` for lighter-weight change control.
- **Playground (dry-run).** Type a message, pick a language and a config version, and see the would-be result — drafted reply, chosen media, extracted `profile_patch`, confidence — **without sending anything and without touching a real conversation**. This is the primary day-to-day testing surface.
- **Golden set / evals.** A set of **real questions mined from the existing chats** (see [README roadmap](README.md#roadmap)), with expected behavior. Run them against a config **before publishing**. Score two things separately: **answer quality** and **media-selection precision** (did it attach the right asset, and not a wrong-topic one). Attaching the wrong pricing infographic is a correctness failure, not a cosmetic one.

---

## Data model overview

### Tables in the brain's database

| Table | Holds | Keyed by |
|---|---|---|
| `assistant_configs` | persona/guardrails/skeleton settings, versioned | `id` (one `published`) |
| `kb_topics` | answer text per subject, bilingual | `(slug, language)` |
| `kb_assets` | media **metadata** (binaries in `xpayment-content`) | `ref` |
| `tariffs` | canonical prices + cashier limits | `key` |
| `placeholders` | non-tariff token values, bilingual | `token` |

### Relationships

```
assistant_configs        (standalone, versioned)

kb_topics (slug,language) ──1:N──▶ kb_assets (via topic_slug)

kb_topics.body_md  ──{{price.*}} / {{limit.*}}──▶  tariffs (rendered by PriceBook)
kb_topics.body_md  ──{{other.*}}──────────────▶  placeholders

[ Chatwoot contact custom attributes ]  ◀── the lead PROFILE (owned by Chatwoot, not this DB)
```

### The lead profile (lives on the Chatwoot contact, not here)

The profile is computed by the brain and written to **Chatwoot contact custom attributes** (Decision 9). These attributes must be **pre-defined in Chatwoot** before they can be written (see [01](01-infrastructure.md#3-brain--chatwoot)). The expected keys:

| Attribute | Meaning |
|---|---|
| `business_type` | e.g. интернет-магазин, услуги, доставка |
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

`fit_tariff` / `fit_score` are derived by a pure Go function from the other fields (the mapping is brain logic; see the profile discussion in [02 · Memory](02-assistant-brain.md#memory)).

---

## Open questions

- **KK/RU authoring coverage.** Is **every** topic authored in *both* Kazakh and Russian, or are some Russian-only with on-the-fly translation? Pricing must **never** be translated on the fly — it is always rendered from `tariffs` per language. Decide the coverage policy before bulk-authoring.
- **Language tie-break rule.** The persona's `language_policy` needs an explicit rule for mixed-language messages (current default: mirror the customer's dominant language; if genuinely mixed, prefer Russian — see [02 · The FRAME](02-assistant-brain.md#prompt-assembly)).
- **Price-edit authority.** Who may edit `tariffs`, and through what review gate (admin UI with confirmation vs. migration/code review)?
- **`fit_score` calibration.** The score is arbitrary until validated against real conversions; treat it as a prioritization sort key in month one, not a truth.
