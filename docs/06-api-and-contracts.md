# 06 · API & Contracts

The brain's own HTTP surface, plus the **exact Chatwoot calls** the adapter makes. This turns the abstract ports in [02 · Ports](02-assistant-brain.md#ports) into concrete wire contracts and grounds the write-back described in [01 · Brain ↔ Chatwoot](01-infrastructure.md#3-brain--chatwoot). Env vars referenced here are defined in [05-configuration.md](05-configuration.md).

> **Verify, don't assume.** Chatwoot's REST paths, payload fields, and webhook shape vary by version. Every Chatwoot contract below is the *intended* mapping — confirm against the API of the version you deploy. Auth header for the Application API is `api_access_token: <CHATWOOT_API_TOKEN>`.

---

## Brain HTTP surface

Two groups: the **public webhook receiver** (Chatwoot → brain) and the **admin API** (the Vue UI → brain). Generate the spec with `swag` like the main repo (`make swagger`); annotate handlers the same way.

### Webhook receiver

```
POST /v1/assistant/webhook/chatwoot
```

- **Auth:** verify `CHATWOOT_WEBHOOK_SECRET` before any work. *(Verify the mechanism: some Chatwoot versions sign payloads; if yours does not, require the secret as a header or an unguessable path segment — see [01 · webhooks](01-infrastructure.md#3-brain--chatwoot).)*
- **Body:** the Chatwoot `message_created` event ([below](#the-message_created-payload)).
- **Behavior:** classify → if it is an **incoming, non-private** message on the configured inbox, run `HandleMessage`; otherwise ignore. Return **`200` quickly** so Chatwoot does not retry on latency; at ~100 leads the LLM call can run inline within the request, but keep the handler idempotent on the Chatwoot message id (de-dupe redelivery).
- **Response:** `200 {"status":"ok"}` always (even on ignore), so Chatwoot marks delivery successful. Log failures; do not 500 on a recoverable LLM error (post an escalation note instead — [02 · post-processing](02-assistant-brain.md#post-processing-pipeline)).

### Admin API

Base path `/v1/admin/assistant`. Auth per [08-admin-ui.md](08-admin-ui.md) (`ADMIN_AUTH_MODE`). Backed by the schemas in [03-content-and-data.md](03-content-and-data.md).

| Method & path | Purpose |
|---|---|
| `GET /config` · `PUT /config` | Read / edit the draft `assistant_configs` row |
| `GET /config/versions` · `POST /config/publish` · `POST /config/rollback` | Version lifecycle (one published at a time) |
| `GET/POST/PUT/DELETE /topics` | `kb_topics` CRUD |
| `GET/POST/PUT/DELETE /assets` | `kb_assets` CRUD (metadata only; binaries live in `xpayment-content`) |
| `GET/PUT /prices` | `tariffs` + `placeholders` |
| `POST /playground` | Dry-run: `{message, language, config_version?}` → a full `Draft` preview, **nothing sent** |

`POST /playground` response mirrors the post-processed `Draft` ([02 · the contract](02-assistant-brain.md#the-contract)) plus debug fields:

```json
{
  "reply_text": "На тарифе «Рост» можно подключить до 5 касс, стоимость — 25 000 ₸ …",
  "reply_language": "ru",
  "media": [{"ref": "add_cashier_video", "kind": "screen_recording", "url": "https://…"}],
  "profile_patch": {"interested_tariff": "growth"},
  "suggested_status": "qualifying",
  "confidence": 0.82,
  "escalate": false,
  "debug": {"matched_topics": ["tariffs"], "dropped_refs": [], "price_tokens_rendered": ["price.growth","limit.growth"]}
}
```

---

## Chatwoot contracts

### Port → call mapping

| Port method ([02](02-assistant-brain.md#ports)) | Chatwoot call |
|---|---|
| `ChatwootReader.Window` | `GET …/conversations/{id}/messages` |
| `ChatwootReader.Profile` | `GET …/contacts/{contact_id}` (read `custom_attributes`) |
| `ChatwootWriter.PostPrivateNote` | `POST …/conversations/{id}/messages` (`private:true`) |
| `ChatwootWriter.MergeContactAttributes` | `GET` then `PUT …/contacts/{contact_id}` (`custom_attributes`) |
| `ChatwootWriter.SetLabels` | `POST …/conversations/{id}/labels` |
| `ChatwootWriter.SendOutgoing` (Phase 3) | `POST …/conversations/{id}/messages` (`private:false`) |

All paths are prefixed `{{CHATWOOT_BASE_URL}}/api/v1/accounts/{{CHATWOOT_ACCOUNT_ID}}`.

### Reads

**Window** — last ~15 messages of the conversation:
```http
GET /api/v1/accounts/{account_id}/conversations/{conversation_id}/messages
api_access_token: {CHATWOOT_API_TOKEN}
```
Returns the message list (newest pages first in some versions — verify ordering and paginate to ~15). Map each to the neutral `Message` ([02](02-assistant-brain.md#the-contract)); drop private notes and bot/own messages from the window.

**Profile** — the contact's custom attributes:
```http
GET /api/v1/accounts/{account_id}/contacts/{contact_id}
```
Read `custom_attributes` into the profile map. The `contact_id` comes from the webhook payload (`conversation.meta.sender.id` / `sender.id` — verify the exact path).

### Writes

**Private-note draft** (the v1 output):
```http
POST /api/v1/accounts/{account_id}/conversations/{conversation_id}/messages
api_access_token: {CHATWOOT_API_TOKEN}
Content-Type: application/json

{ "content": "🤖 Draft (RU): …", "message_type": "outgoing", "private": true }
```
`private:true` keeps it internal — Evolution never forwards it ([01 · why private notes are safe](01-infrastructure.md#why-private-notes-are-safe)).

**Merge custom attributes** (the profile) — ⚠️ **read-modify-write**: a `PUT` typically **replaces** the whole `custom_attributes` object, so to honor additive-merge (Decision 9) the brain must GET current attrs, merge, then PUT:
```http
PUT /api/v1/accounts/{account_id}/contacts/{contact_id}
{ "custom_attributes": { "...previous...": "...", "interested_tariff": "growth" } }
```

**Labels** (status) — ⚠️ the labels API **sets the full list**, not append; send the union of existing + new:
```http
POST /api/v1/accounts/{account_id}/conversations/{conversation_id}/labels
{ "labels": ["qualifying", "lang:ru"] }
```

**Outgoing message** (Phase 3 auto-send only) — same endpoint as the note with `private:false`; this is the *only* path that reaches the customer, and still goes through Chatwoot, never Evolution (Decision 6).

### The `message_created` payload

The account webhook posts (fields vary by version — verify):
```json
{
  "event": "message_created",
  "message_type": "incoming",           // incoming | outgoing | template
  "private": false,
  "content": "Сколько касс можно подключить?",
  "conversation": { "id": 123, "meta": { "sender": { "id": 456 } } },
  "inbox": { "id": 7 },
  "account": { "id": 1 }
}
```

**Classification rule** (the brain processes only true customer messages):
```
process  ⇔  event == "message_created"
         ∧ message_type == "incoming"
         ∧ private == false
         ∧ inbox.id == CHATWOOT_INBOX_ID
```
Everything else (outgoing agent replies, the brain's own private notes, other inboxes) is acknowledged with `200` and ignored — this is what prevents loops ([02 · what the brain ignores](02-assistant-brain.md#what-the-brain-ignores)).

---

## Open questions

- **Webhook signing.** Does the deployed Chatwoot sign account-webhook payloads? If not, the secret-header/secret-path fallback (above).
- **Contact id path.** The exact field carrying `contact_id` in the `message_created` payload (`conversation.meta.sender.id` vs `sender.id`).
- **Pagination & ordering** of the messages endpoint, to fetch exactly the last ~15.
- **Attribute/label replace semantics.** Confirm whether `PUT contacts` and `POST labels` replace vs. merge in your version (the read-modify-write guards above assume replace).
