# 04 · Service & Deployment

How the **standalone, full-stack** service (Decisions 2, 12, 13) is shaped, built, and run. `xpayment-crm` is one Go binary: the **brain** (Chatwoot webhook) + the **admin UI** + an **embedded SQLite** store, all on one port, calling the LLM via **OpenRouter**. Decisions/layout are in [README.md](README.md); env vars in [05-configuration.md](05-configuration.md); the HTTP surface in [06-api-and-contracts.md](06-api-and-contracts.md). Reuse the main `xpayment` repo's conventions (paths under `…/yessaliyev/xpayment/`).

> **Docs-only round.** The snippets here are **illustrations**, not committed build files. Pin versions and verify upstream images before relying on them.

---

## Repo layout

Hexagonal; the UI and store ship inside the binary.

```
xpayment-crm/
  cmd/main.go                 # start HTTP (webhook + /admin + /media), load snapshot, graceful shutdown
  internal/
    domain/                   # Draft, Message, ChatID, Media, Snapshot … (stdlib only)
    usecase/assistant/        # HandleMessage + ports (ContentSource, ChatwootReader/Writer, Drafter, Prices, Catalog)
    usecase/admin/            # config/KB CRUD + draft/publish/rollback (the UI's service layer)
    infrastructure/
      sqlite/                 # store + migrations (use a PURE-GO driver — modernc.org/sqlite — to keep CGO_ENABLED=0)
      chatwoot/               # ChatwootReader + ChatwootWriter (REST adapter, 06)
      llm/                    # Drafter — OpenRouter (OpenAI-compatible) client (LLM_*)
      config/                 # Config + getEnv (05)
    ports/http/
      webhook.go              # POST /v1/assistant/webhook/chatwoot
      admin/                  # server-rendered handlers (Go html/template + htmx)
      templates/  static/     # *.html + htmx/css — embedded via embed.FS
  migrations/                 # SQLite schema (//go:embed)
  Dockerfile  docker-compose.yml  Makefile  .env.example  .mockery.yml
  docs/
```

---

## Dockerfile

Multi-stage like `xpayment/Dockerfile`, but **no git** (no content repo) and a **pure-Go SQLite** driver so the static `CGO_ENABLED=0` build still works. Templates/static are embedded, so the runtime image carries no web assets.

```dockerfile
# Build stage
FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/main.go   # pure-Go sqlite ⇒ no CGO

# Deploy stage
FROM alpine:3.19 AS deploy
RUN apk add --no-cache tzdata ca-certificates wget   # ca-certificates for OpenRouter TLS; wget for healthcheck
RUN adduser -S -u 1001 appuser
ENV TZ=Asia/Almaty
WORKDIR /app
COPY --from=build /app/main ./main
RUN mkdir -p /app/data && chown appuser /app/data    # SQLite + media live here (mount a volume)
EXPOSE 8080
USER appuser
ENTRYPOINT ["./main"]
```

`DB_PATH` (`/app/data/brain.db`) and `MEDIA_DIR` (`/app/data/media`) live on a **mounted volume** so config/KB survive restarts.

---

## Local stack (docker-compose)

The main repo's compose runs *one* product; the brain's local stack stands up **three** (Chatwoot, Evolution, the brain) plus a tunnel so Chatwoot can reach the brain's webhook. The brain itself needs **no DB server** — SQLite is a file on its volume.

```yaml
# illustrative — pin image versions, set secrets via .env, verify upstream envs
services:
  chatwoot-postgres: { image: postgres:16-alpine, volumes: [chatwoot_pg:/var/lib/postgresql/data] }
  chatwoot-redis:    { image: redis:7-alpine }
  chatwoot:
    image: chatwoot/chatwoot:latest        # pin a real tag
    depends_on: [chatwoot-postgres, chatwoot-redis]
    env_file: [.env]
    ports: ["3000:3000"]

  evolution-postgres: { image: postgres:16-alpine, volumes: [evo_pg:/var/lib/postgresql/data] }
  evolution-redis:    { image: redis:7-alpine }
  evolution:
    image: atendai/evolution-api:latest     # pin a real tag
    depends_on: [evolution-postgres, evolution-redis]
    env_file: [.env]
    ports: ["8081:8080"]

  # ── Brain = xpayment-crm (standalone: webhook + admin + SQLite) ──────────
  brain:
    build: { context: ., dockerfile: Dockerfile }
    env_file: [.env]                         # LLM_*, CHATWOOT_*, ADMIN_*, DB_PATH, MEDIA_DIR …
    volumes:
      - brain_data:/app/data                 # SQLite + uploaded media (persistent)
    ports: ["8080:8080"]                     # webhook + /admin + /media
    healthcheck:
      test: ["CMD","wget","--no-verbose","--tries=1","-O","/dev/null","http://localhost:8080/health"]
      interval: 15s; timeout: 3s; start_period: 10s; retries: 3
    restart: unless-stopped
    stop_grace_period: 25s

  # Chatwoot's account webhook must reach the brain over public HTTPS in local dev.
  tunnel:
    image: cloudflare/cloudflared:latest     # or ngrok
    command: tunnel --url http://brain:8080
volumes: { chatwoot_pg: , evo_pg: , brain_data: }
```

