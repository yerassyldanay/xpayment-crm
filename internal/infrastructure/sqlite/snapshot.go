package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
)

// ErrNoPublished is returned when no published config row exists yet.
var ErrNoPublished = errors.New("sqlite: no published assistant_config")

// LoadSnapshot reads the PUBLISHED config + active topics/assets/prices into an
// immutable snapshot (docs/03 · the in-memory snapshot). It does NOT validate;
// callers validate before swapping the live pointer.
func (s *Store) LoadSnapshot() (*domain.Snapshot, error) {
	cfg, err := s.publishedConfig()
	if err != nil {
		return nil, err
	}
	prices, err := s.priceBook()
	if err != nil {
		return nil, err
	}
	topics, err := s.activeTopics()
	if err != nil {
		return nil, err
	}
	assets, err := s.activeAssets()
	if err != nil {
		return nil, err
	}
	return &domain.Snapshot{
		Config: cfg,
		Prices: prices,
		Topics: topics,
		Assets: assets,
		Loaded: time.Now(),
	}, nil
}

// LoadDraftSnapshot is LoadSnapshot but using the DRAFT config (the Playground
// and publish-validation read this). It falls back to the published config if no
// draft exists, so the Playground still works before the first edit.
func (s *Store) LoadDraftSnapshot() (*domain.Snapshot, error) {
	cfg, err := s.configByStatus("draft")
	if errors.Is(err, ErrNoPublished) {
		cfg, err = s.configByStatus("published")
	}
	if err != nil {
		return nil, err
	}
	prices, err := s.priceBook()
	if err != nil {
		return nil, err
	}
	topics, err := s.activeTopics()
	if err != nil {
		return nil, err
	}
	assets, err := s.activeAssets()
	if err != nil {
		return nil, err
	}
	return &domain.Snapshot{Config: cfg, Prices: prices, Topics: topics, Assets: assets, Loaded: time.Now()}, nil
}

func (s *Store) publishedConfig() (domain.AssistantConfig, error) {
	return s.configByStatus("published")
}

func (s *Store) configByStatus(status string) (domain.AssistantConfig, error) {
	row := s.db.QueryRow(`SELECT version, persona, mission, guardrails, language_policy, reply_max_words
		FROM assistant_config WHERE status=? ORDER BY version DESC LIMIT 1`, status)
	var c domain.AssistantConfig
	err := row.Scan(&c.Version, &c.Persona, &c.Mission, &c.Guardrails, &c.LanguagePolicy, &c.ReplyMaxWords)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AssistantConfig{}, ErrNoPublished
	}
	if err != nil {
		return domain.AssistantConfig{}, fmt.Errorf("%s config: %w", status, err)
	}
	return c, nil
}

func (s *Store) priceBook() (domain.PriceBook, error) {
	pb := domain.PriceBook{
		Tariffs:      map[string]domain.Tariff{},
		Placeholders: map[string]domain.Placeholder{},
	}
	rows, err := s.db.Query(`SELECT key, price_tenge, cashier_limit FROM tariffs`)
	if err != nil {
		return pb, fmt.Errorf("tariffs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t domain.Tariff
		if err := rows.Scan(&t.Key, &t.PriceTenge, &t.CashierLimit); err != nil {
			return pb, err
		}
		pb.Tariffs[t.Key] = t
	}
	prows, err := s.db.Query(`SELECT token, value_ru, value_kk FROM placeholders`)
	if err != nil {
		return pb, fmt.Errorf("placeholders: %w", err)
	}
	defer prows.Close()
	for prows.Next() {
		var p domain.Placeholder
		if err := prows.Scan(&p.Token, &p.ValueRU, &p.ValueKK); err != nil {
			return pb, err
		}
		pb.Placeholders[p.Token] = p
	}
	return pb, nil
}

func (s *Store) activeTopics() ([]domain.Topic, error) {
	rows, err := s.db.Query(`SELECT slug, language, title, summary, body_md, keywords
		FROM kb_topics WHERE active=1 ORDER BY slug, language`)
	if err != nil {
		return nil, fmt.Errorf("topics: %w", err)
	}
	defer rows.Close()
	var out []domain.Topic
	for rows.Next() {
		var t domain.Topic
		if err := rows.Scan(&t.Slug, &t.Language, &t.Title, &t.Summary, &t.BodyMD, &t.Keywords); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) activeAssets() ([]domain.Asset, error) {
	rows, err := s.db.Query(`SELECT ref, topic_slug, kind, url, title, description, language
		FROM kb_assets WHERE active=1 ORDER BY ref`)
	if err != nil {
		return nil, fmt.Errorf("assets: %w", err)
	}
	defer rows.Close()
	var out []domain.Asset
	for rows.Next() {
		var a domain.Asset
		if err := rows.Scan(&a.Ref, &a.TopicSlug, &a.Kind, &a.URL, &a.Title, &a.Description, &a.Language); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
