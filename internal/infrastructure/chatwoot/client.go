// Package chatwoot is the REST adapter for the hub (docs/06 · Chatwoot contracts).
// Auth header is api_access_token. Paths verify against your deployed version.
package chatwoot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
	"github.com/yessaliyev/xpayment-crm/internal/usecase/whatsapp"
)

// Client implements assistant.ChatwootReader and assistant.ChatwootWriter.
type Client struct {
	httpc      *http.Client
	mu         sync.RWMutex // guards base/token so the Settings page can reconfigure live
	base       string       // {CHATWOOT_BASE_URL}/api/v1/accounts/{account_id}
	token      string
	mediaDir   string // local dir backing /media/* URLs, for attaching files
	windowSize int
}

// New builds the client. windowSize caps the messages read (~15). mediaDir is the
// local directory that backs /media/* asset URLs, used to attach files to notes.
func New(baseURL, accountID, token, mediaDir string) *Client {
	return &Client{
		httpc:      &http.Client{Timeout: 30 * time.Second},
		base:       accountBase(baseURL, accountID),
		token:      token,
		mediaDir:   mediaDir,
		windowSize: 15,
	}
}

func accountBase(baseURL, accountID string) string {
	return fmt.Sprintf("%s/api/v1/accounts/%s", strings.TrimRight(baseURL, "/"), accountID)
}

// Configure swaps the connection settings in place so saved Settings apply
// without restarting the process.
func (c *Client) Configure(baseURL, accountID, token string) {
	c.mu.Lock()
	c.base = accountBase(baseURL, accountID)
	c.token = token
	c.mu.Unlock()
}

func (c *Client) endpoint() (base, token string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.base, c.token
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	base, token := c.endpoint()
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
	req.Header.Set("api_access_token", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("chatwoot %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("chatwoot %s %s: http %d: %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("chatwoot decode %s: %w", path, err)
		}
	}
	return nil
}

// --- ChatwootReader ---

type messagesResp struct {
	Payload []struct {
		ID          int64  `json:"id"`
		Content     string `json:"content"`
		MessageType int    `json:"message_type"` // 0 incoming, 1 outgoing
		Private     bool   `json:"private"`
		CreatedAt   int64  `json:"created_at"`
	} `json:"payload"`
}

// Window reads the conversation's recent messages, dropping private notes and
// keeping only customer (incoming) and human-agent (outgoing) turns, newest ~15.
func (c *Client) Window(ctx context.Context, chatID domain.ChatID) ([]domain.Message, error) {
	var mr messagesResp
	path := fmt.Sprintf("/conversations/%d/messages", chatID.ConversationID)
	if err := c.do(ctx, http.MethodGet, path, nil, &mr); err != nil {
		return nil, err
	}
	out := make([]domain.Message, 0, len(mr.Payload))
	for _, m := range mr.Payload {
		if m.Private || m.Content == "" {
			continue
		}
		role := domain.RoleAgent
		if m.MessageType == 0 {
			role = domain.RoleCustomer
		} else if m.MessageType != 1 {
			continue // skip activity/template messages
		}
		out = append(out, domain.Message{
			ID:        strconv.FormatInt(m.ID, 10),
			Role:      role,
			Content:   m.Content,
			CreatedAt: time.Unix(m.CreatedAt, 0),
		})
	}
	// Keep the most recent windowSize, in chronological order.
	if len(out) > c.windowSize {
		out = out[len(out)-c.windowSize:]
	}
	return out, nil
}

type contactResp struct {
	Payload struct {
		CustomAttributes map[string]any `json:"custom_attributes"`
	} `json:"payload"`
}

// Profile reads the contact's custom attributes (Decision 9).
func (c *Client) Profile(ctx context.Context, chatID domain.ChatID) (map[string]any, error) {
	var cr contactResp
	path := fmt.Sprintf("/contacts/%d", chatID.ContactID)
	if err := c.do(ctx, http.MethodGet, path, nil, &cr); err != nil {
		return nil, err
	}
	if cr.Payload.CustomAttributes == nil {
		return map[string]any{}, nil
	}
	return cr.Payload.CustomAttributes, nil
}

// --- ChatwootWriter ---

// PostPrivateNote posts the draft as an internal note Evolution never forwards.
// Local media (URLs under /media/) are uploaded as real attachments so the agent
// sees inline previews and can forward them; external URLs are appended as links.
func (c *Client) PostPrivateNote(ctx context.Context, chatID domain.ChatID, text string, media []domain.ResolvedAsset) error {
	apiPath := fmt.Sprintf("/conversations/%d/messages", chatID.ConversationID)

	var files []string
	for _, m := range media {
		if strings.HasPrefix(m.URL, "/media/") && c.mediaDir != "" {
			files = append(files, filepath.Join(c.mediaDir, filepath.FromSlash(path.Base(m.URL))))
		} else if m.URL != "" {
			text += "\n" + m.URL // external link — can't attach a local file
		}
	}

	if len(files) == 0 {
		body := map[string]any{"content": text, "message_type": "outgoing", "private": true}
		return c.do(ctx, http.MethodPost, apiPath, body, nil)
	}
	return c.postMultipart(ctx, apiPath, text, files)
}

