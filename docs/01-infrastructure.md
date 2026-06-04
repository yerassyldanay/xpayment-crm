# 01 · Infrastructure — Evolution + Chatwoot wiring and operations

This file covers the **channel/transport layer**: how WhatsApp reaches the shared inbox, how the inbox reaches the brain, and how to run it. The architecture, decisions, and ownership table live in [README.md](README.md); this file assumes them (especially Decisions 1, 3, 5, 6, 11).

> **Verify, don't assume.** Chatwoot and Evolution are third-party systems whose exact field names, settings labels, and behaviors change between versions. Treat the specifics below as the intended wiring, and confirm each against the version you deploy before relying on it. Items that most need confirmation are flagged inline.

---

## 1. Evolution ↔ Chatwoot

Evolution API ships a **native Chatwoot integration**. You configure it once on the Evolution instance, pointing at your Chatwoot:

- **Chatwoot account id** — the numeric account the conversations belong to.
- **Chatwoot API token** — a bot/agent access token Evolution uses to call Chatwoot's REST API.
- **Chatwoot URL** — the base URL of your Chatwoot deployment.
- **Import settings** — typically: import existing contacts, import existing messages, auto-reopen resolved conversations on a new message, and whether new conversations start in `pending` or `open`. *(Verify the exact toggle names for your Evolution version.)*

When enabled, Evolution **auto-creates an "API" channel inbox** in Chatwoot and binds the WhatsApp session to it. From then on:

- **Incoming:** customer's WhatsApp message → Evolution (WhatsApp Web via Baileys) → Evolution **calls Chatwoot's REST API** to create/append the conversation and message under that inbox.
- **Outgoing:** an agent sends a reply in Chatwoot → Chatwoot fires its **inbox webhook** → Evolution receives it → Evolution sends it over WhatsApp.

The brain is **not** in this loop. It never calls Evolution and Evolution never calls it (Decision 3). The brain only reads from and writes to Chatwoot.

```
Customer WhatsApp ⇄ Evolution ⇄ (Chatwoot REST / Chatwoot inbox webhook) ⇄ Chatwoot
                                                                              ⇅  (account webhook + REST)
                                                                            Go Brain
```

---

## 2. The two webhook kinds — do not conflate

There are **two separate webhooks**, configured in different places, serving different directions. Confusing them is the most common wiring mistake.

| | **Inbox webhook** | **Account-level webhook** |
|---|---|---|
| Set where | On the WhatsApp/API **inbox** (auto-configured by the Evolution integration) | On the Chatwoot **account** (Settings → Integrations → Webhooks) |
| Points to | **Evolution** | **The brain** |
| Carries | Outgoing agent replies (so Evolution can deliver them to WhatsApp) | Event notifications, including `message_created` |
| Owned by | The Evolution integration — **leave it alone** | You — this is how the brain hears about new messages (Decision 11) |

They do not collide: the inbox webhook moves replies *out* to WhatsApp; the account webhook notifies the brain of *new activity*. The brain subscribes to the account webhook and filters for the events it cares about (an incoming `message_created` on the WhatsApp inbox).

