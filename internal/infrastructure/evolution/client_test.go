package evolution

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yessaliyev/xpayment-crm/internal/usecase/whatsapp"
)

// SetChatwoot must always send ignoreJids as a JSON array; a nil slice would
// marshal to null and Evolution rejects it ("ignoreJids is not of a type(s) array").
func TestSetChatwootIgnoreJidsAlwaysArray(t *testing.T) {
	cases := map[string]whatsapp.ChatwootConfig{
		"nil":       {AccountID: "2"},
		"populated": {AccountID: "2", IgnoreJids: []string{"123@g.us"}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &got)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			c := New(srv.URL, "key")
			if err := c.SetChatwoot(context.Background(), "xpayment", cfg); err != nil {
				t.Fatalf("SetChatwoot: %v", err)
			}
			raw, ok := got["ignoreJids"]
			if !ok {
				t.Fatal("ignoreJids missing from request body")
			}
			if _, isArray := raw.([]any); !isArray {
				t.Fatalf("ignoreJids must be a JSON array, got %T (%v)", raw, raw)
			}
		})
	}
}

// Configure swaps the target endpoint in place (Settings hot-reload).
func TestConfigureSwapsEndpoint(t *testing.T) {
	var hitA, hitB bool
	srvA := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hitA = true }))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hitB = true }))
	defer srvB.Close()

	c := New(srvA.URL, "k")
	_, _ = c.ConnectionState(context.Background(), "x")
	c.Configure(srvB.URL, "k2")
	_, _ = c.ConnectionState(context.Background(), "x")
	if !hitA || !hitB {
		t.Fatalf("expected both endpoints hit after Configure: A=%v B=%v", hitA, hitB)
	}
}
