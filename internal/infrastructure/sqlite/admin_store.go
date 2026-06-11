package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
	"github.com/yessaliyev/xpayment-crm/internal/usecase/admin"
)

// This file implements the admin.Store port (docs/08 lifecycle).

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) audit(actor, action, detail string) {
	_, _ = s.db.Exec(`INSERT INTO audit_log (actor, action, detail) VALUES (?,?,?)`, actor, action, detail)
}

// --- config lifecycle ---

func (s *Store) configView(status string) (*admin.ConfigView, error) {
	row := s.db.QueryRow(`SELECT id, version, status, persona, mission, guardrails, language_policy,
		reply_max_words, created_at, COALESCE(published_at,'')
		FROM assistant_config WHERE status=? ORDER BY version DESC LIMIT 1`, status)
	var c admin.ConfigView
	err := row.Scan(&c.ID, &c.Version, &c.Status, &c.Persona, &c.Mission, &c.Guardrails,
		&c.LanguagePolicy, &c.ReplyMaxWords, &c.CreatedAt, &c.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) DraftConfig() (*admin.ConfigView, error)     { return s.configView("draft") }
func (s *Store) PublishedConfig() (*admin.ConfigView, error) { return s.configView("published") }

func (s *Store) ConfigVersions() ([]admin.ConfigView, error) {
	rows, err := s.db.Query(`SELECT id, version, status, persona, mission, guardrails, language_policy,
		reply_max_words, created_at, COALESCE(published_at,'')
		FROM assistant_config ORDER BY version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.ConfigView
	for rows.Next() {
		var c admin.ConfigView
		if err := rows.Scan(&c.ID, &c.Version, &c.Status, &c.Persona, &c.Mission, &c.Guardrails,
			&c.LanguagePolicy, &c.ReplyMaxWords, &c.CreatedAt, &c.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveDraftConfig upserts the single working draft. If a draft exists it is
// updated; otherwise a new draft is created at maxVersion+1.
func (s *Store) SaveDraftConfig(in admin.ConfigInput, actor string) error {
	maxWords := in.ReplyMaxWords
	if maxWords == 0 {
		maxWords = 120
	}
	res, err := s.db.Exec(`UPDATE assistant_config
		SET persona=?, mission=?, guardrails=?, language_policy=?, reply_max_words=?
		WHERE status='draft'`, in.Persona, in.Mission, in.Guardrails, in.LanguagePolicy, maxWords)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var maxV sql.NullInt64
		_ = s.db.QueryRow(`SELECT MAX(version) FROM assistant_config`).Scan(&maxV)
		_, err = s.db.Exec(`INSERT INTO assistant_config
			(version, status, persona, mission, guardrails, language_policy, reply_max_words)
			VALUES (?, 'draft', ?, ?, ?, ?, ?)`,
			maxV.Int64+1, in.Persona, in.Mission, in.Guardrails, in.LanguagePolicy, maxWords)
		if err != nil {
			return err
		}
	}
	s.audit(actor, "save_draft_config", "")
	return nil
}

// PublishDraft archives the current published row and promotes the draft. The
// partial unique index guarantees at most one published row at any moment.
func (s *Store) PublishDraft(actor string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var draftID int64
	if err := tx.QueryRow(`SELECT id FROM assistant_config WHERE status='draft' ORDER BY version DESC LIMIT 1`).
		Scan(&draftID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("no draft to publish")
		}
		return err
	}
	if _, err := tx.Exec(`UPDATE assistant_config SET status='archived' WHERE status='published'`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE assistant_config SET status='published', published_at=datetime('now') WHERE id=?`, draftID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.audit(actor, "publish", fmt.Sprintf("config id=%d", draftID))
	return nil
}

// RollbackTo re-publishes an earlier version by cloning it into a fresh draft
// and publishing that, preserving history (docs/03 open question: keep all).
func (s *Store) RollbackTo(version int, actor string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var c admin.ConfigView
	err = tx.QueryRow(`SELECT persona, mission, guardrails, language_policy, reply_max_words
		FROM assistant_config WHERE version=? LIMIT 1`, version).
		Scan(&c.Persona, &c.Mission, &c.Guardrails, &c.LanguagePolicy, &c.ReplyMaxWords)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("version %d not found", version)
	}
	if err != nil {
		return err
	}
	var maxV int
	_ = tx.QueryRow(`SELECT MAX(version) FROM assistant_config`).Scan(&maxV)
	if _, err := tx.Exec(`UPDATE assistant_config SET status='archived' WHERE status IN ('published','draft')`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO assistant_config
		(version, status, persona, mission, guardrails, language_policy, reply_max_words, published_at)
		VALUES (?, 'published', ?, ?, ?, ?, ?, datetime('now'))`,
		maxV+1, c.Persona, c.Mission, c.Guardrails, c.LanguagePolicy, c.ReplyMaxWords); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.audit(actor, "rollback", fmt.Sprintf("to version=%d", version))
	return nil
}

// --- knowledge base ---

func (s *Store) Topics() ([]admin.TopicRow, error) {
	rows, err := s.db.Query(`SELECT id, slug, language, title, summary, body_md, keywords, active
		FROM kb_topics ORDER BY slug, language`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.TopicRow
	for rows.Next() {
		var t admin.TopicRow
		var active int
		if err := rows.Scan(&t.ID, &t.Slug, &t.Language, &t.Title, &t.Summary, &t.BodyMD, &t.Keywords, &active); err != nil {
			return nil, err
		}
		t.Active = active == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SaveTopic(t admin.TopicRow, actor string) error {
	var err error
	if t.ID > 0 {
		_, err = s.db.Exec(`UPDATE kb_topics SET slug=?, language=?, title=?, summary=?, body_md=?, keywords=?, active=?,
			updated_at=datetime('now') WHERE id=?`,
			t.Slug, t.Language, t.Title, t.Summary, t.BodyMD, t.Keywords, boolToInt(t.Active), t.ID)
	} else {
		_, err = s.db.Exec(`INSERT INTO kb_topics (slug, language, title, summary, body_md, keywords, active)
			VALUES (?,?,?,?,?,?,?)
			ON CONFLICT(slug, language) DO UPDATE SET
				title=excluded.title, summary=excluded.summary, body_md=excluded.body_md,
				keywords=excluded.keywords, active=excluded.active, updated_at=datetime('now')`,
			t.Slug, t.Language, t.Title, t.Summary, t.BodyMD, t.Keywords, boolToInt(t.Active))
	}
	if err != nil {
		return err
	}
	s.audit(actor, "save_topic", t.Slug+"/"+t.Language)
	return nil
}

func (s *Store) DeleteTopic(id int64, actor string) error {
	if _, err := s.db.Exec(`DELETE FROM kb_topics WHERE id=?`, id); err != nil {
		return err
	}
	s.audit(actor, "delete_topic", fmt.Sprintf("id=%d", id))
	return nil
}

// --- media catalog ---

func (s *Store) Assets() ([]admin.AssetRow, error) {
	rows, err := s.db.Query(`SELECT id, ref, topic_slug, kind, url, title, description, language, active
		FROM kb_assets ORDER BY ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.AssetRow
	for rows.Next() {
		var a admin.AssetRow
		var active int
		if err := rows.Scan(&a.ID, &a.Ref, &a.TopicSlug, &a.Kind, &a.URL, &a.Title, &a.Description, &a.Language, &active); err != nil {
			return nil, err
		}
		a.Active = active == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SaveAsset(a admin.AssetRow, actor string) error {
	var err error
	if a.ID > 0 {
		_, err = s.db.Exec(`UPDATE kb_assets SET ref=?, topic_slug=?, kind=?, url=?, title=?, description=?,
			language=?, active=?, updated_at=datetime('now') WHERE id=?`,
			a.Ref, a.TopicSlug, a.Kind, a.URL, a.Title, a.Description, a.Language, boolToInt(a.Active), a.ID)
	} else {
		_, err = s.db.Exec(`INSERT INTO kb_assets (ref, topic_slug, kind, url, title, description, language, active)
			VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT(ref) DO UPDATE SET
				topic_slug=excluded.topic_slug, kind=excluded.kind, url=excluded.url, title=excluded.title,
				description=excluded.description, language=excluded.language, active=excluded.active,
				updated_at=datetime('now')`,
			a.Ref, a.TopicSlug, a.Kind, a.URL, a.Title, a.Description, a.Language, boolToInt(a.Active))
	}
	if err != nil {
		return err
	}
	s.audit(actor, "save_asset", a.Ref)
	return nil
}

func (s *Store) DeleteAsset(id int64, actor string) error {
	if _, err := s.db.Exec(`DELETE FROM kb_assets WHERE id=?`, id); err != nil {
		return err
	}
	s.audit(actor, "delete_asset", fmt.Sprintf("id=%d", id))
	return nil
}

// --- prices ---

func (s *Store) Tariffs() ([]domain.Tariff, error) {
	rows, err := s.db.Query(`SELECT key, price_tenge, cashier_limit FROM tariffs ORDER BY price_tenge`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Tariff
	for rows.Next() {
		var t domain.Tariff
		if err := rows.Scan(&t.Key, &t.PriceTenge, &t.CashierLimit); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SaveTariff(t domain.Tariff, actor string) error {
	_, err := s.db.Exec(`INSERT INTO tariffs (key, price_tenge, cashier_limit) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET price_tenge=excluded.price_tenge,
			cashier_limit=excluded.cashier_limit, updated_at=datetime('now')`,
		t.Key, t.PriceTenge, t.CashierLimit)
	if err != nil {
		return err
	}
	s.audit(actor, "save_tariff", t.Key)
	return nil
}

func (s *Store) DeleteTariff(key, actor string) error {
	if _, err := s.db.Exec(`DELETE FROM tariffs WHERE key=?`, key); err != nil {
		return err
	}
	s.audit(actor, "delete_tariff", key)
	return nil
}

func (s *Store) Placeholders() ([]domain.Placeholder, error) {
	rows, err := s.db.Query(`SELECT token, value_ru, value_kk FROM placeholders ORDER BY token`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Placeholder
	for rows.Next() {
		var p domain.Placeholder
		if err := rows.Scan(&p.Token, &p.ValueRU, &p.ValueKK); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SavePlaceholder(p domain.Placeholder, actor string) error {
	_, err := s.db.Exec(`INSERT INTO placeholders (token, value_ru, value_kk) VALUES (?,?,?)
		ON CONFLICT(token) DO UPDATE SET value_ru=excluded.value_ru, value_kk=excluded.value_kk`,
		p.Token, p.ValueRU, p.ValueKK)
	if err != nil {
		return err
	}
	s.audit(actor, "save_placeholder", p.Token)
	return nil
}

func (s *Store) DeletePlaceholder(token, actor string) error {
	if _, err := s.db.Exec(`DELETE FROM placeholders WHERE token=?`, token); err != nil {
		return err
	}
	s.audit(actor, "delete_placeholder", token)
	return nil
}

// --- audit ---

func (s *Store) Audit(limit int) ([]admin.AuditRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT at, COALESCE(actor,''), COALESCE(action,''), COALESCE(detail,'')
		FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []admin.AuditRow
	for rows.Next() {
		var a admin.AuditRow
		if err := rows.Scan(&a.At, &a.Actor, &a.Action, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- webhook idempotency (docs/06) ---

// MarkProcessed records a Chatwoot message id; ok is false if already present,
// giving real idempotency across restarts.
func (s *Store) MarkProcessed(messageID string) (ok bool, err error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO processed_messages (chatwoot_message_id) VALUES (?)`, messageID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
