// Package evolution contains the Evolution API adapter used by the WhatsApp
// provisioning UI.
package evolution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yessaliyev/xpayment-crm/internal/usecase/whatsapp"
)

type Client struct {
	httpc *http.Client

	mu     sync.RWMutex // guards base/apiKey so the Settings page can reconfigure live
	base   string
	apiKey string
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		httpc:  &http.Client{Timeout: 30 * time.Second},
		base:   strings.TrimRight(baseURL, "/"),
		apiKey: apiKey,
	}
}

// Configure swaps the connection settings in place so saved Settings apply
// without restarting the process.
func (c *Client) Configure(baseURL, apiKey string) {
	c.mu.Lock()
	c.base = strings.TrimRight(baseURL, "/")
	c.apiKey = apiKey
	c.mu.Unlock()
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	c.mu.RLock()
	base, apiKey := c.base, c.apiKey
	c.mu.RUnlock()

	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("evolution %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound && out != nil {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("evolution %s %s: http %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil && len(data) > 0 && string(data) != "null" {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("evolution decode %s: %w", path, err)
		}
	}
	return nil
}

func (c *Client) FetchInstances(ctx context.Context) ([]whatsapp.Instance, error) {
	var raw []map[string]any
	if err := c.do(ctx, http.MethodGet, "/instance/fetchInstances", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]whatsapp.Instance, 0, len(raw))
	for _, item := range raw {
		instMap := nestedMap(item, "instance")
		out = append(out, whatsapp.Instance{
			Name:            firstString(item, instMap, "name", "instanceName"),
			ID:              firstString(item, instMap, "id", "instanceId"),
			ConnectionState: firstString(item, instMap, "connectionStatus", "status", "state"),
			OwnerJID:        firstString(item, instMap, "ownerJid", "owner"),
			ProfileName:     firstString(item, instMap, "profileName", "profileName"),
		})
	}
	return out, nil
}

func (c *Client) ConnectionState(ctx context.Context, instance string) (string, error) {
	var raw map[string]any
	if err := c.do(ctx, http.MethodGet, "/instance/connectionState/"+instance, nil, &raw); err != nil {
		return "", err
	}
	inst := nestedMap(raw, "instance")
	return firstString(raw, inst, "state", "connectionStatus"), nil
}

func (c *Client) FindChatwoot(ctx context.Context, instance string) (*whatsapp.ChatwootConfig, error) {
	var cfg whatsapp.ChatwootConfig
	if err := c.do(ctx, http.MethodGet, "/chatwoot/find/"+instance, nil, &cfg); err != nil {
		return nil, err
	}
	if cfg.AccountID == "" && cfg.URL == "" && cfg.NameInbox == "" && !cfg.Enabled {
		return nil, nil
	}
	return &cfg, nil
}

func (c *Client) SetChatwoot(ctx context.Context, instance string, cfg whatsapp.ChatwootConfig) error {
	// Evolution validates ignoreJids as a JSON array; a nil slice marshals to
	// null and is rejected ("ignoreJids is not of a type(s) array"), so always
	// send at least an empty array.
	ignoreJids := cfg.IgnoreJids
	if ignoreJids == nil {
		ignoreJids = []string{}
	}
	body := map[string]any{
		"enabled":                 cfg.Enabled,
		"accountId":               cfg.AccountID,
		"token":                   cfg.Token,
		"url":                     cfg.URL,
		"signMsg":                 cfg.SignMsg,
		"signDelimiter":           cfg.SignDelimiter,
		"reopenConversation":      cfg.ReopenConversation,
		"conversationPending":     cfg.ConversationPending,
		"mergeBrazilContacts":     cfg.MergeBrazilContacts,
		"importContacts":          cfg.ImportContacts,
		"importMessages":          cfg.ImportMessages,
		"daysLimitImportMessages": cfg.DaysLimitImportMessages,
		"nameInbox":               cfg.NameInbox,
		"organization":            cfg.Organization,
		"logo":                    cfg.Logo,
		"ignoreJids":              ignoreJids,
		"autoCreate":              true,
	}
	return c.do(ctx, http.MethodPost, "/chatwoot/set/"+instance, body, nil)
}

func (c *Client) FindWebhook(ctx context.Context, instance string) (*whatsapp.EventWebhook, error) {
	var raw struct {
		Webhook  *whatsapp.EventWebhook `json:"webhook"`
		Enabled  bool                   `json:"enabled"`
		URL      string                 `json:"url"`
		Events   []string               `json:"events"`
		Base64   bool                   `json:"base64"`
		ByEvents bool                   `json:"byEvents"`
	}
	if err := c.do(ctx, http.MethodGet, "/webhook/find/"+instance, nil, &raw); err != nil {
		return nil, err
	}
	if raw.Webhook != nil {
		return raw.Webhook, nil
	}
	if raw.URL == "" && len(raw.Events) == 0 && !raw.Enabled {
		return nil, nil
	}
	return &whatsapp.EventWebhook{
		Enabled: raw.Enabled, URL: raw.URL, Events: raw.Events, Base64: raw.Base64, ByEvents: raw.ByEvents,
	}, nil
}

func (c *Client) SetWebhook(ctx context.Context, instance string, wh whatsapp.EventWebhook) error {
	body := map[string]any{"webhook": map[string]any{
		"enabled":  wh.Enabled,
		"url":      wh.URL,
		"events":   wh.Events,
		"base64":   wh.Base64,
		"byEvents": wh.ByEvents,
	}}
	return c.do(ctx, http.MethodPost, "/webhook/set/"+instance, body, nil)
}

func nestedMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func firstString(primary, nested map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := primary[key].(string); ok && v != "" {
			return v
		}
		if nested != nil {
			if v, ok := nested[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return ""
}
