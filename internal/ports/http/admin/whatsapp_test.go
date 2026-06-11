package adminui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	whatsappuc "github.com/yessaliyev/xpayment-crm/internal/usecase/whatsapp"
)

func TestWhatsAppPageAndAttachRoute(t *testing.T) {
	evo := &adminFakeEvolution{instances: []whatsappuc.Instance{{Name: "xpayment", ConnectionState: "open"}}}
	cw := &adminFakeChatwoot{inboxes: []whatsappuc.Inbox{{ID: 7, Name: "xpayment"}}}
	store := &adminFakeStore{rows: map[string]whatsappuc.ManagedInstance{}}
	svc := whatsappuc.NewService(evo, cw, store, whatsappuc.Config{
		ChatwootAccountID:              "2",
		ChatwootToken:                  "tok",
		EvolutionChatwootURL:           "http://host.docker.internal:3000",
		EvolutionOrganization:          "xpayment",
		EvolutionEventWebhookURL:       "http://evolution-webhook:9701/evolution",
		ChatwootToEvolutionWebhookBase: "http://localhost:9700/chatwoot/webhook",
		BrainWebhookURL:                "http://localhost:8080/v1/assistant/webhook/chatwoot?secret=s",
	})
	h, err := New(nil, svc, nil, "admin", "secret", "session", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	routes := h.Routes()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/whatsapp", nil)
	h.auth.issue(rec)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	page := httptest.NewRecorder()
	routes.ServeHTTP(page, req)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "xpayment") {
		t.Fatalf("whatsapp page code=%d body=%q", page.Code, page.Body.String())
	}

	form := url.Values{"instance": {"xpayment"}}
	post := httptest.NewRequest(http.MethodPost, "/whatsapp/attach", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range rec.Result().Cookies() {
		post.AddCookie(c)
	}
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, post)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("attach code=%d", resp.Code)
	}
	if !store.rows["xpayment"].AIEnabled {
		t.Fatal("attach route should enable AI for the instance")
	}
}

type adminFakeEvolution struct {
	instances []whatsappuc.Instance
	chatwoot  map[string]whatsappuc.ChatwootConfig
	webhooks  map[string]whatsappuc.EventWebhook
}

func (f *adminFakeEvolution) FetchInstances(context.Context) ([]whatsappuc.Instance, error) {
	return f.instances, nil
}
func (f *adminFakeEvolution) ConnectionState(_ context.Context, instance string) (string, error) {
	for _, inst := range f.instances {
		if inst.Name == instance {
			return inst.ConnectionState, nil
		}
	}
	return "", nil
}
func (f *adminFakeEvolution) FindChatwoot(_ context.Context, instance string) (*whatsappuc.ChatwootConfig, error) {
	cfg, ok := f.chatwoot[instance]
	if !ok {
		return nil, nil
	}
	return &cfg, nil
}
func (f *adminFakeEvolution) SetChatwoot(_ context.Context, instance string, cfg whatsappuc.ChatwootConfig) error {
	if f.chatwoot == nil {
		f.chatwoot = map[string]whatsappuc.ChatwootConfig{}
	}
	f.chatwoot[instance] = cfg
	return nil
}
func (f *adminFakeEvolution) FindWebhook(_ context.Context, instance string) (*whatsappuc.EventWebhook, error) {
	wh, ok := f.webhooks[instance]
	if !ok {
		return nil, nil
	}
	return &wh, nil
}
func (f *adminFakeEvolution) SetWebhook(_ context.Context, instance string, wh whatsappuc.EventWebhook) error {
	if f.webhooks == nil {
		f.webhooks = map[string]whatsappuc.EventWebhook{}
	}
	f.webhooks[instance] = wh
	return nil
}

type adminFakeChatwoot struct {
	inboxes []whatsappuc.Inbox
}

func (f *adminFakeChatwoot) ListInboxes(context.Context) ([]whatsappuc.Inbox, error) {
	return f.inboxes, nil
}
func (f *adminFakeChatwoot) UpdateInboxWebhook(context.Context, int64, string) error { return nil }
func (f *adminFakeChatwoot) ListAccountWebhooks(context.Context) ([]whatsappuc.AccountWebhook, error) {
	return nil, nil
}
func (f *adminFakeChatwoot) CreateAccountWebhook(context.Context, string, []string) error { return nil }

type adminFakeStore struct {
	rows map[string]whatsappuc.ManagedInstance
}

func (f *adminFakeStore) ManagedWhatsAppInstances() ([]whatsappuc.ManagedInstance, error) {
	out := make([]whatsappuc.ManagedInstance, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, row)
	}
	return out, nil
}
func (f *adminFakeStore) ManagedWhatsAppInstance(instance string) (*whatsappuc.ManagedInstance, error) {
	row, ok := f.rows[instance]
	if !ok {
		return nil, nil
	}
	return &row, nil
}
func (f *adminFakeStore) UpsertManagedWhatsAppInstance(row whatsappuc.ManagedInstance, _ string) error {
	f.rows[row.InstanceName] = row
	return nil
}
func (f *adminFakeStore) SetManagedWhatsAppDetached(instance string, _ string) error {
	row := f.rows[instance]
	row.AIEnabled = false
	f.rows[instance] = row
	return nil
}
func (f *adminFakeStore) AIEnabledInboxIDs() ([]int64, error) { return nil, nil }