> **Verify:** that your Chatwoot version delivers `message_created` on the account webhook with enough payload to identify the conversation and message type (incoming vs outgoing vs private note). The brain must ignore its own private notes and outgoing agent messages to avoid loops — see [02-assistant-brain.md](02-assistant-brain.md#what-the-brain-ignores).

---

## 3. Brain ↔ Chatwoot

The brain interacts with Chatwoot in exactly two directions, both through the `ChatwootReader` / `ChatwootWriter` ports (Decision 10; signatures in [02-assistant-brain.md](02-assistant-brain.md#ports)):

**Inbound (Chatwoot → brain):** the account webhook POSTs `message_created` to the brain's endpoint. Payloads should be **signed**; the brain verifies the signature/secret before doing any work and responds quickly (acknowledge, then process). *(Verify the signing mechanism available in your Chatwoot version; if none, restrict the endpoint by network/secret header.)*

**Outbound (brain → Chatwoot), via REST:**
- **Private note** — create a message with `private: true` on the conversation. This is the draft the agent reads. See §4 for why it stays internal.
- **Contact custom attributes** — merge the extracted profile onto the contact (Decision 9). These attributes **must be pre-defined** in Chatwoot settings before they can be written; define them in Phase 1.
- **Labels** — apply the suggested status as a conversation label.
- **(Phase 3) Outgoing message** — create a normal outgoing message (`private: false`) for confidence-gated auto-send. This still goes *through Chatwoot*, never Evolution directly (Decision 6).

### Why private notes are safe

A **private note** is internal to Chatwoot. Evolution's integration only forwards **real agent replies** (outgoing, non-private messages) to WhatsApp; it ignores private notes. So a draft posted as a private note is visible to your team but **never reaches the customer** until a human composes and sends an actual reply.

> **Recommended one-line test (Phase 1):** post a private note to a live test conversation via the Chatwoot API and confirm it appears in the agent view but does **not** arrive on the test WhatsApp number. This validates the single most important safety property of suggest-only mode before any brain code exists.

---

## 4. AgentBot — the alternative, and why we don't lead with it

Chatwoot's **AgentBot** is a first-class integration primitive: you register a bot with a webhook URL and assign it to an inbox; Chatwoot routes new conversations to the bot first.

It is a legitimate way to wire a brain in, **but** it is designed for a *handoff* model: while the bot "handles" a conversation it sits in a bot-managed **pending** state, and once a human takes over (handoff), the bot typically goes quiet for the rest of that conversation. For a **persistent copilot** that should keep drafting on *every* message — including deep into a human-led conversation — that handoff semantics works against us.

Therefore we prefer the **account-level webhook** (Decision 11): it fires on every `message_created` regardless of who is "handling" the conversation, so the brain can keep proposing drafts throughout. AgentBot remains documented as a fallback if account-webhook coverage proves insufficient.

> **Verify:** the exact pending/handoff lifecycle in your Chatwoot version, and whether AgentBot events fire after human handoff. If they do fire continuously in your version, AgentBot becomes viable; confirm before choosing.

---

## 5. Operations

### Self-hosting

Both services are self-hosted and each needs its own **Postgres + Redis**:

- **Chatwoot** — Rails app + Sidekiq workers; Postgres for data, Redis for queues/websockets.
- **Evolution API** — Node service holding the WhatsApp Web session; Postgres/Redis for instance state and message bookkeeping. The session is tied to a scanned WhatsApp number and must stay logged in.

Keep them as separate deployments with separate datastores. The **brain keeps no conversations** (Chatwoot owns those, Decision 2) — its config/KB/prices/media live in an **embedded SQLite** DB inside the service ([03-content-and-data.md](03-content-and-data.md)).

### Ban risk and outbound pacing

Evolution is **unofficial** WhatsApp Web (Baileys). Aggressive or bot-like outbound can get the number **banned**. Two mitigations:

- **Suggest-only is naturally protective** (Decision 6): humans send at human pace, and only to people who messaged first (warm inbound).
- **Before any Phase-3 auto-send**, add **pacing**: per-conversation drip/delay and a **daily send cap**, plus quiet hours. This pacing belongs on the send path (Chatwoot outgoing-message call), since that is the only path the brain can ever drive.

### Session-health alerting

The WhatsApp Web session can drop (phone offline, re-login required, Meta logout). When it does, inbound and outbound silently stop.

> **Verify and wire:** where a dropped session surfaces — Evolution exposes instance/connection state (and can emit connection events); decide where the team watches it (a health check on Evolution's instance status, an alert to a Telegram/ops channel). Do **not** rely on "messages stopped arriving" as your first signal.

### Evolution → Cloud API migration (Phase 3)

The official **WhatsApp Cloud API** removes ban risk and is the documented migration path (Decision 5). Because the brain only ever talks to Chatwoot, the swap is a **transport change behind Chatwoot**, not a brain change:

- Re-point the **same WhatsApp number** from the Evolution/API channel to an official **WhatsApp channel** in Chatwoot.
- Preserve the **idempotency key across the seam.** The message id the brain sees is **Chatwoot's**, not WhatsApp's — design any de-duplication on Chatwoot's ids so it survives the channel switch, or you will double-process at the cutover. (The brain stores no messages, so its only exposure is duplicate webhook deliveries; still, make the handler idempotent on the Chatwoot message id.)
- The Cloud API imposes the **24-hour customer-service window** and pre-approved **message templates** for business-initiated messages outside it — account for this in any proactive/auto-send flows.

> **Verify:** that Chatwoot can re-point an existing number to the official channel **without losing conversation history or contact identity**. Confirm before committing to the migration plan.

### Prompt-cache caveat

The brain caches the large static prompt prefix (persona + KB + media catalog) to cut cost and latency (see [02-assistant-brain.md](02-assistant-brain.md#prompt-assembly)). The cache TTL is **short** (minutes). At ~100 low-frequency leads, two messages rarely fall inside one cache window, so **read-side savings are modest** — keep caching for the latency win and intra-conversation bursts, but **do not budget on large cost reductions** at this volume.

### Backups

Decision 1 makes Chatwoot the **system of record** for every contact, conversation, message, and the lead profile (stored as contact custom attributes). That data only exists in Chatwoot's Postgres — back it up like it matters:

- **Chatwoot Postgres** — scheduled `pg_dump` (e.g. nightly) to offsite storage; **test restores periodically**. This is the irreplaceable database.
- **Brain volume** — the embedded SQLite DB (`DB_PATH`) + `MEDIA_DIR` hold your authored config/KB/prices/media; back up that volume (a copied `.db` + the media dir) ([03](03-content-and-data.md), [04](04-service-and-deployment.md#backups--tls)).
- **Evolution session state** — losing it forces a WhatsApp re-login (re-scan), not data loss.

See the cadence note in [04 · Backups & TLS](04-service-and-deployment.md#backups--tls).

### TLS / reverse proxy

Chatwoot webhooks — and any public access to the brain or Chatwoot — require **HTTPS**:

- Put **Caddy or nginx** in front of Chatwoot and the brain (Caddy issues/renews certs automatically).
- The brain's `POST /v1/assistant/webhook/chatwoot` ([06](06-api-and-contracts.md#webhook-receiver)) must be reachable over HTTPS in production; in local dev the **tunnel** ([04 · local stack](04-service-and-deployment.md#local-stack-docker-compose)) provides the public HTTPS URL.
- Terminate TLS at the proxy; keep the services on a private network.

### Observability (baseline)

Minimum to run with confidence:
- **Brain:** per-message structured logs (chatID, latency of each port call, LLM tokens, `confidence`, `escalate`, dropped `asset_refs`, leftover price tokens), and counters for drafts posted / escalations / errors.
- **Chatwoot/Evolution:** the standard app dashboards plus the session-health signal above.

---

## Open questions

- **Webhook signing.** Does the deployed Chatwoot version sign account-webhook payloads, and with what scheme? If not, what network/secret control protects the brain's endpoint?
- **`message_created` payload completeness.** Is the payload enough to classify message type and identify the conversation, or must the brain always call back to read the thread? (Affects the window fetch in [02](02-assistant-brain.md#context-on-read).)
- **Session-health surface.** Exactly which Evolution endpoint/event the team will alert on for a dropped WhatsApp session.
- **Cloud API continuity.** Whether re-pointing the number preserves history/identity (above).
