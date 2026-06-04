-- 0001_init.sql — the embedded SQLite schema (doc 03-content-and-data.md).
-- Money is an INTEGER (tenge). Applied by the in-process migrator on startup.

-- the bot's "brain config" — versioned for draft/publish/rollback
CREATE TABLE IF NOT EXISTS assistant_config (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    version         INTEGER NOT NULL,
    status          TEXT    NOT NULL CHECK (status IN ('draft','published','archived')),
    persona         TEXT    NOT NULL,
    mission         TEXT    NOT NULL DEFAULT '',
    guardrails      TEXT    NOT NULL,             -- newline- or JSON-list of rules
    language_policy TEXT    NOT NULL DEFAULT '',
    reply_max_words INTEGER NOT NULL DEFAULT 120,
    enabled_tools   TEXT    NOT NULL DEFAULT '[]',-- JSON array
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    published_at    TEXT
);
-- at most one published config at a time
CREATE UNIQUE INDEX IF NOT EXISTS assistant_config_one_published
    ON assistant_config (status) WHERE status='published';

-- knowledge topics — one row per (slug, language); body uses PRICE TOKENS, never numerals
CREATE TABLE IF NOT EXISTS kb_topics (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT NOT NULL,
    language   TEXT NOT NULL CHECK (language IN ('ru','kk')),
    title      TEXT NOT NULL,
    summary    TEXT NOT NULL DEFAULT '',          -- helps the model pick the topic
    body_md    TEXT NOT NULL,                      -- tokens only (Decision 8)
    active     INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (slug, language)
);

-- media catalog (metadata; the "menu" the model selects from). Binaries live elsewhere.
CREATE TABLE IF NOT EXISTS kb_assets (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ref         TEXT NOT NULL UNIQUE,              -- the stable slug the model returns
    topic_slug  TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('image','video','screen_recording','gif','link','document')),
    url         TEXT NOT NULL,                     -- served-dir URL or external URL
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL,                      -- WRITTEN FOR THE LLM — the selection menu entry
    language    TEXT NOT NULL DEFAULT 'any' CHECK (language IN ('ru','kk','any')),
    active      INTEGER NOT NULL DEFAULT 1,
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- prices — the SINGLE source of numbers (Decision 8)
CREATE TABLE IF NOT EXISTS tariffs (
    key           TEXT PRIMARY KEY,                -- 'launch','growth','scale'
    price_tenge   INTEGER NOT NULL,                -- integer tenge
    cashier_limit INTEGER NOT NULL,
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

-- non-tariff tokens, bilingual
CREATE TABLE IF NOT EXISTS placeholders (
    token    TEXT PRIMARY KEY,                     -- e.g. 'support.phone'
    value_ru TEXT NOT NULL,
    value_kk TEXT NOT NULL
);

-- audit + (optional) operational state
CREATE TABLE IF NOT EXISTS audit_log (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    at     TEXT NOT NULL DEFAULT (datetime('now')),
    actor  TEXT,
    action TEXT,
    detail TEXT
);
CREATE TABLE IF NOT EXISTS processed_messages (
    chatwoot_message_id TEXT PRIMARY KEY,
    at                  TEXT NOT NULL DEFAULT (datetime('now'))
);
