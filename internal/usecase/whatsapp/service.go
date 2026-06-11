package whatsapp

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

var eventWebhookEvents = []string{"MESSAGES_UPDATE", "SEND_MESSAGE", "MESSAGES_UPSERT"}

type Service struct {
	evolution EvolutionClient
	chatwoot  ChatwootClient
	store     Store
	cfg       atomic.Pointer[Config] // hot-swappable so the Settings page can reconfigure live
}

func NewService(e EvolutionClient, c ChatwootClient, s Store, cfg Config) *Service {
	svc := &Service{evolution: e, chatwoot: c, store: s}
	svc.cfg.Store(&cfg)
	return svc
}

// config returns the current connection config snapshot.
func (s *Service) config() Config { return *s.cfg.Load() }

// SetConfig swaps the connection config in place (used by the Settings page).
func (s *Service) SetConfig(cfg Config) { s.cfg.Store(&cfg) }

func (s *Service) Instances(ctx context.Context) ([]InstanceView, error) {
	cfg := s.config()
	instances, err := s.evolution.FetchInstances(ctx)
	if err != nil {
		return nil, err
	}
	inboxes, _ := s.chatwoot.ListInboxes(ctx)
	accountHooks, _ := s.chatwoot.ListAccountWebhooks(ctx)
	managed, _ := s.store.ManagedWhatsAppInstances()
	managedByName := map[string]ManagedInstance{}
	for _, row := range managed {
		managedByName[row.InstanceName] = row
	}

	out := make([]InstanceView, 0, len(instances))
	for _, inst := range instances {
		view := InstanceView{
			Instance:             inst,
			ExpectedInboxWebhook: s.expectedInboxWebhook(inst.Name),
			ExpectedEventWebhook: cfg.EvolutionEventWebhookURL,
			AccountWebhookOK:     hasAccountWebhook(accountHooks, cfg.BrainWebhookURL),
		}
		if row, ok := managedByName[inst.Name]; ok {
			rowCopy := row
			view.Managed = &rowCopy
			view.AIEnabled = row.AIEnabled
			view.LastCheckedAt = row.LastCheckedAt
		}
		if state, err := s.evolution.ConnectionState(ctx, inst.Name); err == nil && state != "" {
			view.Instance.ConnectionState = state
		}
		if cw, err := s.evolution.FindChatwoot(ctx, inst.Name); err == nil && cw != nil {
			view.Chatwoot = cw
			if in := findInbox(inboxes, cw.NameInbox); in != nil {
				view.Inbox = in
			}
		}
		if wh, err := s.evolution.FindWebhook(ctx, inst.Name); err == nil && wh != nil {
			view.EventWebhook = wh
		}
		view.evaluate()
		out = append(out, view)
	}
	return out, nil
}

func (s *Service) Attach(ctx context.Context, instance, actor string) error {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return fmt.Errorf("instance is required")
	}
	cfg := s.config()
	inst, err := s.findInstance(ctx, instance)
	if err != nil {
		return err
	}

	cwCfg := ChatwootConfig{
		Enabled:                 true,
		AccountID:               cfg.ChatwootAccountID,
		Token:                   cfg.ChatwootToken,
		URL:                     cfg.EvolutionChatwootURL,
		NameInbox:               instance,
		SignMsg:                 true,
		ReopenConversation:      true,
		ConversationPending:     false,
		MergeBrazilContacts:     false,
		ImportContacts:          true,
		ImportMessages:          false,
		DaysLimitImportMessages: 0,
		Organization:            cfg.EvolutionOrganization,
	}
	if err := s.evolution.SetChatwoot(ctx, instance, cwCfg); err != nil {
		return fmt.Errorf("set evolution chatwoot integration: %w", err)
	}
	if err := s.evolution.SetWebhook(ctx, instance, s.eventWebhook()); err != nil {
		return fmt.Errorf("set evolution event webhook: %w", err)
	}
	inbox, err := s.waitForInbox(ctx, instance)
	if err != nil {
		return err
	}
	if err := s.chatwoot.UpdateInboxWebhook(ctx, inbox.ID, s.expectedInboxWebhook(instance)); err != nil {
		return fmt.Errorf("set chatwoot inbox webhook: %w", err)
	}
	if cfg.BrainWebhookURL != "" {
		if err := s.ensureAccountWebhook(ctx); err != nil {
			return fmt.Errorf("ensure chatwoot account webhook: %w", err)
		}
	}
	return s.store.UpsertManagedWhatsAppInstance(ManagedInstance{
		InstanceName:    instance,
		InboxID:         inbox.ID,
		InboxName:       inbox.Name,
		OwnerJID:        inst.OwnerJID,
		ConnectionState: inst.ConnectionState,
		ChatwootEnabled: true,
		BridgeEnabled:   true,
		AIEnabled:       true,
		LastAuditStatus: "attached",
		LastAuditDetail: "chatwoot bridge and ai enabled",
	}, actor)
}