// postMultipart creates a private note with file attachments (multipart/form-data).
func (c *Client) postMultipart(ctx context.Context, apiPath, content string, files []string) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", content)
	_ = mw.WriteField("message_type", "outgoing")
	_ = mw.WriteField("private", "true")
	for _, fp := range files {
		f, err := os.Open(fp)
		if err != nil {
			return fmt.Errorf("open attachment %s: %w", fp, err)
		}
		part, err := mw.CreateFormFile("attachments[]", filepath.Base(fp))
		if err != nil {
			f.Close()
			return err
		}
		if _, err := io.Copy(part, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	if err := mw.Close(); err != nil {
		return err
	}
	base, token := c.endpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+apiPath, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("api_access_token", token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("chatwoot POST %s (multipart): %w", apiPath, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("chatwoot POST %s (multipart): http %d: %s", apiPath, resp.StatusCode, string(data))
	}
	return nil
}

// MergeContactAttributes is a read-modify-write: GET current attrs, additively
// merge (never null a known field), then PUT (the PUT replaces the whole object).
func (c *Client) MergeContactAttributes(ctx context.Context, chatID domain.ChatID, attrs map[string]any) error {
	if len(attrs) == 0 {
		return nil
	}
	current, err := c.Profile(ctx, chatID)
	if err != nil {
		return err
	}
	merged := make(map[string]any, len(current)+len(attrs))
	for k, v := range current {
		merged[k] = v
	}
	for k, v := range attrs {
		if v == nil {
			continue // never null a known field (Decision 9)
		}
		merged[k] = v
	}
	path := fmt.Sprintf("/contacts/%d", chatID.ContactID)
	return c.do(ctx, http.MethodPut, path, map[string]any{"custom_attributes": merged}, nil)
}

type labelsResp struct {
	Payload []string `json:"payload"`
}

// SetLabels reads existing labels and POSTs the union (the API sets the full list).
func (c *Client) SetLabels(ctx context.Context, chatID domain.ChatID, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	var lr labelsResp
	path := fmt.Sprintf("/conversations/%d/labels", chatID.ConversationID)
	if err := c.do(ctx, http.MethodGet, path, nil, &lr); err != nil {
		return err
	}
	set := make(map[string]bool, len(lr.Payload)+len(labels))
	union := make([]string, 0, len(lr.Payload)+len(labels))
	for _, l := range append(lr.Payload, labels...) {
		if l == "" || set[l] {
			continue
		}
		set[l] = true
		union = append(union, l)
	}
	return c.do(ctx, http.MethodPost, path, map[string]any{"labels": union}, nil)
}

// SendOutgoing creates a real outgoing message (Phase 3 auto-send only). Still
// through Chatwoot, never Evolution (Decision 6).
func (c *Client) SendOutgoing(ctx context.Context, chatID domain.ChatID, text string, media []domain.ResolvedAsset) error {
	for _, m := range media {
		text += "\n" + m.URL
	}
	path := fmt.Sprintf("/conversations/%d/messages", chatID.ConversationID)
	body := map[string]any{"content": text, "message_type": "outgoing", "private": false}
	return c.do(ctx, http.MethodPost, path, body, nil)
}

// --- WhatsApp provisioning helpers ---

type inboxesResp struct {
	Payload []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		ChannelType string `json:"channel_type"`
		WebhookURL  string `json:"webhook_url"`
		Channel     struct {
			WebhookURL string `json:"webhook_url"`
		} `json:"channel"`
	} `json:"payload"`
}

func (c *Client) ListInboxes(ctx context.Context) ([]whatsapp.Inbox, error) {
	var resp inboxesResp
	if err := c.do(ctx, http.MethodGet, "/inboxes", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]whatsapp.Inbox, 0, len(resp.Payload))
	for _, in := range resp.Payload {
		webhookURL := in.WebhookURL
		if webhookURL == "" {
			webhookURL = in.Channel.WebhookURL
		}
		out = append(out, whatsapp.Inbox{
			ID: in.ID, Name: in.Name, ChannelType: in.ChannelType, WebhookURL: webhookURL,
		})
	}
	return out, nil
}

func (c *Client) UpdateInboxWebhook(ctx context.Context, inboxID int64, webhookURL string) error {
	body := map[string]any{"channel": map[string]any{"webhook_url": webhookURL}}
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/inboxes/%d", inboxID), body, nil)
}

type accountWebhooksResp struct {
	Payload struct {
		Webhooks []struct {
			ID            int64    `json:"id"`
			URL           string   `json:"url"`
			Subscriptions []string `json:"subscriptions"`
		} `json:"webhooks"`
	} `json:"payload"`
}

func (c *Client) ListAccountWebhooks(ctx context.Context) ([]whatsapp.AccountWebhook, error) {
	var resp accountWebhooksResp
	if err := c.do(ctx, http.MethodGet, "/webhooks", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]whatsapp.AccountWebhook, 0, len(resp.Payload.Webhooks))
	for _, h := range resp.Payload.Webhooks {
		out = append(out, whatsapp.AccountWebhook{ID: h.ID, URL: h.URL, Subscriptions: h.Subscriptions})
	}
	return out, nil
}

func (c *Client) CreateAccountWebhook(ctx context.Context, url string, subscriptions []string) error {
	body := map[string]any{"url": url, "subscriptions": subscriptions}
	if err := c.do(ctx, http.MethodPost, "/webhooks", body, nil); err == nil {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/webhooks", map[string]any{"webhook": body}, nil)
}
