# 08 · Admin & Content Workflow (GitOps)

How the team edits the bot's brain — persona, knowledge, prices, media — and tests changes before they go live. There is **no admin UI in v1**: the admin surface is **git on `xpayment-content`** (Decision 12). The file shapes and the snapshot/validation are in [03-content-and-data.md](03-content-and-data.md); the HTTP reload hook + Playground are in [06-api-and-contracts.md](06-api-and-contracts.md).

---

## No UI in v1 — git is the admin surface

Because the brain is stateless and file-backed (Decision 2), everything an admin would change lives in `xpayment-content`. Editing those files with normal git tooling **is** the admin workflow — so there is **no admin API, no Vue admin, and no cross-service auth** to build for v1. Access control is git access control (repo permissions, branch protection, PR review). A polished UI can come later (below), but it is not needed to operate the bot.

---

## How operators change things

| To change… | Edit | Then |
|---|---|---|
| Persona / guardrails / model settings | `assistant.json` | commit |
| A price or cashier limit | `pricing.json` | commit (review-gated) |
| An answer | the topic's markdown in `knowledge/` | commit |
| Add a media file | drop the binary in `media/` **and** add an entry to `media.json` | commit both |
| Remove a media file | delete the binary **and** its `media.json` entry | commit both |

The two-step for media (binary **and** `media.json`) is enforced by the load-time validator: a `media.json` entry whose `file` is missing fails the snapshot ([03 · validate on load](03-content-and-data.md#validate-on-load-fail-loudly)).

---

## `git status` / `git diff` = the review surface

Every change is visible before it ships:

```console
$ git status
On branch main
Changes not staged for commit:
  modified:   assistant.json
  modified:   pricing.json
  modified:   media.json
  deleted:    media/onboarding/old-setup.mp4
Untracked files:
  media/onboarding/new-setup.mp4

$ git diff pricing.json
-    "growth": { "price_tenge": 19900, "cashier_limit": 5 },
+    "growth": { "price_tenge": 22900, "cashier_limit": 5 },
```

## The lifecycle maps onto git

| Lifecycle | Git action |
|---|---|
| **Draft** | a branch, or just the working copy |
| **Publish** | merge to `main` + push |
| **Rollback** | `git revert <commit>` |
| **Audit** ("who raised the price, when") | `git log` / `git blame pricing.json` |
| **Review** | a pull request (recommended for `pricing.json`) |

Protect `main` and add CODEOWNERS on `pricing.json` so price changes require review — that is the whole price-governance story (Decision 8).

---

## Reload-on-change

The brain reloads when `xpayment-content` changes: a **GitHub push webhook** ([06 · reload webhook](06-api-and-contracts.md#github-reload-webhook)) triggers `git pull` → build a **new** `Snapshot` → **validate** → atomically swap only if valid (keep the old snapshot + log on failure). A `--reload` signal and a poll fallback are also supported. So **publish = merge + push**, and the bot picks it up within seconds — or rejects a broken change and keeps serving the last good config.

---

## Playground CLI — test before you commit

A CLI mode builds a `Snapshot` from the **working copy, including uncommitted edits**, and runs the real `HandleMessage` against it with a **mockable LLM and Chatwoot**, printing the resulting `Draft` (reply text with prices injected, chosen media, extracted profile, confidence). This is how an editor checks a change *before* committing:

```console
$ brain playground --content ./xpayment-content \
    --lang ru --message "А сколько касс на тарифе Рост и сколько стоит?"
draft (ru):  На тарифе «Рост» — до 5 касс, 19 900 ₸/мес. …
media:       [tariffs_infographic_ru]
profile:     {interested_tariff: growth}
confidence:  0.82   escalate: false
```

Nothing is sent and no real conversation is touched. The same path powers the golden-set evals ([07-testing-and-evals.md](07-testing-and-evals.md)).

---

## A future UI (optional, later)

If non-technical editing is wanted, a thin admin UI can be added in Phase 3 that **reads and commits the same files** in `xpayment-content` via the GitHub API (or a server-side checkout). It would be a convenience layer over the git lifecycle — **the brain is unchanged**, since it still loads its snapshot from the repo. Until then, git + the Playground CLI are the workflow.

---

## Open questions

- **Merge rights** — who may merge to `xpayment-content` `main`, and is `pricing.json` CODEOWNERS-gated?
- **Media storage** — Git LFS vs object storage for video (also in [03](03-content-and-data.md#open-questions)).
- **Reload webhook security** — the shared secret on the GitHub push webhook ([06](06-api-and-contracts.md#github-reload-webhook)).