**Wiring order:** Chatwoot up → create account + agent token → Evolution up + configure its Chatwoot integration ([01](01-infrastructure.md#1-evolution--chatwoot)) → brain up → set the **tunnel URL** as Chatwoot's account webhook ([01](01-infrastructure.md#2-the-two-webhook-kinds--do-not-conflate)) → open `/admin` (over the tunnel or locally) to seed persona/KB/prices.

---

## Snapshot load & hot-reload

The brain never queries SQLite on the hot path. At **startup** it runs migrations, then loads the **published** config + active KB/prices into an immutable in-memory `Snapshot` ([03 · snapshot](03-content-and-data.md#the-in-memory-snapshot)). When an admin **publishes** ([08](08-admin-ui.md)), the service validates the draft, atomically swaps the snapshot pointer **in-process** (no restart, no external webhook), and keeps the old snapshot if validation fails. There is no content repo and no reload webhook — reload is an internal call triggered by publish.

---

## Startup & shutdown

Mirror `xpayment/cmd/main.go`, minus the Kaspi workers:
1. `signal.NotifyContext` for `SIGINT`/`SIGTERM`.
2. Build config ([05](05-configuration.md)); init `slog` + OTel.
3. Open SQLite (`DB_PATH`), run migrations, **load the snapshot** (refuse to boot if the published config is invalid).
4. Build the container: store → adapters (`chatwoot`, `llm`) → `assistant` + `admin` usecases → HTTP handlers (webhook + admin + media).
5. Start the chi server (one port; read/write timeouts ~30s, 1 MB body limit) in a goroutine.
6. On signal: `srv.Shutdown(ctx)` (~10s drain). `stop_grace_period: 25s` gives headroom.

No background workers in v1.

---

## Observability

Reuse the main repo's setup: `GET /health`, `GET /ready`; `GET /metrics` (Prometheus, `METRICS_TOKEN`-gated, RED metrics from chi middleware); `log/slog` JSON + OTel `TraceHandler`; OTel→OTLP→Jaeger toggled by `OTEL_ENABLED`. Log per message: `chatID`, port-call latencies, LLM tokens, `confidence`, `escalate`, dropped `asset_refs`, leftover price tokens; and per publish: version + validation result.

---

## Deployment

One **stateless-compute** binary with a **stateful volume** (SQLite + media). **Recommended:** a single container on a VPS — `docker compose up -d brain` with `restart: unless-stopped` behind a reverse proxy ([Backups & TLS](#backups--tls)). Upgrades: build, `docker compose up -d --no-deps brain` (a few seconds of webhook downtime is fine — Chatwoot retries; humans reply manually meanwhile). The main repo's blue-green pattern is available if zero-downtime is later needed — overkill at ~100 leads.

CI is in [07-testing-and-evals.md](07-testing-and-evals.md#ci).

---

## Backups & TLS

- **Backups:** two things hold data now. **Chatwoot's Postgres** is the system of record for conversations/contacts/profile — nightly `pg_dump` + offsite, test restores. **The brain's volume** (`DB_PATH` SQLite + `MEDIA_DIR`) holds your authored config/KB/prices/media — back it up too (a copied `.db` file + the media dir). *(Cross-links [01 · Operations](01-infrastructure.md#5-operations).)*
- **TLS / reverse proxy:** **required** — both Chatwoot's webhook *and the public `/admin` UI* go to the brain over the internet. Put Caddy/nginx in front (Caddy auto-certs); terminate TLS there; add an **IP allowlist + rate-limit on `/admin`** ([08](08-admin-ui.md#auth-same-service-login)). In local dev the tunnel provides HTTPS.

---

## Open questions

- **SQLite driver** — pure-Go `modernc.org/sqlite` (keeps `CGO_ENABLED=0`, recommended) vs. `mattn/go-sqlite3` (CGO).
- **Volume durability/backup cadence** for `DB_PATH` + `MEDIA_DIR`.
- **Deploy target / image tags** — single VPS + compose (recommended); pin Chatwoot/Evolution tags.