func (s *Service) Detach(ctx context.Context, instance, actor string) error {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return fmt.Errorf("instance is required")
	}
	cfg := s.config()
	existing, _ := s.evolution.FindChatwoot(ctx, instance)
	disable := ChatwootConfig{Enabled: false}
	if existing != nil {
		disable = *existing
		disable.Enabled = false
	}
	if disable.AccountID == "" {
		disable.AccountID = cfg.ChatwootAccountID
	}
	if disable.Token == "" {
		disable.Token = cfg.ChatwootToken
	}
	if disable.URL == "" {
		disable.URL = cfg.EvolutionChatwootURL
	}
	if disable.NameInbox == "" {
		disable.NameInbox = instance
	}
	if err := s.evolution.SetChatwoot(ctx, instance, disable); err != nil {
		return fmt.Errorf("disable evolution chatwoot integration: %w", err)
	}
	if row, err := s.store.ManagedWhatsAppInstance(instance); err == nil && row != nil && row.InboxID > 0 {
		if err := s.chatwoot.UpdateInboxWebhook(ctx, row.InboxID, ""); err != nil {
			return fmt.Errorf("clear chatwoot inbox webhook: %w", err)
		}
	}
	return s.store.SetManagedWhatsAppDetached(instance, actor)
}

func (s *Service) FixWebhooks(ctx context.Context, instance, actor string) error {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return fmt.Errorf("instance is required")
	}
	if err := s.evolution.SetWebhook(ctx, instance, s.eventWebhook()); err != nil {
		return fmt.Errorf("set evolution event webhook: %w", err)
	}
	row, err := s.store.ManagedWhatsAppInstance(instance)
	if err != nil {
		return err
	}
	if row == nil || row.InboxID == 0 {
		return fmt.Errorf("instance %q is not attached; attach it before fixing inbox webhooks", instance)
	}
	if err := s.chatwoot.UpdateInboxWebhook(ctx, row.InboxID, s.expectedInboxWebhook(instance)); err != nil {
		return fmt.Errorf("set chatwoot inbox webhook: %w", err)
	}
	if s.config().BrainWebhookURL != "" {
		if err := s.ensureAccountWebhook(ctx); err != nil {
			return fmt.Errorf("ensure chatwoot account webhook: %w", err)
		}
	}
	row.BridgeEnabled = true
	row.AIEnabled = true
	row.LastAuditStatus = "fixed"
	row.LastAuditDetail = "webhooks refreshed"
	return s.store.UpsertManagedWhatsAppInstance(*row, actor)
}

func (s *Service) findInstance(ctx context.Context, name string) (Instance, error) {
	instances, err := s.evolution.FetchInstances(ctx)
	if err != nil {
		return Instance{}, err
	}
	for _, inst := range instances {
		if inst.Name == name {
			return inst, nil
		}
	}
	return Instance{}, fmt.Errorf("evolution instance %q not found", name)
}

