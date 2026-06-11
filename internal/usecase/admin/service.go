package admin

import (
	"context"
	"fmt"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
	"github.com/yessaliyev/xpayment-crm/internal/usecase/assistant"
)

// Service is the admin UI's service layer. Publish validates the draft snapshot,
// promotes it, and hot-reloads the live pointer in-process (docs/03, docs/04).
type Service struct {
	store   Store
	content *domain.Content   // the live snapshot pointer (ContentSource)
	drafter assistant.Drafter // for the Playground dry-run
}

// NewService wires the admin service. content is the same atomic pointer the
// assistant reads; drafter is used only by the Playground.
func NewService(store Store, content *domain.Content, drafter assistant.Drafter) *Service {
	return &Service{store: store, content: content, drafter: drafter}
}

// --- config lifecycle ---

func (s *Service) DraftConfig() (*ConfigView, error)     { return s.store.DraftConfig() }
func (s *Service) PublishedConfig() (*ConfigView, error) { return s.store.PublishedConfig() }
func (s *Service) ConfigVersions() ([]ConfigView, error) { return s.store.ConfigVersions() }
func (s *Service) SaveDraftConfig(in ConfigInput, actor string) error {
	return s.store.SaveDraftConfig(in, actor)
}

// Publish validates the draft snapshot, then promotes + hot-reloads. If
// validation fails, nothing is promoted and the live snapshot is unchanged.
func (s *Service) Publish(actor string) (warnings []string, err error) {
	draft, err := s.store.LoadDraftSnapshot()
	if err != nil {
		return nil, fmt.Errorf("load draft: %w", err)
	}
	warnings, err = domain.ValidateSnapshot(draft)
	if err != nil {
		return warnings, fmt.Errorf("validation failed: %w", err)
	}
	if err := s.store.PublishDraft(actor); err != nil {
		return warnings, err
	}
	return warnings, s.reload()
}

// Rollback re-publishes an earlier version and hot-reloads.
func (s *Service) Rollback(version int, actor string) error {
	if err := s.store.RollbackTo(version, actor); err != nil {
		return err
	}
	return s.reload()
}

// reload loads the published snapshot, validates it, and atomically swaps the
// live pointer (keeps the old snapshot on failure).
func (s *Service) reload() error {
	snap, err := s.store.LoadSnapshot()
	if err != nil {
		return fmt.Errorf("reload snapshot: %w", err)
	}
	if _, err := domain.ValidateSnapshot(snap); err != nil {
		return fmt.Errorf("published snapshot invalid: %w", err)
	}
	s.content.Set(snap)
	return nil
}

// --- KB / media / prices passthrough ---

func (s *Service) Topics() ([]TopicRow, error)              { return s.store.Topics() }
func (s *Service) SaveTopic(t TopicRow, actor string) error { return s.store.SaveTopic(t, actor) }
func (s *Service) DeleteTopic(id int64, actor string) error { return s.store.DeleteTopic(id, actor) }
func (s *Service) Assets() ([]AssetRow, error)              { return s.store.Assets() }
func (s *Service) SaveAsset(a AssetRow, actor string) error { return s.store.SaveAsset(a, actor) }
func (s *Service) DeleteAsset(id int64, actor string) error { return s.store.DeleteAsset(id, actor) }
func (s *Service) Tariffs() ([]domain.Tariff, error)        { return s.store.Tariffs() }
func (s *Service) SaveTariff(t domain.Tariff, actor string) error {
	return s.store.SaveTariff(t, actor)
}
func (s *Service) DeleteTariff(key, actor string) error        { return s.store.DeleteTariff(key, actor) }
func (s *Service) Placeholders() ([]domain.Placeholder, error) { return s.store.Placeholders() }
func (s *Service) SavePlaceholder(p domain.Placeholder, actor string) error {
	return s.store.SavePlaceholder(p, actor)
}
func (s *Service) DeletePlaceholder(token, actor string) error {
	return s.store.DeletePlaceholder(token, actor)
}
func (s *Service) Audit(limit int) ([]AuditRow, error) { return s.store.Audit(limit) }

// --- Playground (test before you publish, docs/08) ---

// PlaygroundResult mirrors a post-processed Draft plus debug fields (docs/06).
type PlaygroundResult struct {
	Draft domain.Draft
	Debug PlaygroundDebug
}

type PlaygroundDebug struct {
	MatchedTopics []string
	DroppedRefs   []string
}

// Playground runs HandleMessage against the DRAFT snapshot — a dry-run; nothing
// is sent. It uses the real Drafter so authors see the actual model behavior.
func (s *Service) Playground(ctx context.Context, profile map[string]any, window []domain.Message, msg string) (PlaygroundResult, error) {
	draft, err := s.store.LoadDraftSnapshot()
	if err != nil {
		return PlaygroundResult{}, err
	}
	content := &domain.Content{}
	content.Set(draft)
	reader := fixedReader{window: window, profile: profile}
	brain := assistant.New(content, reader, s.drafter, nil)
	d, err := brain.HandleMessage(ctx, domain.ChatID{}, domain.Message{Role: domain.RoleCustomer, Content: msg})
	if err != nil {
		return PlaygroundResult{}, err
	}
	return PlaygroundResult{Draft: d, Debug: PlaygroundDebug{DroppedRefs: d.DroppedRefs}}, nil
}

// fixedReader feeds the Playground a fixed window/profile (no Chatwoot call).
type fixedReader struct {
	window  []domain.Message
	profile map[string]any
}

func (r fixedReader) Window(context.Context, domain.ChatID) ([]domain.Message, error) {
	return r.window, nil
}
func (r fixedReader) Profile(context.Context, domain.ChatID) (map[string]any, error) {
	return r.profile, nil
}
