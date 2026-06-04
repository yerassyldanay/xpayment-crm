# 08 · Admin UI (server-rendered, Go templates + htmx)

The **built-in** admin where the team edits the bot's settings and knowledge base. It is served by the **same Go binary** on the **same port** as the webhook (Decision 12) — **no `xpayment-frontend`, no JS build, no separate deploy**. The data schema and the draft/publish lifecycle are in [03-content-and-data.md](03-content-and-data.md); the HTTP routes are in [06-api-and-contracts.md](06-api-and-contracts.md).

---

## How it's built

- **Server-rendered Go `html/template` + [htmx](https://htmx.org/).** Pages are HTML; forms POST to `/admin/...` handlers; handlers render **partial templates** that htmx swaps into the page. No SPA, no build step, no Node.
- **Embedded in the binary.** Templates and static assets (htmx, a little CSS) ship via `embed.FS`, so the whole UI travels with the one executable.
- **Served at `/admin/*`** behind a session login; everything is one origin → **no cross-service auth** (the problem the standalone design removes).

```
GET  /admin                      → dashboard
GET  /admin/persona              → edit persona + guardrails (draft)        POST → save draft (htmx partial)
GET  /admin/topics               → list kb_topics                          POST/PUT/DELETE rows
GET  /admin/media                → list kb_assets + upload                  POST upload → MEDIA_DIR + row
GET  /admin/prices               → edit tariffs + placeholders              POST → save (review-gated)
GET  /admin/playground           → dry-run a message                       POST → render the draft + debug
POST /admin/publish              → validate → promote draft → hot-reload
POST /admin/rollback?v=N         → re-publish version N
GET  /admin/versions             → version + audit log
```

---

## Auth (same-service login)

Because the UI and the API are the same service, auth is a **simple admin login** — no tokens across services:

- `ADMIN_USER` + `ADMIN_PASSWORD` (stored hashed) → a **signed session cookie** ([05](05-configuration.md)).
- Session middleware guards every `/admin/*` route; the webhook route stays separate (its own secret).
- **The admin is on the public port, so:** serve only over **TLS** ([04](04-service-and-deployment.md#backups--tls)), put a **CSRF token** on every form, rate-limit the login, use a strong `ADMIN_PASSWORD`, and ideally an **IP allowlist** in the reverse proxy. Treat `/admin` as internet-exposed.

---

## Screens

| Screen | Edits | Notes |
|---|---|---|
| **Dashboard** | — | current published version, last publish, quick links |
| **Persona & guardrails** | `assistant_config` (draft): persona, mission, guardrails, language policy, reply-max-words, enabled tools | the bot's "soul" ([11](11-sales-playbook.md)); the LLM **model is `LLM_MODEL` env**, not edited here (Decision 13) |
| **Topics** | `kb_topics` (bilingual RU/KK), markdown body with a **price-token helper** | tokens, never digits (Decision 8) |
| **Media** | `kb_assets` + **upload** to `MEDIA_DIR` | the LLM-facing `description` is the selection menu entry |
| **Prices** | `tariffs` + `placeholders` | the single price source; review-gated |
| **Playground** | nothing — **dry-run** | see below |
| **Versions** | publish / rollback / audit log | draft → publish → rollback ([03](03-content-and-data.md#draft--publish--rollback-the-config-lifecycle)) |

---

## The config lifecycle (draft → publish → rollback)

You always edit a **draft**; the live bot keeps serving the **published** snapshot. **Publish** validates the draft ([03 · validate on load](03-content-and-data.md#validate-on-load-fail-loudly)) → promotes it → **hot-reloads** the in-memory snapshot (no restart). **Rollback** re-publishes an earlier version. Every change is in the `audit_log`. A bad edit never reaches a customer because (a) it's a draft until you publish, and (b) publish refuses an invalid snapshot.

---

## Playground (test before you publish)

The most-used screen. Type a customer message, pick a language, and the brain runs the **real `HandleMessage`** against the **draft** config + live KB — with an **LLM toggle** (real OpenRouter call, or a stubbed response) and a **mocked Chatwoot** — then renders the resulting `Draft`: reply text (prices injected), chosen media (previews), extracted `profile_patch`, matched topic, confidence, escalate. **Nothing is sent and no real conversation is touched.** This is where you tune the persona/KB/prices before publishing, and it's the same path the [golden-set evals](07-testing-and-evals.md#golden-set-eval-harness) use.

---

## Testing

`httptest`-based handler tests for the admin routes (render, save-draft, publish-validates-and-rejects-bad-config, rollback) + the Playground dry-run. No browser/JS test stack needed since there's no SPA ([07](07-testing-and-evals.md)).

---

## Open questions

- **Auth model** — one shared `ADMIN_USER` login (simplest) vs. per-user admin accounts with the `audit_log` attributing changes.
- **Exposure** — admin on the public port behind TLS + IP allowlist, vs. only reachable over a VPN/private network.
- **Editor richness** — plain `<textarea>` for topic markdown vs. a light markdown editor with live token preview.
