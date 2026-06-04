# 07 · Testing & Evals

How we know the brain is correct. Two layers: ordinary **Go tests** (reusing the main repo's conventions) and a **golden-set eval harness** (net-new — neither repo has LLM-eval precedent). Runtime behavior under test is in [02-assistant-brain.md](02-assistant-brain.md); the things being asserted (prices, refs, profile) are defined in [03-content-and-data.md](03-content-and-data.md).

---

## Unit tests

The core (`HandleMessage`) depends only on ports (Decision 10), so the whole pipeline runs with **mocked ports and zero external services** — exactly the property that makes the channel-agnostic claim testable.

**Mocks** — mockery + testify, same config as `xpayment/.mockery.yml` (`with-expecter: true`, `dir: '{{.InterfaceDir}}/mocks'`, `structname: 'Mock{{.InterfaceName}}'`). Add the brain's ports (match your `go.mod` module path):

```yaml
packages:
  github.com/yerassyldanay/xpayment-crm/internal/usecase/assistant:
    interfaces:
      ChatwootReader:
      ChatwootWriter:
      Drafter:
      KnowledgeRetriever:
      ConfigStore:
      Prices:
```
Regenerate with a `make mocks` target (reuse the main repo's).

**Style** — copy `xpayment/internal/usecase/payment/service_test.go`: a `newBrain(t)` helper that wires the mocks, table-driven `t.Run`, `require` assertions, `.EXPECT()…Return(…).Once()`:

```go
func TestHandleMessage_PriceSafety(t *testing.T) {
    reader  := mocks.NewMockChatwootReader(t)
    drafter := mocks.NewMockDrafter(t)
    // … other ports …
    reader.EXPECT().Window(mock.Anything, chatID).Return(window, nil)
    reader.EXPECT().Profile(mock.Anything, chatID).Return(profile, nil)
    // The model returns TOKENS, never numerals (Decision 8):
    drafter.EXPECT().Draft(mock.Anything, mock.Anything).
        Return(assistant.RawDraft{ReplyText: "до {{limit.growth}} касс, {{price.growth}}/мес"}, nil).Once()

    b := newBrain(reader, drafter /*, …*/)
    draft, err := b.HandleMessage(context.Background(), chatID, msg)

    require.NoError(t, err)
    require.NotContains(t, draft.ReplyText, "{{")          // every token rendered
    require.Contains(t, draft.ReplyText, "25 000 ₸")       // injected by the stub PriceBook
}
```

### Must-pass behavioral tests (the correctness guarantees)

Each is one focused test; together they pin the non-negotiable behaviors:

| Test | Asserts |
|---|---|
| **Price-safety** | Model output carries only `{{…}}` tokens; the `Draft` has them rendered and contains **no model-authored numeral**; a leftover/unknown token → "check pricing manually" note, not a half-rendered price (Decision 8). |
| **Asset-ref validation** | Unknown/hallucinated refs are dropped + logged; >3 refs capped; wrong-topic ref flagged ([02 · post-processing](02-assistant-brain.md#post-processing-pipeline)). |
| **Language** | Russian message → `reply_language: ru`; Kazakh → `kk`; mixed → Russian (the persona rule). |
| **Escalation gate** | `escalate:true` (or low confidence / off-KB) → a flag note and **no auto-anything**, no media. |
| **Additive merge** | A `profile_patch` adds/updates fields but **never nulls** a known one; the `stage` key is routed to a label, not stored as an attribute (Decision 9). |
| **Ignore rule** | An outgoing/private/other-inbox webhook is a no-op `200` (no LLM call) — guards against loops ([06 · classification](06-api-and-contracts.md#the-message_created-payload)). |

---

## Integration tests

- **Brain DB** — testcontainers Postgres, exactly like `xpayment/tests/integration/{main_test.go,fixtures_test.go}`: a package `TestMain` boots `postgres:16-alpine`, runs `goose.Up`, and a `truncateAll(t)` resets between tests. Use this to test the `postgres` repos (config/KB/media/prices) against real SQL.
- **Chatwoot adapter** — contract tests for `chatwoot.Reader/Writer`: either record real responses into `testdata/` and replay with `httptest`, or run a disposable Chatwoot from the local compose ([04](04-service-and-deployment.md#local-stack-docker-compose)) and assert a posted private note + merged attributes actually appear. This is where the "verify against your version" risks in [06](06-api-and-contracts.md) get pinned down.

---

## Golden-set eval harness

The unit tests prove *mechanics*; the golden set proves *answer quality* on **real questions mined from the ~100 chats** (the load-bearing Phase-1 task — [README roadmap](README.md#roadmap)).

**Cases** — `testdata/golden/*.json`, one per real question:
```json
{
  "id": "tariffs-cashier-count-ru-01",
  "language": "ru",
  "window": [{"role": "customer", "text": "А на каком тарифе сколько касс?"}],
  "profile": {"business_type": "интернет-магазин"},
  "expect": {
    "topic": "tariffs",
    "asset_refs": ["pricing_table"],
    "language": "ru",
    "escalate": false,
    "price_safe": true,
    "answer_must_include": ["Рост"],
    "answer_must_not_include": ["%"]
  }
}
```

**Runner** — a Go test behind a build tag (`//go:build eval`) so it never runs in the normal `go test ./...` (it needs `ANTHROPIC_API_KEY` and costs tokens). For each case it calls `HandleMessage` with a **real Drafter** (live LLM, low temperature) + a stub `ChatwootReader` returning the case's `window`/`profile` + the real KB/PriceBook, then scores:

- **Deterministic metrics (exact / set-precision):** `asset_refs` precision & recall vs expected; `escalate` match; **price-safety** (no model numeral, no leftover token); language match; `answer_must_include/exclude` substring checks.
- **Prose quality (LLM-as-judge):** a separate judge call scores the reply against a rubric (accuracy vs KB, tone, concision, no invented facts) on a 1–5 scale.

**Report & gate** — emit a per-metric scorecard; **publishing a config** ([03 · admin lifecycle](03-content-and-data.md#admin--config-lifecycle)) is gated on the suite meeting thresholds (e.g. media-precision ≥ 0.9, price-safety = 1.0, judge-mean ≥ 4.0). Media-selection precision is scored **separately** — attaching the wrong pricing image is a correctness failure, not a style nit.

**Nondeterminism** — pin the model, use low temperature, judge with a fixed rubric, and assert **ranges/thresholds not equality** on prose. For a flaky case, run N times and require k-of-N. Keep the golden set in git so changes are reviewable.

---

## CI

Neither repo has CI today. Add a minimal GitHub Actions workflow for the brain:

```yaml
# .github/workflows/ci.yml  (illustrative)
on: [push, pull_request]
jobs:
  test:
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5         # go 1.25
      - run: golangci-lint run            # add a .golangci.yml (the main repo has none)
      - run: go test ./...                # unit + (containerized) integration
      - run: docker build .               # the Dockerfile builds
```
**Evals are not in PR CI** (they need a live key + cost). Run them **nightly or manually**, and as the **publish gate** in the admin flow.

---

## Definition of Done (per phase)

| Phase | Done when |
|---|---|
| **1 · Crawl** | WhatsApp↔Chatwoot round-trips both ways ([01 test](01-infrastructure.md#why-private-notes-are-safe)); ~100 chats imported **and mined** into a topic list + golden cases; labels/snooze/merge/canned configured; contact custom attributes pre-defined. |
| **2 · Walk** | `HandleMessage` unit suite green incl. all must-pass tests; golden gate met in the Playground; a real inbound produces a correct-language **private-note draft** with the right media and a merged profile; the ignore rule prevents loops. |
| **3 · Run** | Auto-send fires **only** above the calibrated confidence threshold and only after the golden gate; ban-safe outbound pacing in place; Evolution→Cloud-API path verified. |

---

## Open questions

- **Eval thresholds** — the exact pass bars (media-precision, judge-mean, price-safety) and k-of-N for flaky cases.
- **Judge model** — same model as drafting, or a different one, for the LLM-as-judge.
- **Golden-set size & refresh** — how many cases, and how often re-mined as conversations accumulate.
- **Frontend tests** — the admin UI has no runner yet ([08](08-admin-ui.md)); add vitest or accept manual + Playground for v1.
