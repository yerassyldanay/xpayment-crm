# 08 · Admin UI

The internal surface where the team edits the bot's brain — persona, knowledge base, media, prices — and tests it in a Playground. It lives in the existing **`xpayment-frontend`** (Vue 3 + TS + Pinia), and it talks to the brain's admin API ([06-api-and-contracts.md](06-api-and-contracts.md)). The lifecycle (draft → publish → rollback) and schemas are in [03 · admin lifecycle](03-content-and-data.md#admin--config-lifecycle).

---

## Where it lives

A protected section under `/admin/assistant/*` in `xpayment-frontend`, following that app's established pattern:

```
View (.vue) → composable (useXxx) → api module (xxxApi) → axios client → backend
```

Reuse `src/api/client.ts` (axios + bearer-token injection) and the Pinia auth store (`src/stores/auth.ts`). New files:

```
src/api/{assistantConfig,kb,prices,playground}.ts   # thin api modules
src/stores/assistant.ts                              # admin state (current draft, versions)
src/composables/useAssistantConfig.ts  …             # loading/error wrappers
src/views/admin/{PersonaEditor,KnowledgeTopics,MediaCatalog,Prices,Playground}.vue
router: routes under /admin/assistant (guarded by the existing auth guard + an admin check)
```

---

## Screens

Each maps to the admin endpoints in [06](06-api-and-contracts.md#admin-api) and the schemas in [03](03-content-and-data.md).

| Screen | Edits | Endpoints |
|---|---|---|
| **Persona** | `assistant_configs` (persona/“soul”, guardrails, language policy, model, temperature, enabled tools) with **draft / publish / rollback** + version history | `GET/PUT /config`, `…/versions`, `…/publish`, `…/rollback` |
| **Topics** | `kb_topics` — bilingual answer text with price **tokens only** | `…/topics` CRUD |
| **Media** | `kb_assets` — `ref`, kind, url, **LLM-facing description**, tags; binaries pushed to `xpayment-content` separately | `…/assets` CRUD |
| **Prices** | `tariffs` + `placeholders` — the single source of numbers (review-gated) | `GET/PUT /prices` |
| **Playground** | nothing — dry-run only | `POST /playground` |
| **Evals** (Phase 3) | run the golden set against a version; show the scorecard | (see [07](07-testing-and-evals.md)) |

**Playground is the day-to-day testing surface** ([07 · how we keep it simple](07-testing-and-evals.md)): type a KK or RU message, pick a config version (draft or published), and see the drafted reply, matched topic, chosen media (with previews), extracted profile, and confidence — **nothing is sent, no real conversation is touched**.

---

## Cross-service auth (the key standalone-service consequence)

Decision 12 splits the surfaces: the **frontend** authenticates users against the **main `xpayment` backend** (user bearer `xusr_live_…`), but the **brain** — a separate service — serves the admin API the UI now calls. The brain must therefore validate those tokens across the boundary. Two modes (selected by `ADMIN_AUTH_MODE` in [05](05-configuration.md#admin-auth-cross-service--08)):

- **`introspect` (recommended).** The brain forwards the incoming `Authorization: Bearer xusr_live_…` to the main backend's user-token validation endpoint (reuse its existing "current user" / token-validation route — *verify the exact path*), checks the user is an admin, and caches the result briefly (by token hash, ~60s). **Pros:** admins log in once through the normal app; real per-user identity; no new credential. **Cons:** a runtime dependency on the main backend for admin calls (acceptable — admin is internal and low-frequency).
- **`static`.** A single shared `ADMIN_API_TOKEN` the frontend sends to the brain. **Pros:** trivial to ship, no coupling. **Cons:** a separate secret to distribute, no per-user identity, coarse access.

**Recommendation:** ship **`introspect`** so the existing login "just works" and you get per-user attribution on config changes; fall back to `static` only if exposing/validating the main backend's tokens proves awkward in your setup. This is tracked as an owned item in the [09 open-questions register](09-product-and-ops.md).

### Frontend wiring detail

The frontend's existing axios client points at the **main backend**. The admin API is on the **brain** (a different base URL), so add a **second axios client** (`VITE_BRAIN_BASE_URL`) that reuses the same bearer-injection interceptor. The admin api modules use this brain client; everything else keeps using the main client.

---

## Testing

`xpayment-frontend` currently has **no test runner** ([07](07-testing-and-evals.md)). For the admin:
- **Recommended:** add **vitest** for the api modules and composables (mock the brain client), and component tests for the editors' validation (e.g. blocking a price numeral typed into a topic body).
- **Acceptable for v1:** rely on the **Playground** + manual checks, and defer automated frontend tests — but note this explicitly so it's a choice, not an oversight.

---

## Open questions

- **Auth mode** — confirm `introspect` vs `static`, and the exact main-backend validation endpoint + the admin check (`is_admin`).
- **Who edits what** — is price editing restricted to a sub-role (review-gated per [03](03-content-and-data.md#pricing--templates))?
- **Frontend tests** — add vitest now or accept manual-for-v1.
