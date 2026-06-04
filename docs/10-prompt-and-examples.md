# 10 · The Assistant Brain — Prompt & Examples

The concrete prompt-engineering companion to [02-assistant-brain.md](02-assistant-brain.md). `02` describes the *pipeline* (`HandleMessage`, ports, post-processing); **this file shows the actual prompt** — every block filled with real xpayment content, the OpenRouter call with caching, how the window/summary and knowledge base enter the prompt, the JSON the model returns, and a gallery of worked KK/RU examples.

> All prices/numbers below are **illustrative** — they come from a sample `tariffs` ([03 · schema](03-content-and-data.md#schema-ddl)); the real values live in that file and the model never sees them (it sees only `{{price.*}}`/`{{limit.*}}` tokens, Decision 8).

**Contents:** [Prompt anatomy](#prompt-anatomy) · [The system prompt block-by-block](#the-system-prompt-block-by-block) · [The dynamic suffix](#the-dynamic-suffix) · [A full assembled prompt](#a-full-assembled-prompt) · [The OpenRouter call](#the-llm-call-openrouter) · [Window & "summary"](#window--summary) · [Knowledge base in the prompt](#knowledge-base-in-the-prompt) · [Output contract + filled examples](#output-contract--filled-examples) · [Post-processing walkthrough](#post-processing-walkthrough) · [Worked examples gallery](#worked-examples-gallery) · [Language handling](#language-handling) · [Token budget & caching](#token-budget--caching) · [Implementation checklist](#implementation-checklist)

---

## Prompt anatomy

One LLM call per inbound message. The prompt = a **cached static prefix** (`[A]–[E]`, the same for every message until the content snapshot changes) + a **dynamic suffix** (this conversation's profile, window, and current message).

```
┌──────────────── SYSTEM (cached prefix — built from the content Snapshot) ────────────────┐
│ [A] FRAME       code-owned: role · output contract · hard rules        (from Go, fixed)   │
│ [B] IDENTITY    who the bot is, tone                                   (assistant_config)   │
│ [C] GUARDRAILS  must / must-not                                        (assistant_config)   │
│ [D] KNOWLEDGE   every topic body, both languages, price tokens intact  (kb_topics)   │
│ [E] MEDIA       the catalog as `ref | kind | topic | description`      (kb_assets)        │
│  ⟵ cache breakpoint here                                                                   │
└────────────────────────────────────────────────────────────────────────────────────────┘
┌──────────────── MESSAGES (dynamic — rebuilt every call) ─────────────────────────────────┐
│ one user turn: the PROFILE + the WINDOW transcript + the CURRENT MESSAGE                  │
└────────────────────────────────────────────────────────────────────────────────────────┘
   → model replies by calling the `emit_draft` tool (forced) → strict JSON (the contract)
```

`[A]` is assembled in Go and never editable; `[B]–[E]` are rendered from the in-memory `Snapshot` (`ContentSource.Get()`); the dynamic block is built per message from Chatwoot reads. See [02 · prompt assembly](02-assistant-brain.md#prompt-assembly).

---

## The system prompt block-by-block

### `[A]` FRAME (code-owned, never editable)

```
You are the drafting engine for xpayment's WhatsApp sales assistant. You write ONE reply
draft that a human will review and send. You never send messages yourself.

Rules (hard, non-negotiable):
1. Answer ONLY from the KNOWLEDGE BASE below. If the answer is not there, do not guess —
   set "escalate": true with a short escalation_reason and a brief holding reply.
2. NEVER write a price or a number of cashiers as a digit. Use the tokens exactly as written
   in the knowledge base: {{price.launch}}, {{price.growth}}, {{price.scale}},
   {{limit.launch}}, {{limit.growth}}, {{limit.scale}}, etc. Code fills the real values after you.
3. Attach media ONLY by returning refs that exist in the MEDIA CATALOG. Maximum 3. If none fit, [].
4. Reply in the customer's language. If the latest message mixes Kazakh and Russian, reply in Russian.
5. Keep the reply under ~120 words, warm and concrete. One clear next step or question.
6. Never ask for or repeat passwords. When trust comes up, use the "cashier role" explanation from the KB.
7. Extract into profile_patch ONLY facts you are newly confident about. Do not invent fields.

You MUST respond by calling the `emit_draft` tool with the required JSON. No prose outside the tool call.
```

### `[B]` IDENTITY (from `assistant_config` → `persona`/`mission`)

```
You are "xpayment-ассистент" — a friendly, competent sales assistant for Kazakhstani merchants.
Tone: helpful, concrete, no fluff, no hard-selling. You sound like a knowledgeable colleague,
not a brochure. You explain simply (most customers are non-technical business owners).
Mission: help the merchant understand xpayment, pick the right tariff, and take the next step
(test it, add a cashier, or talk to a human) — while quietly learning what kind of business they are.
```
*(Lives in `assistant_config`; edited in the admin UI — [08](08-admin-ui.md).)*

### `[C]` GUARDRAILS (from `assistant_config` → `guardrails[]`)

```
- Never promise features not in the knowledge base.
- Never quote a price you weren't given as a token; never do mental math on prices.
- Never claim to be an official Kaspi partner.
- If asked about legality/contracts/refund disputes or the customer is angry → escalate.
- Don't push tariffs; recommend the smallest plan that fits the stated volume.
- Always offer a concrete next step (a link, an image, or "хотите, подключим за 5 минут?").
```

### `[D]` KNOWLEDGE BASE (rendered from `kb_topics`, both languages, tokens intact)

Each topic appears as its markdown body. Examples (abbreviated):

```markdown
# topic: tariffs (ru)
xpayment подключает приём оплаты через Kaspi за минуты: QR, ссылка на оплату и счёт в чат.
Деньги приходят сразу на ваш счёт Kaspi — мы не храним ваши средства.
Тарифы:
- Пробный — бесплатно, 3 дня.
- Launch — {{price.launch}}/мес, до {{limit.launch}} кассы. Для небольших магазинов.
- Growth — {{price.growth}}/мес, до {{limit.growth}} касс. Для растущего оборота.
- Scale — {{price.scale}}/мес, до {{limit.scale}} касс. Для большого объёма.

# topic: security (ru)  — the cashier-role trust story
Это безопасно: вы создаёте в Kaspi роль «Кассир», а не передаёте пароль. Кассир может
только принимать платежи — не может переводить деньги, видеть баланс или менять настройки.
Даже при компрометации доступа вывести средства нельзя.

# topic: add_cashier (kk)  — onboarding
Kaspi-де виртуалды кассирді қосу — 2 минут:
1. Kaspi Pay-да «Кассирлер» бөліміне кіріңіз.
2. Жаңа кассир қосып, рөлін «Кассир» етіп таңдаңыз (тек төлем қабылдау).
3. xpayment берген кодты енгізіп, SMS-тегі OTP-ны растаңыз.
Кассир тек төлем қабылдай алады — ақша аудара алмайды.
```

### `[E]` MEDIA CATALOG (rendered from `kb_assets` — the selection menu)

```
ref                   | kind             | topic      | description (what the model reads to choose)
----------------------|------------------|------------|------------------------------------------------------
tariffs_table_ru      | image            | tariffs    | Инфографика всех тарифов: цена + лимит касс (RU). Для вопросов о тарифах/выборе плана.
how_it_works_ru       | image            | overview   | Схема Kaspi→устройство→API→продукт (RU). Для общего "как это работает".
add_cashier_video_kk  | screen_recording | onboarding | 30-сек запись (KK): как добавить кассира и ввести OTP. Для вопроса "как подключить кассу".
qr_flow_ru            | image            | payments   | Как покупатель платит по QR (RU). Для вопросов про QR-оплату.
cashier_role_ru       | image            | security   | Что может и не может роль «Кассир» (RU). Для возражений про безопасность/пароль.
```

The model returns `asset_refs` (e.g. `["tariffs_table_ru"]`); Go resolves each to a URL ([Post-processing](#post-processing-walkthrough)). It can never invent a URL — only pick a ref from this menu.

---

## The dynamic suffix

Rebuilt every call from Chatwoot reads ([02 · context-on-read](02-assistant-brain.md#context-on-read)), packed into **one user turn** (see [why one turn](#why-the-window-is-one-user-turn-not-roleplayed)):

```
PROFILE (what we already know about this contact):
{ "business_type": "интернет-магазин", "monthly_volume_tenge": 1500000,
  "preferred_language": "ru", "urgency": "this_month" }

CONVERSATION (most recent messages, oldest first):
customer: Здравствуйте, нужно принимать оплату на сайте
agent:    Здравствуйте! xpayment подключает приём Kaspi за пару минут — по QR, ссылке или счётом в чат. Какой примерно оборот?
customer: ~1.5 млн в месяц, интернет-магазин
agent:    Отлично, это вам отлично подойдёт. Что ещё подсказать?

CURRENT MESSAGE:
customer: А сколько касс можно на Growth и сколько он стоит?
```

---

## A full assembled prompt

Putting it together for the case above (50th message; the window holds only the last few, the profile carries the early facts):

```
SYSTEM  = [A] FRAME
        + [B] IDENTITY
        + [C] GUARDRAILS
        + "KNOWLEDGE BASE:\n" + [D] all topic bodies (ru+kk, tokens intact)
        + "MEDIA CATALOG:\n"  + [E] the ref|kind|topic|description menu
        (the SYSTEM string is the large stable prefix; cached by the provider where supported)

MESSAGES = [ { role: "user", content: PROFILE + CONVERSATION + CURRENT MESSAGE (the block above) } ]

TOOLS    = [ emit_draft ]   ;  tool_choice = force emit_draft
```

---

## The LLM call (OpenRouter)

The `llm.Drafter` adapter ([02 · ports](02-assistant-brain.md#ports), [05 · LLM vars](05-configuration.md#llm-openrouter-provider-neutral--decision-13-10)) issues an **OpenAI-compatible** `chat/completions` call to **OpenRouter** (`LLM_BASE_URL`), with the large stable prefix as the **system message** and **function-calling** forcing strict JSON.

```jsonc
POST {{LLM_BASE_URL}}/chat/completions          // OpenRouter — OpenAI-compatible
Authorization: Bearer {{LLM_API_KEY}}
{
  "model": "{{LLM_MODEL}}",                      // e.g. "anthropic/claude-sonnet-4" — an OpenRouter model id
  "max_tokens": 1024,                            // LLM_MAX_TOKENS
  "temperature": 0.3,                            // LLM_TEMPERATURE
  "messages": [
    { "role": "system", "content": "[A]…[B]…[C]…\nKNOWLEDGE BASE:\n…[D]…\nMEDIA CATALOG:\n…[E]…" },
    { "role": "user",   "content": "PROFILE:\n{…}\n\nCONVERSATION (oldest first):\ncustomer: …\nagent: …\n\nCURRENT MESSAGE:\ncustomer: А сколько касс на Growth и сколько он стоит?" }
  ],
  "tools": [
    { "type": "function",
      "function": {
        "name": "emit_draft",
        "description": "Return exactly one reply draft for the human to review.",
        "parameters": {
          "type": "object",
          "properties": {
            "reply_text":        { "type": "string", "description": "Uses {{price.*}}/{{limit.*}} tokens, never digits." },
            "reply_language":    { "type": "string", "enum": ["ru","kk"] },
            "asset_refs":        { "type": "array", "items": { "type": "string" }, "maxItems": 3 },
            "profile_patch":     { "type": "object", "description": "Only newly-confident fields." },
            "suggested_status":  { "type": "object", "properties": { "stage": { "type": "string" } } },
            "confidence":        { "type": "number" },
            "escalate":          { "type": "boolean" },
            "escalation_reason": { "type": "string" }
          },
          "required": ["reply_text","reply_language","asset_refs","confidence","escalate"]
        }
      }
    }
  ],
  "tool_choice": { "type": "function", "function": { "name": "emit_draft" } }   // force the JSON
}
```

The reply comes back as `choices[0].message.tool_calls[0].function.arguments` — a JSON **string**; parse it into `RawDraft` (defensively).

**Provider portability & caching.** Because this is the OpenAI-compatible shape, switching model/provider is just `LLM_MODEL`. **Prompt caching is provider-dependent**, not a universal field — some providers cache automatically; Anthropic models via OpenRouter accept an opt-in `cache_control` marker. Keep the big prefix **first and stable** so any caching provider can use it, but **don't budget on cache savings** at ~100 low-frequency leads ([01 · prompt-cache caveat](01-infrastructure.md#prompt-cache-caveat)). If a model lacks function-calling, fall back to `response_format: {"type":"json_object"}` + a schema instruction.

### Go drafter sketch (consistent with `Drafter.Draft(ctx, system, window, msg)`)

```go
func (d *Drafter) Draft(ctx context.Context, system string, window []Message, msg Message) (RawDraft, error) {
    user := buildUserBlock(d.profileJSON, window, msg)            // PROFILE + transcript + CURRENT MESSAGE
    resp, err := d.client.ChatCompletions(ctx, llm.Request{       // OpenAI-compatible client → OpenRouter
        Model: d.model, MaxTokens: d.maxTokens, Temperature: d.temperature,
        Messages: []llm.Msg{{Role: "system", Content: system}, {Role: "user", Content: user}},
        Tools:    []llm.Tool{emitDraftFn},
        ToolChoice: llm.ForceFunction("emit_draft"),
    })
    if err != nil { return RawDraft{}, fmt.Errorf("llm draft: %w", err) }
    return decodeFunctionArgs[RawDraft](resp)                     // tool_calls[0].function.arguments
}
```
The **`system` string is built by the `assistant` usecase from `ContentSource.Get()`** (the SQLite snapshot), so it changes only on publish — which keeps it stable for any provider-side caching.

### Why the window is one user turn (not role-played)

Past `agent:` lines were **typed by humans**, not by this model. If we replayed them as `assistant` turns, the model would think it had written them and imitate their style/promises. So the whole window is a **labeled transcript inside a single `user` block**. Cleaner, and no role confusion.

---

## Window & "summary"

- **The window** is the last ~15 messages (or 48h) read from Chatwoot via `ChatwootReader.Window` ([02 · memory](02-assistant-brain.md#memory)). It's a recency view.
- **No running summary in v1** (Decision 9). The reason it still works on a 50-message chat: **the profile *is* the durable summary.** Early facts (business type, volume) were captured into `profile_patch` and live on the contact, so they survive even after they scroll out of the 15-message window.

```
Turn 2:  customer states "интернет-магазин, ~1.5 млн/мес"  → profile_patch sets business_type + volume
Turn 50: window no longer contains turn 2, BUT the PROFILE block still carries those facts
         → the model stays contextual without a summarizer
```

- **The one gap:** things that matter but aren't profile fields (a specific promise a rep made, the emotional tenor) can drop out of the window. Harmless in suggest-only v1 (a human reads the full Chatwoot thread before sending); **re-evaluate before Phase-3 auto-send**. The documented next step then is a single rolling-summary **conversation custom attribute** in Chatwoot — still no worker ([02 · open questions](02-assistant-brain.md#open-questions)).

---

## Knowledge base in the prompt

- The whole KB + whole media catalog go into the cached system block — **no retrieval, no vector DB** (Decision 7, LLM-as-selector). At a few dozen topics this fits comfortably and is fully predictable.
- **Price tokens stay intact** in `[D]`; the model copies them verbatim into `reply_text`; Go renders them after ([Post-processing](#post-processing-walkthrough)). The model is told (rule 2) it must not emit digits.
- **Media selection:** the model reads each `description` in `[E]` and returns the matching `ref`(s). The description is the only thing it sees — so descriptions are written *for the model* ("Для вопросов о тарифах/выборе плана").
- **Growth path** (only if the KB ever outgrows a comfortable prompt): classify the message to a topic, then load only that topic's text — still behind the same `ContentSource`/snapshot, no schema change.

---

## Output contract + filled examples

The `emit_draft` tool input (the contract, [02 · JSON output contract](02-assistant-brain.md#json-output-contract)). Filled examples:

**(a) Normal pricing answer (RU) — tokens intact, one media, profile learned:**
```json
{ "reply_text": "На тарифе Growth — до {{limit.growth}} касс, {{price.growth}}/мес. Для интернет-магазина с оборотом ~1.5 млн ₸ этого обычно хватает. Показать, как подключить кассу?",
  "reply_language": "ru",
  "asset_refs": ["tariffs_table_ru"],
  "profile_patch": { "interested_tariff": "growth" },
  "suggested_callback": { "due_at": null, "note": "" },
  "suggested_status": { "stage": "qualifying" },
  "confidence": 0.83, "escalate": false, "escalation_reason": "" }
```

**(b) Off-KB → escalate (no guessing):**
```json
{ "reply_text": "Уточню это у коллеги и вернусь с точным ответом — буквально пару минут.",
  "reply_language": "ru", "asset_refs": [], "profile_patch": {},
  "suggested_callback": null, "suggested_status": { "stage": "engaged" },
  "confidence": 0.32, "escalate": true,
  "escalation_reason": "Запрос про интеграцию с 1С — нет в базе знаний." }
```

**(c) Onboarding (KK) → attaches the screen-recording:**
```json
{ "reply_text": "Кассирді қосу — 2 минут. Kaspi Pay → «Кассирлер» → жаңа кассир (рөлі «Кассир») → xpayment коды + OTP. Бейнеден көрсетейін:",
  "reply_language": "kk",
  "asset_refs": ["add_cashier_video_kk"],
  "profile_patch": { "technical_level": "non_technical" },
  "suggested_callback": null, "suggested_status": { "stage": "qualifying" },
  "confidence": 0.88, "escalate": false, "escalation_reason": "" }
```

---

## Post-processing walkthrough

Take output **(a)** and run [02 · post-processing](02-assistant-brain.md#post-processing-pipeline) (illustrative `tariffs`: `growth.price_tenge=19900`, `growth.cashier_limit=5`):

```
1. parse           → valid JSON from tool_calls[0].function.arguments
2. escalate gate   → escalate=false → continue
3. resolve refs    → ["tariffs_table_ru"] exists → {kind:image, url: <MEDIA_BASE_URL>/tariffs/pricing-ru.png}
                     (unknown ref would be dropped + logged; cap 3)
4. inject prices   → PriceBook.Render(reply_text, "ru"):
                       {{limit.growth}} → "5"        {{price.growth}} → "19 900 ₸"
                     → "На тарифе Growth — до 5 касс, 19 900 ₸/мес. Для интернет-магазина …"
                     (any leftover {{…}} or unknown token → "check pricing manually" note, stop)
5. merge profile   → contact attrs += { interested_tariff: "growth" }  (overwrite-if-confident, never null)
6. apply status    → conversation label "qualifying"
7. outcome (v1)    → ChatwootWriter.PostPrivateNote(chatID, rendered reply + media URL)  → human reviews & sends
```

**Customer-visible result (after a human taps send):**
> На тарифе Growth — до 5 касс, 19 900 ₸/мес. Для интернет-магазина с оборотом ~1.5 млн ₸ этого обычно хватает. Показать, как подключить кассу? *(+ инфографика тарифов)*

Note the model never wrote "5" or "19 900" — Go did.

---

## Worked examples gallery

Each shows the **current message → model JSON (abridged) → what the rep sees** (post-processed).

**1) Tariff + cashier count (RU)** — see (a) above. Rep sees prices injected + tariff infographic; profile gains `interested_tariff:growth`.

**2) "Как подключить кассу?" (KK)** — output (c). Rep sees the KK steps + `add_cashier_video_kk`; profile gains `technical_level:non_technical`.

**3) Security objection (RU)** — *"А это не опасно давать доступ к Kaspi?"*
```json
{ "reply_text": "Вы не передаёте пароль — создаёте роль «Кассир». Она только принимает платежи, не переводит деньги и не видит баланс. Это штатная роль Kaspi.",
  "reply_language": "ru", "asset_refs": ["cashier_role_ru"],
  "profile_patch": { "main_objection": "security_credentials" },
  "suggested_status": { "stage": "engaged" }, "confidence": 0.9, "escalate": false }
```
Rep sees the trust answer + the cashier-role image; profile records the objection.

**4) Returning customer, brand-new conversation** — window = 1 message, profile = rich.
*"Здравствуйте, я снова по поводу подключения"* → because the **profile** carries `business_type/volume/interested_tariff`, the model greets them in context (e.g. "Рад снова видеть! По Growth для вашего магазина — подключаем?") even though the window is empty. This is the payoff of contact-scoped profile ([Window & summary](#window--summary)).

**5) Code-switching (mixed KK/RU)** — *"Сәлеметсіз бе, мне нужно онлайн оплата на сайт"* → rule 4 → `reply_language: "ru"`; a normal RU answer about QR/link/site integration.

**6) Pricing negotiation (escalate)** — *"А скидку дадите если оплачу за год?"* → `escalate:true`, `escalation_reason:"Переговоры по цене/скидке"`; holding reply only — pricing negotiation always goes to a human ([09 · operating procedure](09-product-and-ops.md#operating-procedure)).

---

## Language handling

1. **Detect** the language of the *latest* customer message (cheap: Kazakh-specific letters ә/қ/ұ/ң/ө/ғ/і/һ → `kk`; otherwise Cyrillic → `ru`; the model also returns `reply_language` as the authoritative signal).
2. **Mixed message → Russian** (rule 4 / persona `language_policy`).
3. **KB is authored in both languages** (`tariffs.ru.md` + `tariffs.kk.md`); the model picks the language to answer in and pulls from the matching topic body.
4. **Pricing is never translated on the fly** — it's rendered from `tariffs` per language in step 4 of post-processing (e.g. `₸` formatting), so a missing KK topic never produces a mistranslated price.

---

## Token budget & caching

| Part | Rough size | Cached? |
|---|---|---|
| `[A]`–`[C]` frame+persona+guardrails | ~400–800 tokens | ✅ (system block) |
| `[D]` knowledge base (dozens of topics, ru+kk) | ~2–4k tokens | ✅ |
| `[E]` media catalog (dozens of refs) | ~0.5–1.5k tokens | ✅ |
| dynamic: profile + window (~15 msgs) + current | ~300–800 tokens | ❌ |
| output (the draft JSON) | ≤ ~300 tokens (`max_tokens` 1024) | — |

The cached prefix is reused across messages **while the snapshot is unchanged and within the cache TTL** (~5 min). At ~100 low-frequency leads, two messages rarely fall in one TTL window, so **cache savings are modest** — keep caching for latency and intra-conversation bursts, but don't budget on it ([01 · prompt-cache caveat](01-infrastructure.md#prompt-cache-caveat)). A content reload (git push) invalidates the cache by changing the system string — expected and cheap.

---

## Implementation checklist

1. **Snapshot → system string.** `assistant` usecase reads `ContentSource.Get()` and renders `[A]` (Go const) + `[B][C]` (`assistant_config`) + `[D]` (all `kb_topics` bodies) + `[E]` (`kb_assets` menu) into one cache-stable string. Rebuild only on reload.
2. **Read context.** `ChatwootReader.Window` + `ChatwootReader.Profile` (by `chatID`) → build the user block (PROFILE + transcript + CURRENT MESSAGE).
3. **Draft.** `Drafter.Draft(system, window, msg)` → OpenRouter chat/completions with a forced `emit_draft` function → `RawDraft`.
4. **Post-process** (Go, never the model): parse → escalate gate → `Catalog.Resolve(asset_refs)` → `Prices.Render` → additive `profile_patch` merge → status label → `PostPrivateNote`.
5. **Test** with the [07 golden set](07-testing-and-evals.md#golden-set-eval-harness) (the cases here are good seeds) and the [Playground CLI](08-admin-ui.md#playground-test-before-you-publish).

---

## Open questions

- **Forced tool vs. structured-output mode** — both give strict JSON; pick per SDK support. Keep the defensive parse either way.
- **Window size / rolling summary** — tune on mined chats; revisit before auto-send ([02](02-assistant-brain.md#open-questions)).
- **Persona length** — `[B]`/`[C]` are cached, so richer persona is cheap; keep it focused to avoid diluting the rules in `[A]`.
