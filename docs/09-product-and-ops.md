# 09 · Product & Operations

The "how we *see* the product" half of alignment: what it's for, how a rep uses it, what it costs, what's legally required, and the single consolidated **open-questions register**. The "how we *build* it" half is in [02](02-assistant-brain.md)–[08](08-admin-ui.md).

---

## Vision

Turn the WhatsApp inbox from a manual, founder-bottlenecked sales channel into a **fast, consistent, qualified** one — without losing the human touch. The copilot drafts correct, on-brand answers in the customer's language so a rep can reply in seconds instead of minutes, while quietly building a qualification profile that tells the team **who to call first**. Suggest-only means quality and trust come first; automation is earned later.

## Personas

- **The merchant (customer):** a Kazakhstani business owner or their developer/employee, on WhatsApp, in RU or KK, asking about what xpayment does, tariffs, cashier limits, and how to integrate. Often price-sensitive and security-cautious (the Kaspi cashier-role question).
- **The rep (primary user):** the founder/sales person working the Chatwoot inbox. Wants to answer faster, never misquote a price, and not lose track of follow-ups.
- **The admin (config owner):** edits the persona, KB, media, and prices in the **built-in admin UI** ([08](08-admin-ui.md)); tests in the Playground + eval gate, then **publishes**.

## Job to be done

> *When a lead messages us on WhatsApp in Russian or Kazakh, help me reply quickly with an accurate, well-illustrated answer and capture what kind of merchant they are — so I convert more leads without hiring a team.*

## Scope & non-goals (v1)

**In:** suggest-only drafting in RU/KK from a curated KB; media attachment; price-safe answers; lead-profile extraction; Chatwoot-native status/callbacks/grouping; an admin to edit the brain; a Playground + golden-set gate.

**Out (v1):** autonomous sending (Phase 3, behind a confidence gate); channels other than WhatsApp (arrive later via Chatwoot, no core change); a custom CRM UI (Chatwoot is the inbox, Decision 1); outbound campaigns/blasts; a vector search stack (Decision 7); deep integration into the payments backend (the brain is standalone, Decision 12).

---

## KPIs

Track from day one; most are computable from Chatwoot data.

| Metric | Why |
|---|---|
| **Median first-response time** | The core promise — should drop sharply with drafts ready on arrival. |
| **Draft-acceptance rate** | % of AI drafts sent with no/minor edits — the trust signal that gates Phase-3 auto-send. |
| **Edit distance** (sent vs draft) | Where the bot is weak; feeds KB/persona fixes. |
| **Qualification coverage** | % of conversations with a populated profile — the bot's "quiet" value. |
| **Escalation rate** | How often the bot correctly defers to a human; too high = KB gaps, too low = over-confidence. |
| **Conversion** (lead→trial→paid) | The business outcome; attribute against the profile. |

