// Package admin is the service layer behind the built-in admin UI (docs/08):
// config/KB/price/media CRUD and the draft → publish → rollback lifecycle.
package admin

import "github.com/yessaliyev/xpayment-crm/internal/domain"

// ConfigView is one assistant_config row as the UI sees it.
type ConfigView struct {
	ID             int64
	Version        int
	Status         string // draft | published | archived
	Persona        string
	Mission        string
	Guardrails     string
	LanguagePolicy string
	ReplyMaxWords  int
	CreatedAt      string
	PublishedAt    string
}

// ConfigInput is the editable subset of a draft config.
type ConfigInput struct {
	Persona        string
	Mission        string
	Guardrails     string
	LanguagePolicy string
	ReplyMaxWords  int
}

// TopicRow is one kb_topics row.
type TopicRow struct {
	ID       int64
	Slug     string
	Language string
	Title    string
	Summary  string
	BodyMD   string
	Active   bool
}

// AssetRow is one kb_assets row.
type AssetRow struct {
	ID          int64
	Ref         string
	TopicSlug   string
	Kind        string
	URL         string
	Title       string
	Description string
	Language    string
	Active      bool
}

// AuditRow is one audit_log entry.
type AuditRow struct {
	At     string
	Actor  string
	Action string
	Detail string
}

// Store is the persistence port the admin service depends on (implemented by
// internal/infrastructure/sqlite).
type Store interface {
	// Config lifecycle
	DraftConfig() (*ConfigView, error)
	PublishedConfig() (*ConfigView, error)
	ConfigVersions() ([]ConfigView, error)
	SaveDraftConfig(in ConfigInput, actor string) error
	PublishDraft(actor string) error
	RollbackTo(version int, actor string) error

	// Knowledge base
	Topics() ([]TopicRow, error)
	SaveTopic(t TopicRow, actor string) error
	DeleteTopic(id int64, actor string) error

	// Media catalog
	Assets() ([]AssetRow, error)
	SaveAsset(a AssetRow, actor string) error
	DeleteAsset(id int64, actor string) error

	// Prices
	Tariffs() ([]domain.Tariff, error)
	SaveTariff(t domain.Tariff, actor string) error
	DeleteTariff(key, actor string) error
	Placeholders() ([]domain.Placeholder, error)
	SavePlaceholder(p domain.Placeholder, actor string) error
	DeletePlaceholder(token, actor string) error

	// Audit
	Audit(limit int) ([]AuditRow, error)

	// Snapshots
	LoadSnapshot() (*domain.Snapshot, error)      // published
	LoadDraftSnapshot() (*domain.Snapshot, error) // draft config + current KB/prices
}
