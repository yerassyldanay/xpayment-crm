package adminui

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	settingsuc "github.com/yessaliyev/xpayment-crm/internal/usecase/settings"
)

type fakeSettingsStore struct{ data map[string]string }

func (f *fakeSettingsStore) BridgeSettings() (map[string]string, error) { return f.data, nil }
func (f *fakeSettingsStore) SaveBridgeSettings(values map[string]string, _ string) error {
	for k, v := range values {
		f.data[k] = v
	}
	return nil
}

func TestSettingsPageMasksSecretsAndKeepsOnBlank(t *testing.T) {
	store := &fakeSettingsStore{data: map[string]string{
		"CHATWOOT_API_TOKEN": "supersecret",
		"CHATWOOT_BASE_URL":  "http://localhost:3000",
	}}
	svc := settingsuc.NewService(store, nil, settingsuc.Bridge{})
	h, err := New(nil, nil, svc, "admin", "secret", "session", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	routes := h.Routes()
	authRec := httptest.NewRecorder()
	h.auth.issue(authRec)
	cookies := authRec.Result().Cookies()

	// GET must not leak the secret, but should render the non-secret value.
	get := httptest.NewRequest(http.MethodGet, "/settings", nil)
	for _, c := range cookies {
		get.AddCookie(c)
	}
	gr := httptest.NewRecorder()
	routes.ServeHTTP(gr, get)
	if gr.Code != http.StatusOK {
		t.Fatalf("settings GET code=%d", gr.Code)
	}
	body := gr.Body.String()
	if strings.Contains(body, "supersecret") {
		t.Error("secret token leaked into settings page")
	}
	if !strings.Contains(body, "http://localhost:3000") {
		t.Error("non-secret value not rendered")
	}

	// POST with a blank token must keep the stored secret.
	form := url.Values{
		"chatwoot_base_url":   {"http://localhost:3000"},
		"chatwoot_api_token":  {""},
		"chatwoot_account_id": {"2"},
	}
	post := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	pr := httptest.NewRecorder()
	routes.ServeHTTP(pr, post)
	if pr.Code != http.StatusSeeOther {
		t.Fatalf("settings POST code=%d", pr.Code)
	}
	if store.data["CHATWOOT_API_TOKEN"] != "supersecret" {
		t.Errorf("blank token should keep stored secret, got %q", store.data["CHATWOOT_API_TOKEN"])
	}
	if store.data["CHATWOOT_ACCOUNT_ID"] != "2" {
		t.Errorf("account id not saved: %q", store.data["CHATWOOT_ACCOUNT_ID"])
	}
}
