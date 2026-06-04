# 05 · Configuration

The **canonical home for every environment variable** the brain reads (just as [03-content-and-data.md](03-content-and-data.md) is the home for the data schema). Other docs reference these names and never restate the catalog. Decision context is in [README.md](README.md); how config is consumed at boot is in [04-service-and-deployment.md](04-service-and-deployment.md).

> **Naming rule (Decision 13):** the LLM is reached via **OpenRouter**, but env vars are **provider-neutral** — `LLM_*`, never `OPENROUTER_*` / `OPENAI_*` / `ANTHROPIC_*`. Swapping provider/model is a config change.

## Loading pattern

Mirror `xpayment/internal/infrastructure/config/config.go`: one `Config` struct of nested structs, a `getEnv(key, fallback)` helper (required values validated at boot — refuse to start if missing), and a `.env` / `.env.example` / `.env.remote` split (`.env` holds secrets and is gitignored; `.env.example` lists every key).

```go
type Config struct {
    Env      string        // APP_ENV
    LogLevel string        // LOG_LEVEL
    HTTPAddr string        // HTTP_ADDR  (one port: webhook + /admin + /media)
    DBPath   string        // DB_PATH    (embedded SQLite file)
    LLM      LLMConfig     // OpenRouter, provider-neutral
    Chatwoot ChatwootConfig
    Admin    AdminConfig
    Media    MediaConfig
    OTel     OTelConfig
}
```

## Catalog

Legend — **Req?**: ✅ required (no safe default), ⚙️ has a default, 🔒 secret.

### Runtime / HTTP

| Variable | Default | Req? | Note |
|---|---|---|---|
| `APP_ENV` | `prod` | ⚙️ | `dev`\|`stage`\|`prod`. |
| `LOG_LEVEL` | `info` | ⚙️ | `debug`\|`info`\|`warn`\|`error`. |
| `HTTP_ADDR` | `:8080` | ⚙️ | The **one public port** — webhook + `/admin` + `/media`. |
| `METRICS_TOKEN` | `` (off) | ⚙️🔒 | If set, `GET /metrics` requires `Authorization: Bearer <token>`. |

### Store

| Variable | Default | Req? | Note |
|---|---|---|---|
| `DB_PATH` | `./data/brain.db` | ⚙️ | Embedded SQLite file (config/KB/prices/media-meta + dedup). Put on a persistent volume in prod. |

### LLM (OpenRouter, provider-neutral — Decision 13, [10](10-prompt-and-examples.md))

| Variable | Default | Req? | Note |
|---|---|---|---|
| `LLM_API_KEY` | — | ✅🔒 | The OpenRouter API key (named neutrally). |
| `LLM_BASE_URL` | `https://openrouter.ai/api/v1` | ⚙️ | OpenAI-compatible base URL; swappable to any compatible gateway. |
| `LLM_MODEL` | `anthropic/claude-sonnet-4` | ⚙️ | An **OpenRouter model id**. Bump to a stronger model for quality or a cheaper one for cost — config only. |
| `LLM_MAX_TOKENS` | `1024` | ⚙️ | Cap on draft output (drafts are ≤~120 words, [02](02-assistant-brain.md)). |
| `LLM_TEMPERATURE` | `0.3` | ⚙️ | Low for consistent drafting. |

> **Compliance gate.** Setting `LLM_API_KEY` means customer conversation text is sent to OpenRouter (and its upstream model provider) abroad. Confirm this is permitted under Kazakhstan's personal-data law before production — tracked in [09 · open-questions](09-product-and-ops.md). The choice of `LLM_MODEL` also decides *which* provider receives the data.

### Chatwoot (the hub — [01](01-infrastructure.md), [06](06-api-and-contracts.md))

| Variable | Default | Req? | Note |
|---|---|---|---|
| `CHATWOOT_BASE_URL` | — | ✅ | e.g. `https://chat.xpayment.kz`. |
| `CHATWOOT_ACCOUNT_ID` | — | ✅ | Numeric account. |
| `CHATWOOT_API_TOKEN` | — | ✅🔒 | Agent/bot token for REST write-back (`api_access_token` header). |
| `CHATWOOT_INBOX_ID` | — | ✅ | The WhatsApp/API inbox the brain serves. |
| `CHATWOOT_WEBHOOK_SECRET` | — | ✅🔒 | Verifies inbound account-webhook calls ([06](06-api-and-contracts.md)). |

### Admin UI ([08](08-admin-ui.md))

| Variable | Default | Req? | Note |
|---|---|---|---|
| `ADMIN_USER` | `admin` | ⚙️ | Login for `/admin`. |
| `ADMIN_PASSWORD` | — | ✅🔒 | Stored/compared as a hash; set a strong value (admin is on the public port). |
| `SESSION_SECRET` | — | ✅🔒 | Signs the admin session cookie. |

### Media ([03 · Media](03-content-and-data.md#media))

| Variable | Default | Req? | Note |
|---|---|---|---|
| `MEDIA_DIR` | `./data/media` | ⚙️ | Local dir the binary serves at `/media/...` (uploads land here). Persistent volume in prod. |
| `MEDIA_BASE_URL` | `` (= app base) | ⚙️ | Absolute base for `kb_assets.url`; leave empty to use the app's own `/media`. Set to an object-store/CDN base for video. |

### Observability (OTel — [04](04-service-and-deployment.md#observability))

| Variable | Default | Req? | Note |
|---|---|---|---|
| `OTEL_ENABLED` | `false` | ⚙️ | No-op tracer when false. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | ⚙️ | OTLP HTTP → Jaeger. |
| `OTEL_SERVICE_NAME` | `xpayment-crm` | ⚙️ | |
| `OTEL_SAMPLE_RATE` | `1.0` | ⚙️ | `0.0`–`1.0`. |

## Secrets handling

- **Secret (🔒):** `LLM_API_KEY`, `CHATWOOT_API_TOKEN`, `CHATWOOT_WEBHOOK_SECRET`, `ADMIN_PASSWORD`, `SESSION_SECRET`, `METRICS_TOKEN`.
- Keep them only in the gitignored `.env` (dev) or injected by the host/secret manager (prod). Never in `.env.example`/`.env.remote`.
- `.env.example` lists **every** key with empty/placeholder values.

## Open questions

- **Model choice** — confirm the `LLM_MODEL` default after eval results ([07](07-testing-and-evals.md)); note it picks the upstream provider (compliance).
- **DB & media durability** — confirm `DB_PATH` and `MEDIA_DIR` sit on a backed-up persistent volume in prod ([04 · Backups](04-service-and-deployment.md#backups--tls)).
- **Admin exposure** — public-port + TLS + IP allowlist vs VPN-only ([08](08-admin-ui.md)).
