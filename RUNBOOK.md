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

# now the apps (Evolution is NOT started here — see note below)
docker compose up -d chatwoot chatwoot-sidekiq
docker compose logs -f chatwoot   # wait for "Listening on http://0.0.0.0:3000"
```

> **Evolution is opt-in.** This compose's `evolution` service is behind the `evolution` profile and
> does **not** start with a plain `docker compose up` — most setups already run a separate Evolution
> (e.g. the `evolution/` repo on host `:9700`, or a remote one) and a duplicate would compete for the
> same WhatsApp number. Only if you want THIS one:
> `docker compose --profile evolution up -d evolution` — and first set `EVOLUTION_DATABASE_CONNECTION_URI`
> / `EVOLUTION_CACHE_REDIS_URI` to the real services via `host.docker.internal` (e.g.
> `host.docker.internal:54321` for the dockerized Postgres), **not** `127.0.0.1` (the container's own
> loopback), and create the `evolution` role + DB on that Postgres first.

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

Use whatever Evolution actually runs your number — e.g. the standalone stack at
`http://localhost:9700` (header `apikey: <AUTHENTICATION_API_KEY>`). Create an instance and scan
the QR, then enable its native Chatwoot integration. **Two networking gotchas:**

- `url` must be reachable **from the Evolution container** → use `http://host.docker.internal:3000`
  (the container has the `host-gateway` mapping), **not** `http://chatwoot:3000` (only resolves if
  Evolution shares Chatwoot's compose network) and **not** `127.0.0.1` (the container's own loopback).
- `nameInbox` should match the inbox the brain targets so `CHATWOOT_INBOX_ID` stays valid; with
  `autoCreate:true` Evolution reuses an inbox of that name or creates it.

```bash
API=http://localhost:9700   # the Evolution that hosts your WhatsApp number

# create + connect the instance, then scan the returned QR
curl -s "$API/instance/create" -H "apikey: $AUTHENTICATION_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"instanceName":"xpayment","integration":"WHATSAPP-BAILEYS"}'
curl -s "$API/instance/connect/xpayment" -H "apikey: $AUTHENTICATION_API_KEY"

# enable the Chatwoot integration on that instance
curl -s -X POST "$API/chatwoot/set/xpayment" -H "apikey: $AUTHENTICATION_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"accountId":"<ACCOUNT_ID>","token":"<CHATWOOT_API_TOKEN>",
       "url":"http://host.docker.internal:3000","signMsg":true,"reopenConversation":true,
       "conversationPending":false,"nameInbox":"xpayment-test","autoCreate":true,
       "importContacts":true,"importMessages":true,"daysLimitImportMessages":7}'

# confirm it took
curl -s "$API/chatwoot/find/xpayment" -H "apikey: $AUTHENTICATION_API_KEY"   # expect enabled:true
```

This binds (or auto-creates) an **API inbox** in Chatwoot. Open **Settings → Inboxes**, note its
numeric id, and set `CHATWOOT_INBOX_ID` in `.env` to match, then restart the brain
(`docker compose up -d brain`).

> Importing messages backfills history into Chatwoot, which fires `message_created` webhooks and can
> make the brain draft on every imported message. To avoid that, `docker compose stop brain` before
> enabling import and `docker compose up -d brain` after it settles.

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

### WhatsApp shows "Waiting for this message"

This is a WhatsApp end-to-end-encryption / Baileys **session-decryption** failure inside Evolution —
**not** the brain (the brain only posts private-note drafts; it never sends to WhatsApp). The usual
cause is a drifted `atendai/evolution-api:latest` whose bundled Baileys no longer matches WhatsApp's
current web protocol.

> ⚠️ The Evolution that actually serves the linked WhatsApp number runs from the **separate
> `evolution/` repo** (container `evolution-api`, host port **9700**). The version pin below is also
> applied there for a real fix; `docker-compose.yml` here pins the optional Tier-2 `evolution` service
> (port 8081). Run the API calls against whichever Evolution your number is linked to.

Fix:

1. **Pin / upgrade the image** to a current stable tag (done here: `atendai/evolution-api:v2.2.3`),
   then pull and recreate:
   ```bash
   docker compose pull evolution && docker compose up -d evolution
   ```
