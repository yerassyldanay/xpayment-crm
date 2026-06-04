# Glossary

Terms used across the documentation, grouped by cluster. Each entry is one or two sentences and links to the file that goes deep. Decisions are defined in [README.md](README.md#decisions).

---

## System shape

- **Hub-and-satellites** — The topology where Chatwoot is the central hub and Evolution + the brain are satellites that talk only to the hub, never to each other (Decision 3). See [README · Architecture](README.md#hub-and-satellites).
- **Single source of truth** — One owner per piece of state, so there is nothing to keep in sync. Chatwoot owns all conversational state (Decision 1); the brain owns config/KB/media/prices. See [README · Ownership](README.md#ownership).
- **Stateless decision service** — The brain stores no conversations; it reads context fresh from Chatwoot on every call and returns a decision (Decision 2). See [02 · Context-on-read](02-assistant-brain.md#context-on-read).
- **Channel-agnostic** — The brain takes a `chatID` and never learns the transport, so the same code serves WhatsApp now and any channel later — *because everything funnels through Chatwoot* (Decision 4; the named assumption in [README](README.md#named-assumption)).

## Transport

- **Evolution API** — The self-hosted gateway that bridges WhatsApp to Chatwoot via its native Chatwoot integration (Decision 5). See [01 · Evolution ↔ Chatwoot](01-infrastructure.md#1-evolution--chatwoot).
- **Baileys** — The unofficial WhatsApp Web library Evolution is built on; it carries ban risk and a session that can drop. See [01 · Operations](01-infrastructure.md#ban-risk-and-outbound-pacing).
- **Cloud API** — Meta's official WhatsApp Business API; the documented migration target that removes ban risk but adds the 24-hour window and message templates. See [01 · Migration](01-infrastructure.md#evolution--cloud-api-migration-phase-3).
- **API channel** — The Chatwoot inbox type Evolution auto-creates to receive WhatsApp messages over REST. See [01 · Evolution ↔ Chatwoot](01-infrastructure.md#1-evolution--chatwoot).
- **Webhook — account-level vs. inbox** — Two different hooks: the **account-level** webhook fires `message_created` to the **brain**; the **inbox** webhook carries outgoing replies to **Evolution**. They are configured separately and must not be conflated. See [01 · The two webhook kinds](01-infrastructure.md#2-the-two-webhook-kinds--do-not-conflate).
- **Private note** — A message internal to Chatwoot (`private: true`) that Evolution never forwards to WhatsApp; this is how the brain delivers a draft safely in suggest-only mode (Decision 6). See [01 · Why private notes are safe](01-infrastructure.md#why-private-notes-are-safe).
- **AgentBot** — Chatwoot's bot-integration primitive; a documented alternative to the account webhook, but its pending/handoff lifecycle makes it less suited to a persistent copilot. See [01 · AgentBot](01-infrastructure.md#4-agentbot--the-alternative-and-why-we-dont-lead-with-it).

## Memory & state

- **Contact vs. conversation** — In Chatwoot, a **contact** is the person/business (persistent); a **conversation** is one thread with them. The profile attaches to the contact, the window to the conversation. See [02 · Memory](02-assistant-brain.md#memory).
- **Window** — The last ~15 messages (or 48h) of the current conversation, read from Chatwoot per call (Decision 9). See [02 · Memory](02-assistant-brain.md#memory).
- **Profile** — The persistent, structured lead facts, stored as Chatwoot contact custom attributes (Decision 9). See [03 · The lead profile](03-content-and-data.md#the-lead-profile-lives-on-the-chatwoot-contact-not-here).
- **Custom attributes** — Chatwoot's key/value fields on a contact (or conversation); must be **pre-defined** before the brain can write them. See [01 · Brain ↔ Chatwoot](01-infrastructure.md#3-brain--chatwoot).
- **Context-on-read** — Reading the window and profile live on each message instead of storing them, which is why first-message and mid-conversation share one code path. See [02 · Context-on-read](02-assistant-brain.md#context-on-read).
- **`profile_patch`** — The subset of newly-confident profile fields the model returns on a given message. See [02 · JSON output contract](02-assistant-brain.md#json-output-contract).
- **Additive merge** — Merging a `profile_patch` so it adds or updates fields but **never nulls** a previously-known one (Decision 9). See [02 · Post-processing](02-assistant-brain.md#post-processing-pipeline).

## The brain's reasoning

- **`HandleMessage`** — The brain's single entry point: `HandleMessage(ctx, chatID, inbound) → (Draft, error)`; it decides what to respond and never sends. See [02 · The contract](02-assistant-brain.md#the-contract).
- **System prompt — cached prefix vs. dynamic suffix** — The prompt is a stable prefix `[A]–[E]` (frame, identity, guardrails, KB, media menu) plus a per-message suffix (profile, window, current message), with the cache breakpoint after `[E]`. See [02 · Prompt assembly](02-assistant-brain.md#prompt-assembly).
- **Prompt caching** — Reusing the static prefix across calls to cut latency/cost; savings are modest at low volume due to a short TTL. See [01 · Prompt-cache caveat](01-infrastructure.md#prompt-cache-caveat).
- **Structured output** — Forcing the model to return the fixed JSON contract rather than free text. See [02 · JSON output contract](02-assistant-brain.md#json-output-contract).
- **Persona / "soul" vs. code-owned skeleton** — The editable identity/guardrails (soul, in `assistant_configs`) vs. the non-editable FRAME — the JSON contract, assembly order, and hard rules (skeleton, in code). See [03 · Assistant config](03-content-and-data.md#assistant-config).
- **Guardrails** — The editable must/must-not rules shaping the bot's behavior (block `[C]`). See [03 · Assistant config](03-content-and-data.md#assistant-config).
- **Grounding** — Answering only from the knowledge base, escalating when the answer isn't there, so the bot doesn't invent facts. See [02 · The FRAME](02-assistant-brain.md#prompt-assembly).
- **Escalation** — When the model is unsure or off-KB, it flags for a human instead of guessing; the brain posts a note and stops. See [02 · Post-processing](02-assistant-brain.md#post-processing-pipeline).
- **Human-in-the-loop** — The suggest-only model where every reply is a draft a human approves and sends (Decision 6). See [README · Decisions](README.md#decisions).
- **Confidence** — The model's self-rated certainty; the gate for a future auto-send phase. See [02 · Post-processing](02-assistant-brain.md#post-processing-pipeline).

## Knowledge & media

- **Topic-organized KB** — Knowledge grouped into answerable subjects (`kb_topics`), authored in both languages, all loaded into the prompt. See [03 · Knowledge base](03-content-and-data.md#knowledge-base).
- **LLM-as-selector** — Instead of search, the whole catalog is shown to the model and it returns the items it wants by name (Decision 7). See [03 · Media](03-content-and-data.md#media).
- **Asset catalog** — The full list of media (`kb_assets`) rendered into the prompt as a menu (`ref | kind | topic | description`). See [02 · Prompt assembly](02-assistant-brain.md#prompt-assembly).
- **`ref`** — The stable slug naming one media asset (e.g. `add_cashier_video`); the menu key the model returns. See [03 · Media](03-content-and-data.md#media).
- **`asset_refs`** — The list of refs the model picks for a given reply (≤3). See [02 · JSON output contract](02-assistant-brain.md#json-output-contract).
- **Asset-ref validation** — Post-processing that drops unknown/hallucinated or over-limit refs before resolving them to URLs. See [02 · Post-processing](02-assistant-brain.md#post-processing-pipeline).
- **Binaries vs. metadata** — Media **files** live in the `xpayment-content` repo (served by URL); only **metadata** lives in `kb_assets`. See [03 · Media](03-content-and-data.md#media).
- **Vector index / embeddings / RAG** — Semantic-search machinery for retrieving relevant chunks at scale — **deliberately avoided** (Decision 7) because the corpus fits in one cached prompt; kept behind the `KnowledgeRetriever` port for later. See [03 · Knowledge base](03-content-and-data.md#knowledge-base).

## Correctness & lifecycle

- **Price tokens** — Placeholders like `{{price.growth}}` / `{{limit.growth}}` the KB uses instead of numerals; `{{namespace.key}}` maps a namespace to a column and a key to a row (Decision 8). See [03 · Pricing & templates](03-content-and-data.md#pricing--templates).
- **PriceBook** — The single source of price numbers (`tariffs` + `placeholders`) and the `Render` function that fills tokens. See [03 · Pricing & templates](03-content-and-data.md#pricing--templates).
- **Code injection** — Substituting real prices **after** the model runs, so the model never emits a numeral and cannot misquote (Decision 8). See [02 · Post-processing](02-assistant-brain.md#post-processing-pipeline).
- **Lead qualification** — Building the structured profile that scores a lead's fit (business type, volume, urgency, tariff). See [03 · The lead profile](03-content-and-data.md#the-lead-profile-lives-on-the-chatwoot-contact-not-here).
- **Fit score** — A derived 0–100 prioritization number; a sort key until calibrated against real conversions. See [03 · The lead profile](03-content-and-data.md#the-lead-profile-lives-on-the-chatwoot-contact-not-here).
- **Draft → publish → rollback** — The config lifecycle: edit a draft, publish exactly one version live, roll back to an earlier one. See [03 · Admin & config lifecycle](03-content-and-data.md#admin--config-lifecycle).
- **Playground** — The admin dry-run: try a message and see the draft, media, and profile without sending or touching a real conversation. See [03 · Admin & config lifecycle](03-content-and-data.md#admin--config-lifecycle).
- **Golden set / eval** — Real questions mined from chats, run against a config before publish, scoring answer quality **and** media-selection precision. See [03 · Admin & config lifecycle](03-content-and-data.md#admin--config-lifecycle).
- **Hexagonal / ports & adapters** — The architecture style where the brain depends on interfaces (ports), so it is built and tested before any real integration exists (Decision 10). See [02 · Ports](02-assistant-brain.md#ports).
- **Mining** — Extracting the real topics, KK/RU phrasings, and qualification signals from existing conversations to seed the KB and the golden set; the load-bearing Phase-1 task. See [README · Roadmap](README.md#roadmap).

## Service & deployment

- **Standalone service** — The brain runs as its own Go service/repo (`xpayment-crm`) with its own Postgres, image, and config, reusing the main repo's conventions (Decision 12). See [04 · Repo layout](04-service-and-deployment.md#repo-layout).
- **Multi-stage build** — A Dockerfile that compiles on a Go image and ships the binary on a minimal Alpine runtime. See [04 · Dockerfile](04-service-and-deployment.md#dockerfile).
- **Migrations on startup** — goose applies embedded SQL automatically when the service boots; no separate migrate step. See [04 · Migrations](04-service-and-deployment.md#migrations).
- **Full-stack compose** — The local docker-compose that stands up Chatwoot + Evolution + the brain (each with its datastore) together — unique to this service. See [04 · Local stack](04-service-and-deployment.md#local-stack-docker-compose).
- **Webhook tunnel** — A cloudflared/ngrok tunnel giving Chatwoot a public HTTPS URL to reach the brain in local dev. See [04 · Local stack](04-service-and-deployment.md#local-stack-docker-compose).
- **Blue-green deploy** — The main repo's zero-downtime pattern (two slots + HAProxy); the brain starts with a simpler single-container deploy. See [04 · Deployment](04-service-and-deployment.md#deployment).
- **Health / readiness / RED metrics** — `/health`, `/ready`, and Prometheus request metrics (Rate, Errors, Duration); reused from the main repo. See [04 · Observability](04-service-and-deployment.md#observability).
- **Backups** — Scheduled dumps of Chatwoot's Postgres (the system of record) and the brain's. See [01 · Backups](01-infrastructure.md#backups).

## Testing & evals

- **Port mock** — A generated test double for a brain interface (mockery + testify, `.EXPECT()`), letting `HandleMessage` run with no external services. See [07 · Unit tests](07-testing-and-evals.md#unit-tests).
- **Testcontainers** — Spinning a real Postgres in a container for integration tests (repos, migrations). See [07 · Integration tests](07-testing-and-evals.md#integration-tests).
- **Golden-set harness** — The eval runner: real mined questions scored on deterministic fields + prose, gating publish. See [07 · Golden-set eval harness](07-testing-and-evals.md#golden-set-eval-harness).
- **LLM-as-judge** — Using a model to score draft prose against a rubric, since exact-match doesn't fit free text. See [07 · Golden-set eval harness](07-testing-and-evals.md#golden-set-eval-harness).
- **Media-selection precision** — An eval metric scoring whether the right asset (and not a wrong-topic one) was attached. See [07 · Golden-set eval harness](07-testing-and-evals.md#golden-set-eval-harness).
- **Price-safety test** — A must-pass test asserting the model emits only tokens and Go injects every price. See [07 · Must-pass behavioral tests](07-testing-and-evals.md#must-pass-behavioral-tests-the-correctness-guarantees).
- **Definition of Done** — Per-phase acceptance criteria for the rollout. See [07 · Definition of Done](07-testing-and-evals.md#definition-of-done-per-phase).

## Auth & security

- **Cross-service auth** — The standalone consequence: the brain must validate the frontend's main-backend user tokens to serve its admin API. See [08 · Cross-service auth](08-admin-ui.md#cross-service-auth-the-key-standalone-service-consequence).
- **Token introspection** — Auth mode where the brain forwards a user token to the main backend to validate it. See [08 · Cross-service auth](08-admin-ui.md#cross-service-auth-the-key-standalone-service-consequence).
- **Static admin token** — Simpler auth mode: a single shared secret the UI sends to the brain. See [08 · Cross-service auth](08-admin-ui.md#cross-service-auth-the-key-standalone-service-consequence).
- **`api_access_token`** — The header Chatwoot's REST API expects for write-back. See [06 · Chatwoot contracts](06-api-and-contracts.md#chatwoot-contracts).
- **Webhook secret** — The shared secret the brain verifies on inbound account-webhook calls. See [06 · Webhook receiver](06-api-and-contracts.md#webhook-receiver).

## Compliance & cost

- **Cross-border processing** — Sending customer chat to Anthropic (abroad) — personal data leaving the country, governed by KZ law; the top go-live gate. See [09 · Compliance](09-product-and-ops.md#compliance-resolve-before-go-live--critical).
- **Personal-data law** — Kazakhstan's law on personal data and its protection, requiring a lawful basis, consent, and retention rules. See [09 · Compliance](09-product-and-ops.md#compliance-resolve-before-go-live--critical).
- **Consent / opt-out** — Messaging only inbound contacts and honoring "stop"; also reduces ban risk. See [09 · Compliance](09-product-and-ops.md#compliance-resolve-before-go-live--critical).
- **Prompt-cache caveat** — Caching the prefix saves less than the headline at low message frequency (short TTL). See [01 · Prompt-cache caveat](01-infrastructure.md#prompt-cache-caveat).