> `fit_score` ([03](03-content-and-data.md#the-lead-profile-lives-in-chatwoot-not-here)) is a **prioritization sort key, not truth**, until calibrated against real conversions — do not report it as a probability in month one.

---

## Operating procedure

> The **conversation flow** the bot drafts toward — greeting → qualify → recommend → handle objections → drive to top-up/tariff → maintain — and the stance/scripts behind it are the **[sales playbook](11-sales-playbook.md)** (the brain's "soul").

The rep's daily loop in Chatwoot:

1. Open the WhatsApp inbox; conversations with new customer messages carry an AI **private-note draft** and an updated profile in the contact sidebar.
2. Read the draft + the suggested media; **approve, edit, or ignore**.
3. **Send** the reply through Chatwoot's composer → Evolution → WhatsApp.
4. Set a **snooze** if a callback is needed (resurfaces the conversation), and let labels reflect status; **merge** contacts when one person writes from two numbers ([01](01-infrastructure.md), Decision 1).

> **Known friction (v1):** a private-note draft must be **copied into the composer** to send — Chatwoot does not let a bot pre-fill an agent's reply box. Format the note for easy one-tap copy; Phase 3 can smooth this with a Chatwoot dashboard-app "use this reply" button or confidence-gated auto-send ([02 · post-processing](02-assistant-brain.md#post-processing-pipeline)).

**Escalation routing:** when the bot sets `escalate`, it posts a "needs human" note instead of a draft — the rep handles it directly. Pricing negotiation, legal/compliance specifics, and complaints should always route to the founder/owner, not be auto-answered.

---

## Compliance (resolve before go-live — critical)

This is the highest-priority open item, not a footnote.

- **Sending chat to OpenRouter.** Setting `LLM_API_KEY` ([05](05-configuration.md)) means customer conversation text (personal data) leaves your infrastructure for a third-party processor abroad. Under Kazakhstan's **Law "On Personal Data and its Protection"** this is **cross-border processing** and needs a lawful basis (consent and/or a documented assessment), and possibly data-minimization (avoid sending more than needed). **Decide and document this before production.** Options to weigh: explicit consent in the first auto-reply, minimizing/redacting PII before the LLM call, or a deployment/terms that satisfy the requirement.
- **Consent & opt-out.** You only message people who messaged you first (warm inbound) — keep it that way, provide a clear opt-out, and honor "stop". This also reduces Evolution ban risk ([01](01-infrastructure.md#ban-risk-and-outbound-pacing)).
- **Retention & access.** Conversation data lives in self-hosted Chatwoot (you control it). Define a retention period and a deletion-on-request path; back up per [04 · Backups](04-service-and-deployment.md#backups--tls).

---

## Cost model

Small at this scale, but make it explicit so it isn't a surprise.

- **LLM (OpenRouter).** Per drafted message ≈ (cached-prefix read + dynamic suffix in) + (short output) tokens. The prefix (persona + KB + media catalog) is large but cached; the suffix (window + profile + message) and the ≤~120-word output are small. At ~100 leads with a few messages each, expect a **low monthly bill**; cap output with `LLM_MAX_TOKENS`. Note the prompt-cache caveat ([01](01-infrastructure.md#prompt-cache-caveat)) — savings are modest at low frequency. Eval runs (LLM-as-judge) add cost; run them nightly/manually, not per PR ([07](07-testing-and-evals.md#ci)).
- **Infra.** Three self-hosted services (Chatwoot, Evolution, the brain) fit on one modest VPS — Chatwoot & Evolution each bring a Postgres/Redis; the **brain is stateless** (no DB). The biggest operational cost is **attention** (session health, backups), not compute.

---

## Risks

| Risk | Mitigation |
|---|---|
| WhatsApp number banned (Evolution) | Suggest-only + warm inbound + outbound pacing; Cloud-API migration path ([01](01-infrastructure.md)). |
| Bot misquotes a price | Tokens + Go injection + price-safety eval = model never authors a number (Decision 8, [07](07-testing-and-evals.md)). |
| Wrong media attached | Runtime `asset_ref` validation + media-precision eval ([02](02-assistant-brain.md), [07](07-testing-and-evals.md)). |
| Chatwoot/brain downtime | Humans reply manually in Chatwoot; webhook handler idempotent on retry ([01](01-infrastructure.md), [06](06-api-and-contracts.md)). |
| Data/compliance breach | Resolve the OpenRouter/KZ-law question before go-live (above). |
| WhatsApp session silently drops | Session-health alerting ([01](01-infrastructure.md#session-health-alerting)). |
| LLM cost creep | `LLM_MAX_TOKENS`, cached prefix, evals off the PR path. |

---

## Open-questions register

The single consolidated list (Definition-of-Ready #6 in [README](README.md#definition-of-ready--is-this-set-enough-to-build-from)). Every per-file *Open Questions* entry appears here. Assign an **owner** and resolve before the dependent phase.

| # | Question | Surfaced in | Owner | Status |
|---|---|---|---|---|
| 1 | **LLM/OpenRouter + KZ personal-data law** — lawful basis / consent / minimization for sending chat abroad | 05, 09 | — | **open (blocks go-live)** |
| 2 | **Admin governance** — who may edit/publish config; admin login strength + exposure (TLS / IP allowlist) | 08 | — | open |
| 3 | **Webhook signing** — does Chatwoot sign account webhooks; else secret-header/path | 01, 06 | — | open |
| 4 | **`message_created` payload** — exact `contact_id` path + classification completeness | 01, 06 | — | open |
| 5 | **Messages endpoint** — ordering/pagination to fetch the last ~15 (window size) | 02, 06 | — | open |
| 6 | **Attribute/label replace semantics** — confirm read-modify-write is needed | 06 | — | open |
| 7 | **Cloud-API migration** — re-point number without losing history/identity; idempotency at the seam | 01 | — | open |
| 8 | **Session-health surface** — where a dropped WhatsApp session alerts | 01 | — | open |
| 9 | **Auto-send confidence threshold** — the numeric gate for Phase 3 | 02, 07 | — | open |
| 10 | **Eval thresholds + judge model + golden-set size/refresh** | 07 | — | open |
| 11 | **KK/RU authoring coverage** + mixed-language tie-break rule | 02, 03 | — | open |
| 12 | **Media storage** — served `MEDIA_DIR` vs object storage for video; `MEDIA_BASE_URL` target | 03, 05 | — | open |
| 13 | **`fit_score` calibration** against real conversions | 03, 09 | — | open |
| 14 | **Deploy target / shared infra / image tags** (Chatwoot, Evolution, brain) | 04 | — | open |
| 15 | **Model choice** (`LLM_MODEL`) after eval results; **DB/media durability** (`DB_PATH`/`MEDIA_DIR` volume) | 04, 05 | — | open |
| 16 | **Publish gate** — run snapshot-validation + the golden set before an admin publish | 07, 08 | — | open |