2. **Re-link the instance** (resets the stale session keys). Against the running Evolution
   (`API=http://localhost:9700` for the separate stack, or `:8081` for this compose;
   `KEY=$AUTHENTICATION_API_KEY`):
   ```bash
   curl -X DELETE "$API/instance/logout/xpayment" -H "apikey: $KEY"
   curl -X DELETE "$API/instance/delete/xpayment" -H "apikey: $KEY"
   # recreate + re-enable the Chatwoot integration (see step 4 above), then:
   curl -s "$API/instance/connect/xpayment" -H "apikey: $KEY"   # scan the returned QR
   ```
3. **Keep the phone online** — Baileys is a linked device and needs the phone reachable to sync keys.
4. **Fallback** if auto-detect keeps failing: set `CONFIG_SESSION_PHONE_VERSION` to a current
   WhatsApp Web version, and `LOG_BAILEYS=debug` to inspect decryption errors in the Evolution logs.

### Agent replies in Chatwoot don't reach WhatsApp (outbound)

Inbound works but pressing **Send** in Chatwoot doesn't deliver to WhatsApp. Outbound flows
Chatwoot → the **inbox's `webhook_url`** → Evolution → Baileys. The webhook path segment must equal
the **Evolution instance name**, or Evolution drops it ("instance not found").

- Inspect/fix the inbox webhook:
  ```bash
  TOK=$CHATWOOT_API_TOKEN
  curl -s -H "api_access_token: $TOK" http://localhost:3000/api/v1/accounts/2/inboxes/1   # check webhook_url
  curl -s -X PATCH http://localhost:3000/api/v1/accounts/2/inboxes/1 \
    -H "api_access_token: $TOK" -H 'Content-Type: application/json' \
    -d '{"channel":{"webhook_url":"http://localhost:9700/chatwoot/webhook/<INSTANCE_NAME>"}}'
  ```
  (This mismatch happens when `nameInbox` ≠ the instance name. To prevent it on a fresh setup, set
  `nameInbox` equal to the instance name in `/chatwoot/set`.)
- Confirm with `docker logs -f evolution-api` while sending: you should see the outbound send, not
  an instance-not-found / logout error.

### Reply fails: "the contact is not a valid Whatsapp number" (`@lid`)

The send now reaches Evolution but is rejected because the contact's WhatsApp identifier is a
**LID** (`<digits>@lid`, e.g. `5540591734861@lid`) instead of a real phone JID
(`<phone>@s.whatsapp.net`). A LID is WhatsApp's hidden/linked id — **not a dialable number** — so
Evolution can only send if it holds a **LID → phone mapping**, which it builds while the instance
**fully syncs contacts/app-state**.

- **Verify scope:** reply to a contact with a real `@s.whatsapp.net` JID (e.g. `77058686509`) — it
  should deliver. If only `@lid` contacts fail, this is the issue.
- **Fix:** re-link the instance and let it finish syncing on a stable session (see "Waiting for this
  message" above for the logout→delete→recreate→connect steps); keep the phone online. A completed
  sync (no `init queries` timeout / `failed to sync state` in the logs) populates the LID→phone
  mappings, after which previously-`@lid` contacts you've chatted with become sendable.
- **Avoid bulk history import** — `importMessages:true` backfills many LID-only contacts (from
  historical/group chats) that often have no phone mapping and can't be replied to. Prefer live 1:1
  inbound. Some LIDs (privacy-hidden / community senders) may never resolve to a number.

### Outbound media (images) don't reach WhatsApp, but text does

Evolution sends an attachment by downloading it from Chatwoot's `data_url`, which is built from
Chatwoot's **`FRONTEND_URL`**. With `FRONTEND_URL=http://localhost:3000`, that URL points at the
Evolution container itself (its own loopback), so the download fails — text sends, media doesn't.
Set `FRONTEND_URL` to a host the **Evolution container can reach and you can still open in a
browser** — i.e. the host's **LAN IP** (`http://<LAN_IP>:3000`) — then
`docker compose up -d chatwoot chatwoot-sidekiq`. (`host.docker.internal` works for the container but
not for a browser on Linux.)
