-- 0004_whatsapp_instances.sql — Evolution/Chatwoot bridge state managed by
-- the brain admin UI. Conversation data still lives in Chatwoot.

CREATE TABLE IF NOT EXISTS managed_whatsapp_instances (
    instance_name       TEXT PRIMARY KEY,
    inbox_id            INTEGER NOT NULL DEFAULT 0,
    inbox_name          TEXT    NOT NULL DEFAULT '',
    owner_jid           TEXT    NOT NULL DEFAULT '',
    connection_state    TEXT    NOT NULL DEFAULT '',
    chatwoot_enabled    INTEGER NOT NULL DEFAULT 0,
    bridge_enabled      INTEGER NOT NULL DEFAULT 0,
    ai_enabled          INTEGER NOT NULL DEFAULT 0,
    last_audit_status   TEXT    NOT NULL DEFAULT '',
    last_audit_detail   TEXT    NOT NULL DEFAULT '',
    last_checked_at     TEXT,
    attached_at         TEXT,
    detached_at         TEXT,
    updated_at          TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS managed_whatsapp_instances_ai_inbox
    ON managed_whatsapp_instances (ai_enabled, inbox_id);
