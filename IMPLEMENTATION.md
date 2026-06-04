# Implementation status

This repo implements the **brain** described in [`docs/`](docs/README.md): the standalone
Go service (Chatwoot webhook + admin UI + embedded SQLite + OpenRouter), **suggest-only**,
with the **~15-message window** memory model (Decision 9 as written in the docs).

Chatwoot and Evolution are **third-party services** — they are *configured* (see
`docker-compose.yml` and [`docs/01-infrastructure.md`](docs/01-infrastructure.md)), not coded here.

## What's built (Phase 2 — the brain + admin)

| Area | Where | Notes |
|---|---|---|
| Domain core | `internal/domain` | `ChatID, Message, Draft, RawDraft, Snapshot, PriceBook.Render`, snapshot validation |
| `HandleMessage` + pipeline | `internal/usecase/assistant` | prompt assembly `[A]–[E]`, post-processing (escalate→refs→prices→profile→status), ports, mocked-port tests |
| Admin service | `internal/usecase/admin` | config/KB/price CRUD, draft→publish→rollback, Playground dry-run |
| SQLite store | `internal/infrastructure/sqlite` | embedded migrations + seed, snapshot load, lifecycle, dedup |
| OpenRouter `Drafter` | `internal/infrastructure/llm` | OpenAI-compatible `chat/completions`, forced `emit_draft`, defensive parse |
| Chatwoot adapter | `internal/infrastructure/chatwoot` | window/profile reads; private-note, attribute-merge (read-modify-write), label writes |
| HTTP surface | `internal/ports/http` | webhook (classify, dedup, per-contact lock), `/admin`, `/media`, health/ready/metrics |
| Admin UI | `internal/ports/http/admin` | server-rendered templates + session auth (dashboard, persona, KB, media, prices, Playground, audit) |
| Config | `internal/infrastructure/config` | full env catalog from `docs/05` |
| Packaging | `Dockerfile`, `docker-compose.yml`, `Makefile`, `.env.example` | one static binary; full local stack |

Run locally: `cp .env.example .env`, fill `LLM_API_KEY` / Chatwoot / `ADMIN_PASSWORD` /
`SESSION_SECRET`, then `make run` and open `http://localhost:8080/admin`.
Tests/build: `make test && make build`.

## Next steps

**Phase 1 (ops, no code):** self-host Chatwoot + Evolution; wire Evolution's native Chatwoot
integration; pre-define the contact custom attributes (`docs/03` profile keys); set the brain's
URL as Chatwoot's **account webhook**; mine the ~100 existing chats to seed the KB and the golden set.

**Phase 2 polish (code, optional):** richer Playground (window editor), media object-storage backend,
OTel exporter wiring, a golden-set eval harness (`docs/07`).

**Phase 3 (scale):** confidence-gated auto-send via `SendOutgoing` (already on the port) with send
pacing/quiet-hours; Evolution → WhatsApp Cloud API behind the adapter.

## Known open items (from `review.md` / docs open questions)

- Memory model is the **~15-msg window** as chosen; revisit before Phase-3 auto-send.
- `suggested_callback` is now surfaced in the private note (was flagged as unwired in `review.md`).
- Webhook auth uses a secret header/query param; confirm your Chatwoot version's signing scheme.
- Compliance gate (`LLM_API_KEY` sends conversation text abroad) — confirm before production.