func (s *Service) waitForInbox(ctx context.Context, name string) (*Inbox, error) {
	for i := 0; i < 6; i++ {
		inboxes, err := s.chatwoot.ListInboxes(ctx)
		if err != nil {
			return nil, err
		}
		if in := findInbox(inboxes, name); in != nil {
			return in, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("chatwoot inbox %q was not created by Evolution", name)
}

func (s *Service) ensureAccountWebhook(ctx context.Context) error {
	brainWebhookURL := s.config().BrainWebhookURL
	hooks, err := s.chatwoot.ListAccountWebhooks(ctx)
	if err != nil {
		return err
	}
	if hasAccountWebhook(hooks, brainWebhookURL) {
		return nil
	}
	return s.chatwoot.CreateAccountWebhook(ctx, brainWebhookURL, []string{"message_created"})
}

func (s *Service) expectedInboxWebhook(instance string) string {
	base := strings.TrimRight(s.config().ChatwootToEvolutionWebhookBase, "/")
	if base == "" {
		return ""
	}
	return base + "/" + instance
}

func (s *Service) eventWebhook() EventWebhook {
	return EventWebhook{
		Enabled:  true,
		URL:      s.config().EvolutionEventWebhookURL,
		Events:   eventWebhookEvents,
		Base64:   false,
		ByEvents: true,
	}
}

func findInbox(inboxes []Inbox, name string) *Inbox {
	for _, in := range inboxes {
		if in.Name == name {
			copy := in
			return &copy
		}
	}
	return nil
}

func hasAccountWebhook(hooks []AccountWebhook, url string) bool {
	if url == "" {
		return false
	}
	for _, h := range hooks {
		if h.URL == url && slices.Contains(h.Subscriptions, "message_created") {
			return true
		}
	}
	return false
}

func (v *InstanceView) evaluate() {
	if v.Instance.ConnectionState != "open" && v.Instance.ConnectionState != "" {
		v.Warnings = append(v.Warnings, "WhatsApp session is not open")
	}
	if v.Chatwoot == nil || !v.Chatwoot.Enabled {
		v.Warnings = append(v.Warnings, "Evolution Chatwoot bridge is disabled")
	}
	if v.Inbox == nil {
		v.Warnings = append(v.Warnings, "Chatwoot inbox is missing")
	} else if v.Inbox.WebhookURL != v.ExpectedInboxWebhook {
		v.Warnings = append(v.Warnings, "Chatwoot inbox webhook does not match this instance")
	}
	if v.EventWebhook == nil || !v.EventWebhook.Enabled {
		v.Warnings = append(v.Warnings, "Evolution event webhook is disabled")
	} else if v.EventWebhook.URL != v.ExpectedEventWebhook {
		v.Warnings = append(v.Warnings, "Evolution event webhook URL is not expected")
	}
	if !v.AccountWebhookOK {
		v.Warnings = append(v.Warnings, "Chatwoot account webhook to brain is missing")
	}
	if strings.Contains(v.Instance.OwnerJID, "@lid") {
		v.Warnings = append(v.Warnings, "WhatsApp owner uses @lid; outbound can fail until contacts sync")
	}
	v.BridgeOK = v.Chatwoot != nil && v.Chatwoot.Enabled && v.Inbox != nil && v.Inbox.WebhookURL == v.ExpectedInboxWebhook
	v.EventWebhookOK = v.EventWebhook != nil && v.EventWebhook.Enabled && v.EventWebhook.URL == v.ExpectedEventWebhook
	switch {
	case v.BridgeOK && v.EventWebhookOK && v.AccountWebhookOK && v.AIEnabled:
		v.Status = "ready"
	case v.Managed != nil && !v.AIEnabled:
		v.Status = "detached"
	default:
		v.Status = "needs_attention"
	}
}
