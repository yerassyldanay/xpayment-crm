# Running locally

Two tiers. Start with **Tier 1** while developing the brain; use **Tier 2** when you
need the real Chatwoot + Evolution + WhatsApp loop.

Prerequisites: Go 1.25+, Docker + Docker Compose, and an **OpenRouter API key**
(https://openrouter.ai). All commands run from the repo root.

---

## Tier 1 — just the Go app (fast inner loop, no Docker)

You do **not** need Chatwoot or Evolution to build, edit config, and test the brain's
output: the **admin UI** and **Playground** only need an LLM key. The webhook just sits
idle until a real Chatwoot points at it.

```bash
cp .env.example .env
# edit .env and set, at minimum:
#   LLM_API_KEY=<your OpenRouter key>
#   ADMIN_PASSWORD=admin            SESSION_SECRET=$(openssl rand -hex 32)
#   CHATWOOT_BASE_URL=http://localhost   CHATWOOT_ACCOUNT_ID=1
#   CHATWOOT_API_TOKEN=dummy        CHATWOOT_INBOX_ID=1   CHATWOOT_WEBHOOK_SECRET=dummy
# (the CHATWOOT_* values are placeholders here — Playground never calls Chatwoot)

make run          # or: go run ./cmd/main.go
```

Open **http://localhost:8080/admin** (login `admin` / your `ADMIN_PASSWORD`):
- **Persona / Knowledge / Media / Prices** — edit the draft, then **Publish** on the Dashboard.
- **Playground** — type a customer message (optionally a profile JSON) and hit Run; it calls
  OpenRouter and shows the post-processed draft (prices injected, media resolved). Nothing is sent.

`make test` / `make build` validate the code. This is the loop for everything except the
live WhatsApp/Chatwoot wiring.

---

## Tier 2 — full stack (Chatwoot + Evolution + brain) in Docker

Chatwoot and Evolution **reuse your existing Postgres/Redis** (no bundled DBs); the brain
uses its own SQLite. Because the native Postgres listens on `127.0.0.1` only, **chatwoot and
the brain use host networking** (`network_mode: host`) — they reach the native PG/Redis and
each other over `localhost` (Chatwoot ↔ brain at `localhost:3000` / `localhost:8080`). No
public tunnel needed. URLs from your browser: Chatwoot `:3000`, brain `:8080`. (Local
Evolution is optional — production WhatsApp uses the remote Evolution.)

> Pin real image tags in `docker-compose.yml` (`chatwoot/chatwoot`, `atendai/evolution-api`)
> — `latest` can change behavior. The exact Evolution↔Chatwoot setup steps vary by version;
> verify against the version you run.

### 1. Secrets + env

```bash
cp .env.example .env
# fill the brain section (LLM_API_KEY, ADMIN_PASSWORD, SESSION_SECRET, CHATWOOT_WEBHOOK_SECRET=$(openssl rand -hex 16))
# and, for the stack, the two secrets:
#   SECRET_KEY_BASE=$(openssl rand -hex 64)
#   AUTHENTICATION_API_KEY=$(openssl rand -hex 16)
# Set the brain's view of Chatwoot (host networking -> localhost):
#   CHATWOOT_BASE_URL=http://localhost:3000
# Fill the "Existing Postgres/Redis to REUSE" block (CHATWOOT_POSTGRES_*, CHATWOOT_REDIS_URL,
#   EVOLUTION_DATABASE_CONNECTION_URI, EVOLUTION_CACHE_REDIS_URI) — see step 2 for the DB setup.
# Leave CHATWOOT_ACCOUNT_ID / CHATWOOT_API_TOKEN / CHATWOOT_INBOX_ID blank for now — you fill them after step 3.
```

### 2. Prepare the existing Postgres, then bring up Chatwoot + Evolution

**One-time setup on your existing Postgres** (Chatwoot must NOT share an app's DB — it runs
destructive migrations). Run as a superuser; the PG must have the **pgvector** binary:

```sql
CREATE ROLE chatwoot  LOGIN PASSWORD '...';  CREATE DATABASE chatwoot  OWNER chatwoot;
CREATE ROLE evolution LOGIN PASSWORD '...';  CREATE DATABASE evolution OWNER evolution;
\c chatwoot
-- Pre-create as superuser: the chatwoot role is NOT a superuser, but Chatwoot's
-- schema enables these. Pre-creating makes its enable_extension a no-op.
CREATE EXTENSION IF NOT EXISTS vector;              -- Chatwoot requires pgvector
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

> **Reachability:** the containers reach the existing PG/Redis via `host.docker.internal`
> (mapped to the docker host gateway). A service published on a host port (e.g. the local
> `kaspi-service-db` on 54321, `kaspim-redis` on 6379) is reachable as-is. A PG bound only to
> `127.0.0.1` must first open `listen_addresses`/`pg_hba.conf` to the gateway. Pick a free
> Redis DB index (locally 0/1/6 are in use). Redis must be `noeviction` so Sidekiq jobs aren't dropped.

```bash
# one-time Chatwoot DB prepare (migrations + seed) — runs against your existing Postgres
docker compose run --rm chatwoot bundle exec rails db:chatwoot_prepare

# now the apps
docker compose up -d chatwoot chatwoot-sidekiq evolution
docker compose logs -f chatwoot   # wait for "Listening on http://0.0.0.0:3000"
```

### 3. Configure Chatwoot

1. Open **http://localhost:3000**, create the first admin account (signup is enabled).
2. Note your **Account ID** (the number in the dashboard URL `/app/accounts/<ID>/...`).
3. **Profile → Access Token** → copy the **agent API token** → put it in `.env` as `CHATWOOT_API_TOKEN`,
   and set `CHATWOOT_ACCOUNT_ID`.
4. **Settings → Custom Attributes** → pre-define the contact attributes the profile uses
   (`business_type`, `monthly_volume_tenge`, `current_payment_method`, `cashiers_needed`,
   `technical_level`, `urgency`, `interested_tariff`, `preferred_language`, `main_objection`,
   `fit_tariff`, `notes`). See `docs/03`.

### 4. Connect WhatsApp via Evolution and bind it to Chatwoot

Evolution's API is at `http://localhost:8081` (header `apikey: <AUTHENTICATION_API_KEY>`).
Create an instance, enable its native Chatwoot integration (pointing at `http://chatwoot:3000`,
your account id + token), then scan the QR with the WhatsApp number. Example shape (verify
the exact fields for your Evolution version):

```bash
# create an instance
curl -s http://localhost:8081/instance/create -H "apikey: $AUTHENTICATION_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"instanceName":"xpayment","integration":"WHATSAPP-BAILEYS"}'

# enable the Chatwoot integration on that instance
curl -s http://localhost:8081/chatwoot/set/xpayment -H "apikey: $AUTHENTICATION_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"accountId":"<ACCOUNT_ID>","token":"<CHATWOOT_API_TOKEN>",
       "url":"http://chatwoot:3000","signMsg":true,"reopenConversation":true,
       "conversationPending":false,"importContacts":true,"importMessages":true}'

# get the QR to scan with the WhatsApp phone
curl -s http://localhost:8081/instance/connect/xpayment -H "apikey: $AUTHENTICATION_API_KEY"
```

This auto-creates an **API inbox** in Chatwoot. Open **Settings → Inboxes**, find it, and put
its numeric id in `.env` as `CHATWOOT_INBOX_ID`.

### 5. Point Chatwoot's account webhook at the brain

**Settings → Integrations → Webhooks → Add** a webhook subscribed to **`message_created`**:

```
http://localhost:8080/v1/assistant/webhook/chatwoot?secret=<CHATWOOT_WEBHOOK_SECRET>
```

(The brain verifies the secret from this query param. Use the same value you put in `.env`.)

### 6. Start the brain and seed its config

```bash
docker compose up -d --build brain
docker compose logs -f brain      # "snapshot loaded" then "listening"
```

Open **http://localhost:8080/admin**, edit Persona/Knowledge/Media/Prices, and **Publish**.
(The seed gives you a working starting config out of the box.)

### 7. Test the loop

Message the WhatsApp number from another phone. You should see:
`customer WhatsApp → Evolution → Chatwoot conversation → account webhook → brain → private-note
draft` appear in the Chatwoot conversation. A human reads the draft and sends the real reply.
The brain also merges the lead profile onto the contact and applies a status label.

### Teardown

```bash
docker compose down            # keep data (volumes)
docker compose down -v         # wipe everything (fresh start)
```

---

## Troubleshooting

- **Brain exits on boot with "missing required env"** — fill every required key in `.env`
  (`LLM_API_KEY`, all `CHATWOOT_*`, `ADMIN_PASSWORD`, `SESSION_SECRET`). For Tier 1 use placeholder
  `CHATWOOT_*` values.
- **No draft appears** — check `docker compose logs brain`. Common causes: webhook not firing
  (wrong account-webhook URL or event), `CHATWOOT_INBOX_ID` mismatch (brain ignores other inboxes),
  or the webhook secret not matching the `?secret=` query param.
- **Chatwoot won't start** — ensure `SECRET_KEY_BASE` is set and you ran `db:chatwoot_prepare` once.
- **Brain ↔ Chatwoot auth fails** — `CHATWOOT_API_TOKEN` must be a valid agent token and
  `CHATWOOT_BASE_URL=http://localhost:3000` (chatwoot + brain share the host network).
- **Chatwoot `db:chatwoot_prepare` fails with "permission denied to create extension"** — the
  `chatwoot` role isn't a superuser; pre-create `vector` / `pg_stat_statements` / `pg_trgm` in
  the `chatwoot` DB as a superuser (step 2), then re-run.
- **Chatwoot can't reach Postgres** — the native PG listens on `127.0.0.1`; chatwoot must run
  with `network_mode: host` and `CHATWOOT_POSTGRES_HOST=127.0.0.1`.
