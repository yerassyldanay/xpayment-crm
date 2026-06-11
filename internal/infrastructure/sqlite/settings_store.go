package sqlite

import "fmt"

// This file implements the settings.Store port: a small key/value table backing
// the admin Settings page (Evolution/Chatwoot bridge connection config).

// BridgeSettings returns all persisted settings as a key→value map.
func (s *Store) BridgeSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SaveBridgeSettings upserts every provided key/value in one transaction.
func (s *Store) SaveBridgeSettings(values map[string]string, actor string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range values {
		if _, err := tx.Exec(`INSERT INTO app_settings (key, value, updated_at)
			VALUES (?,?,datetime('now'))
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=datetime('now')`, k, v); err != nil {
			return fmt.Errorf("save setting %q: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.audit(actor, "bridge_settings_save", "evolution/chatwoot connection updated")
	return nil
}
