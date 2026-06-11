-- 0005_app_settings.sql — key/value store for runtime-editable settings, used by
-- the admin Settings page to override the .env Evolution/Chatwoot bridge config.
-- Keys mirror the corresponding env var names (e.g. EVOLUTION_API_KEY).

CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
