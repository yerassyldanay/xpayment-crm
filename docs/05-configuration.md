# 05 · Configuration

This file is the **canonical home for every environment variable** the brain reads (just as [03-content-and-data.md](03-content-and-data.md) is the home for the content-repo file shapes). Other docs reference these names and never restate the catalog. Decision context is in [README.md](README.md); how config is consumed at boot is in [04-service-and-deployment.md](04-service-and-deployment.md).

## Loading pattern

Copy `xpayment/internal/infrastructure/config/config.go`:

- A single `Config` struct composed of nested structs (`Chatwoot`, `Anthropic`, `Content`, `OTel`, …).
- A `getEnv(key, fallback)` helper that trims and falls back to a default; **required** values are validated at startup and the service refuses to boot if missing.
- `loadDotEnv(".env")` at startup, with the file split:
  - **`.env`** — local dev, contains secrets, **gitignored**.
  - **`.env.example`** — committed template, every key present with placeholder/empty values.
  - **`.env.remote`** — committed production overrides, **no secrets** (injected by the host/orchestrator).

The brain has **no database** (Decision 2) — there is no `DATABASE_URL`. Its persona/KB/prices/media come from the content repo (the `Content` group below).

```go
// shape only — see xpayment/internal/infrastructure/config/config.go
type Config struct {
    Env       string        // APP_ENV
    LogLevel  string        // LOG_LEVEL
    HTTPAddr  string        // HTTP_ADDR
    Chatwoot  ChatwootConfig
    Anthropic AnthropicConfig
    Content   ContentConfig
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

*(No `ALLOWED_ORIGINS`/CORS in v1 — the only HTTP endpoints are server-to-server webhooks; there is no browser-facing admin API.)*

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

### Content repo (the config source — Decision 2; [03](03-content-and-data.md), [08](08-admin-ui.md))

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `CONTENT_REPO_PATH` | `./xpayment-content` | ✅ | `Content.Path` | Local checkout the brain loads the snapshot from. |
| `CONTENT_REPO_URL` | `` | ⚙️ | `Content.RepoURL` | If set, the brain `git clone`/`pull`s this on boot and reload. |
| `CONTENT_REPO_BRANCH` | `main` | ⚙️ | `Content.Branch` | Branch to track (publish = merge here). |
| `KB_MEDIA_BASE_URL` | — | ✅ | `Content.MediaBaseURL` | Static base that `media.json` `file` paths resolve against (Git LFS / object storage / served `media/`). |
| `RELOAD_WEBHOOK_SECRET` | — | ✅🔒 | `Content.ReloadSecret` | Verifies the GitHub push webhook that triggers reload ([06](06-api-and-contracts.md#github-reload-webhook)). |

### Observability (OTel — [04](04-service-and-deployment.md#observability))

| Variable | Default | Req? | Struct field | Note |
|---|---|---|---|---|
| `OTEL_ENABLED` | `false` | ⚙️ | `OTel.Enabled` | When false, a no-op tracer is installed. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | ⚙️ | `OTel.Endpoint` | OTLP HTTP → Jaeger. |
| `OTEL_SERVICE_NAME` | `xpayment-copilot` | ⚙️ | `OTel.ServiceName` | Distinguish from the main `xpayment` service. |
| `OTEL_SAMPLE_RATE` | `1.0` | ⚙️ | `OTel.SampleRate` | `0.0`–`1.0`. |

## Secrets handling

- **Secret (🔒):** `ANTHROPIC_API_KEY`, `CHATWOOT_API_TOKEN`, `CHATWOOT_WEBHOOK_SECRET`, `RELOAD_WEBHOOK_SECRET`, `METRICS_TOKEN`.
- Keep them only in the gitignored `.env` (dev) or injected by the host/secret manager (prod). Never in `.env.example` or `.env.remote`.
- `.env.example` must list **every** key above with empty/placeholder values so a new engineer sees the full surface at a glance.

## Open questions

- **Content delivery** — mount the content repo as a read-only volume vs. `git clone` on boot ([04](04-service-and-deployment.md#content-checkout--reload)).
- **Media storage** — Git LFS vs object storage for video; `KB_MEDIA_BASE_URL` points at whichever ([03](03-content-and-data.md#open-questions)).
- **Model choice** — confirm `ANTHROPIC_MODEL` default after eval results ([07](07-testing-and-evals.md)).
