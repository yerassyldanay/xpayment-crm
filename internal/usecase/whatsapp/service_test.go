package whatsapp

import (
	"context"
	"testing"
)

func TestAttachConfiguresBridgeAndEnablesAI(t *testing.T) {
	ctx := context.Background()
	evo := &fakeEvolution{
		instances: []Instance{{Name: "xpayment", ConnectionState: "open", OwnerJID: "7701@s.whatsapp.net"}},
	}
	cw := &fakeChatwoot{inboxes: []Inbox{{ID: 42, Name: "xpayment", ChannelType: "Channel::Api"}}}
	store := &fakeStore{rows: map[string]ManagedInstance{}}
	svc := NewService(evo, cw, store, testConfig())

	if err := svc.Attach(ctx, "xpayment", "tester"); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	if !evo.chatwoot["xpayment"].Enabled || evo.chatwoot["xpayment"].NameInbox != "xpayment" {
		t.Fatalf("chatwoot config not set correctly: %+v", evo.chatwoot["xpayment"])
	}
	if evo.chatwoot["xpayment"].Token != "chatwoot-token" {
		t.Fatal("chatwoot token should be supplied on /chatwoot/set")
	}
	if !evo.webhooks["xpayment"].Enabled || evo.webhooks["xpayment"].URL != "http://evolution-webhook:9701/evolution" {
		t.Fatalf("event webhook not set correctly: %+v", evo.webhooks["xpayment"])
	}
	if cw.inboxWebhooks[42] != "http://localhost:9700/chatwoot/webhook/xpayment" {
		t.Fatalf("inbox webhook = %q", cw.inboxWebhooks[42])
	}
	if !cw.accountWebhookCreated {
		t.Fatal("account webhook should be ensured")
	}
	row := store.rows["xpayment"]
	if !row.AIEnabled || row.InboxID != 42 {
		t.Fatalf("managed row not AI-enabled: %+v", row)
	}
}

func TestDetachDisablesBridgeWithoutRemovingSession(t *testing.T) {
	ctx := context.Background()
	evo := &fakeEvolution{
		instances: []Instance{{Name: "xpayment", ConnectionState: "open"}},
		chatwoot:  map[string]ChatwootConfig{"xpayment": {Enabled: true, NameInbox: "xpayment"}},
	}
	cw := &fakeChatwoot{inboxes: []Inbox{{ID: 42, Name: "xpayment"}}, inboxWebhooks: map[int64]string{42: "old"}}
	store := &fakeStore{rows: map[string]ManagedInstance{"xpayment": {InstanceName: "xpayment", InboxID: 42, AIEnabled: true}}}
	svc := NewService(evo, cw, store, testConfig())

	if err := svc.Detach(ctx, "xpayment", "tester"); err != nil {
		t.Fatalf("Detach() error = %v", err)
	}
	if evo.chatwoot["xpayment"].Enabled {
		t.Fatal("chatwoot bridge should be disabled")
	}
	if cw.inboxWebhooks[42] != "" {
		t.Fatalf("inbox webhook should be cleared, got %q", cw.inboxWebhooks[42])
	}
	if store.rows["xpayment"].AIEnabled {
		t.Fatal("AI should be disabled for detached instance")
	}
	if len(evo.deleted) > 0 || len(evo.loggedOut) > 0 {
		t.Fatal("detach must not logout or delete WhatsApp sessions")
	}
}

