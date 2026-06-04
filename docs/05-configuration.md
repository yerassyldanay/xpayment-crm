# 05 · Configuration

This file is the **canonical home for every environment variable** the brain reads (just as [03-content-and-data.md](03-content-and-data.md) is the home for all DDL). Other docs reference these names and never restate the catalog. Decision context is in [README.md](README.md); how config is consumed at boot is in [04-service-and-deployment.md](04-service-and-deployment.md).

## Loading pattern

Copy `xpayment/internal/infrastructure/config/config.go`:

- A single `Config` struct composed of nested structs (`Chatwoot`, `Anthropic`, `OTel`, …).
- A `getEnv(key, fallback)` helper that trims and falls back to a default; **required** values are validated at startup and the service refuses to boot if missing.
- `loadDotEnv(".env")` at startup, with the file split:
  - **`.env`** — local dev, contains secrets, **gitignored**.
  - **`.env.example`** — committed template, every key present with placeholder/empty values.
  - **`.env.remote`** — committed production overrides, **no secrets** (secrets are injected by the host/orchestrator).
- A `buildDatabaseURL()` that prefers `DATABASE_URL` and otherwise assembles it from parts (mirror the main repo).

```go
// shape only — see xpayment/internal/infrastructure/config/config.go
type Config struct {
    Env       string        // APP_ENV
    LogLevel  string        // LOG_LEVEL
    HTTPAddr  string        // HTTP_ADDR
    Database  DatabaseConfig
    Chatwoot  ChatwootConfig
    Anthropic AnthropicConfig
    KB        KBConfig
    Admin     AdminAuthConfig
    OTel      OTelConfig
}
```

## Catalog

Legend — **Req?**: ✅ required (no safe default), ⚙️ has a default, 🔒 secret.

### Runtime / HTTP

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `APP_ENV` | `prod` | ⚙️ | `Env` | `dev`\|`stage`\|`prod`; invalid → `prod`. |
| `LOG_LEVEL` | `info` | ⚙️ | `LogLevel` | `debug`\|`info`\|`warn`\|`error`. |
| `HTTP_ADDR` | `:8080` | ⚙️ | `HTTPAddr` | Listen address. Matches `EXPOSE 8080` ([04](04-service-and-deployment.md#dockerfile)). |
| `METRICS_TOKEN` | `` (off) | ⚙️🔒 | `MetricsToken` | If set, `GET /metrics` requires `Authorization: Bearer <token>`. |
| `ALLOWED_ORIGINS` | `` | ⚙️ | `AllowedOrigins` | Comma-separated; CORS allowlist for the admin API ([08](08-admin-ui.md)). |

### Brain database (its own Postgres — Decision 2)

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `DATABASE_URL` | built from parts | ⚙️ | `Database.URL` | Full DSN; if unset, assembled from the parts below. |
| `DB_HOST` | `localhost` | ⚙️ | — | Used only when `DATABASE_URL` unset. |
| `DB_PORT` | `5432` | ⚙️ | — | |
| `DB_NAME` | `brain_db` | ⚙️ | — | |
| `DB_USER` | `brain` | ⚙️ | — | |
| `DB_PASSWORD` | `` | ⚙️🔒 | — | |
| `DB_SSLMODE` | `disable` (dev) | ⚙️ | — | `require` in production. |

### Chatwoot (the hub — [01](01-infrastructure.md), [06](06-api-and-contracts.md))

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `CHATWOOT_BASE_URL` | — | ✅ | `Chatwoot.BaseURL` | e.g. `https://chat.xpayment.kz`. |
| `CHATWOOT_ACCOUNT_ID` | — | ✅ | `Chatwoot.AccountID` | Numeric account the inbox lives in. |
| `CHATWOOT_API_TOKEN` | — | ✅🔒 | `Chatwoot.APIToken` | Agent/bot token for REST write-back (`api_access_token` header). |
| `CHATWOOT_INBOX_ID` | — | ✅ | `Chatwoot.InboxID` | The WhatsApp/API inbox the brain serves. |
| `CHATWOOT_WEBHOOK_SECRET` | — | ✅🔒 | `Chatwoot.WebhookSecret` | Verifies inbound account-webhook calls ([06](06-api-and-contracts.md)). |

### Anthropic (the LLM — [02](02-assistant-brain.md))

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `ANTHROPIC_API_KEY` | — | ✅🔒 | `Anthropic.APIKey` | |
| `ANTHROPIC_MODEL` | `claude-sonnet-4-6` | ⚙️ | `Anthropic.Model` | Sonnet is the cost/quality default for drafting; bump to `claude-opus-4-8` for quality or `claude-haiku-4-5-20251001` for cost. |
| `ANTHROPIC_MAX_TOKENS` | `1024` | ⚙️ | `Anthropic.MaxTokens` | Drafts are ≤~120 words ([02](02-assistant-brain.md)); cap output. |

> **Compliance gate.** Setting `ANTHROPIC_API_KEY` means customer conversation text is sent to Anthropic. Confirm this is permitted under Kazakhstan's personal-data law before production — tracked in [09 · open-questions](09-product-and-ops.md).

### Knowledge-base media ([03](03-content-and-data.md))

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `KB_MEDIA_BASE_URL` | — | ✅ | `KB.MediaBaseURL` | Static base for `xpayment-content/knowledge-base/…`; `kb_assets.url` resolves against it. |

### Admin auth (cross-service — [08](08-admin-ui.md))

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `ADMIN_AUTH_MODE` | `static` | ⚙️ | `Admin.Mode` | `static` (shared admin token) or `introspect` (validate main-backend user tokens). |
| `ADMIN_API_TOKEN` | — | cond.🔒 | `Admin.Token` | Required when `ADMIN_AUTH_MODE=static`. |
| `MAIN_BACKEND_BASE_URL` | — | cond. | `Admin.IntrospectURL` | Required when `ADMIN_AUTH_MODE=introspect`; the brain calls it to validate `xusr_live_…` tokens. |

### Observability (OTel — [04](04-service-and-deployment.md#observability))

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `OTEL_ENABLED` | `false` | ⚙️ | `OTel.Enabled` | When false, a no-op tracer is installed. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | ⚙️ | `OTel.Endpoint` | OTLP HTTP → Jaeger. |
| `OTEL_SERVICE_NAME` | `xpayment-copilot` | ⚙️ | `OTel.ServiceName` | Distinguish from the main `xpayment` service. |
| `OTEL_SAMPLE_RATE` | `1.0` | ⚙️ | `OTel.SampleRate` | `0.0`–`1.0`. |

## Secrets handling

- **Secret (🔒):** `ANTHROPIC_API_KEY`, `CHATWOOT_API_TOKEN`, `CHATWOOT_WEBHOOK_SECRET`, `ADMIN_API_TOKEN`, `DB_PASSWORD`, `METRICS_TOKEN`.
- Keep them only in the gitignored `.env` (dev) or injected by the host/secret manager (prod). Never in `.env.example` or `.env.remote`.
- `.env.example` must list **every** key above with empty/placeholder values so a new engineer sees the full surface at a glance.

## Open questions

- **Admin auth mode** — `static` vs `introspect` (recommendation in [08](08-admin-ui.md)); the chosen mode decides which of `ADMIN_API_TOKEN` / `MAIN_BACKEND_BASE_URL` is required.
- **Model choice** — confirm `ANTHROPIC_MODEL` default after eval results ([07](07-testing-and-evals.md)).
- **DB SSL** — confirm `DB_SSLMODE=require` and cert setup for the production brain DB.