func TestInstancesAuditsWithoutMutating(t *testing.T) {
	ctx := context.Background()
	evo := &fakeEvolution{instances: []Instance{{Name: "xpayment", ConnectionState: "open"}}}
	cw := &fakeChatwoot{inboxes: []Inbox{{ID: 42, Name: "xpayment", WebhookURL: "bad"}}}
	store := &fakeStore{rows: map[string]ManagedInstance{}}
	svc := NewService(evo, cw, store, testConfig())

	views, err := svc.Instances(ctx)
	if err != nil {
		t.Fatalf("Instances() error = %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("views len = %d", len(views))
	}
	if evo.setChatwootCalls != 0 || evo.setWebhookCalls != 0 || cw.updateInboxCalls != 0 {
		t.Fatal("audit/list must not mutate external services")
	}
}

func testConfig() Config {
	return Config{
		ChatwootAccountID:              "2",
		ChatwootToken:                  "chatwoot-token",
		EvolutionChatwootURL:           "http://host.docker.internal:3000",
		EvolutionOrganization:          "xpayment",
		EvolutionEventWebhookURL:       "http://evolution-webhook:9701/evolution",
		ChatwootToEvolutionWebhookBase: "http://localhost:9700/chatwoot/webhook",
		BrainWebhookURL:                "http://localhost:8080/v1/assistant/webhook/chatwoot?secret=s",
	}
}

type fakeEvolution struct {
	instances        []Instance
	chatwoot         map[string]ChatwootConfig
	webhooks         map[string]EventWebhook
	deleted          []string
	loggedOut        []string
	setChatwootCalls int
	setWebhookCalls  int
}

func (f *fakeEvolution) FetchInstances(context.Context) ([]Instance, error) { return f.instances, nil }
func (f *fakeEvolution) ConnectionState(_ context.Context, instance string) (string, error) {
	for _, inst := range f.instances {
		if inst.Name == instance {
			return inst.ConnectionState, nil
		}
	}
	return "", nil
}
func (f *fakeEvolution) FindChatwoot(_ context.Context, instance string) (*ChatwootConfig, error) {
	if f.chatwoot == nil {
		return nil, nil
	}
	cfg, ok := f.chatwoot[instance]
	if !ok {
		return nil, nil
	}
	return &cfg, nil
}
func (f *fakeEvolution) SetChatwoot(_ context.Context, instance string, cfg ChatwootConfig) error {
	if f.chatwoot == nil {
		f.chatwoot = map[string]ChatwootConfig{}
	}
	f.setChatwootCalls++
	f.chatwoot[instance] = cfg
	return nil
}
func (f *fakeEvolution) FindWebhook(_ context.Context, instance string) (*EventWebhook, error) {
	if f.webhooks == nil {
		return nil, nil
	}
	wh, ok := f.webhooks[instance]
	if !ok {
		return nil, nil
	}
	return &wh, nil
}
func (f *fakeEvolution) SetWebhook(_ context.Context, instance string, wh EventWebhook) error {
	if f.webhooks == nil {
		f.webhooks = map[string]EventWebhook{}
	}
	f.setWebhookCalls++
	f.webhooks[instance] = wh
	return nil
}

type fakeChatwoot struct {
	inboxes               []Inbox
	inboxWebhooks         map[int64]string
	accountWebhookCreated bool
	updateInboxCalls      int
}

func (f *fakeChatwoot) ListInboxes(context.Context) ([]Inbox, error) { return f.inboxes, nil }
func (f *fakeChatwoot) UpdateInboxWebhook(_ context.Context, inboxID int64, webhookURL string) error {
	if f.inboxWebhooks == nil {
		f.inboxWebhooks = map[int64]string{}
	}
	f.updateInboxCalls++
	f.inboxWebhooks[inboxID] = webhookURL
	return nil
}
func (f *fakeChatwoot) ListAccountWebhooks(context.Context) ([]AccountWebhook, error) {
	return nil, nil
}
func (f *fakeChatwoot) CreateAccountWebhook(_ context.Context, _ string, _ []string) error {
	f.accountWebhookCreated = true
	return nil
}

type fakeStore struct {
	rows map[string]ManagedInstance
}

func (f *fakeStore) ManagedWhatsAppInstances() ([]ManagedInstance, error) {
	out := make([]ManagedInstance, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, row)
	}
	return out, nil
}
func (f *fakeStore) ManagedWhatsAppInstance(instance string) (*ManagedInstance, error) {
	row, ok := f.rows[instance]
	if !ok {
		return nil, nil
	}
	return &row, nil
}
func (f *fakeStore) UpsertManagedWhatsAppInstance(row ManagedInstance, _ string) error {
	if f.rows == nil {
		f.rows = map[string]ManagedInstance{}
	}
	f.rows[row.InstanceName] = row
	return nil
}
func (f *fakeStore) SetManagedWhatsAppDetached(instance string, _ string) error {
	row := f.rows[instance]
	row.ChatwootEnabled = false
	row.BridgeEnabled = false
	row.AIEnabled = false
	row.LastAuditStatus = "detached"
	f.rows[instance] = row
	return nil
}
func (f *fakeStore) AIEnabledInboxIDs() ([]int64, error) {
	var out []int64
	for _, row := range f.rows {
		if row.AIEnabled && row.InboxID > 0 {
			out = append(out, row.InboxID)
		}
	}
	return out, nil
}
