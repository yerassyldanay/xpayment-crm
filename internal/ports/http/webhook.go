package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
	"github.com/yessaliyev/xpayment-crm/internal/usecase/assistant"
)

// Brain is the subset of the assistant the webhook needs.
type Brain interface {
	HandleMessage(ctx context.Context, chatID domain.ChatID, inbound domain.Message) (domain.Draft, error)
}

// Writer is the subset of ChatwootWriter the webhook needs.
type Writer interface {
	PostPrivateNote(ctx context.Context, chatID domain.ChatID, text string) error
	MergeContactAttributes(ctx context.Context, chatID domain.ChatID, attrs map[string]any) error
	SetLabels(ctx context.Context, chatID domain.ChatID, labels []string) error
}

// Dedup records processed Chatwoot message ids (idempotency across restarts).
type Dedup interface {
	MarkProcessed(messageID string) (ok bool, err error)
}

// WebhookHandler receives Chatwoot account-webhook events.
type WebhookHandler struct {
	brain   Brain
	writer  Writer
	dedup   Dedup
	inboxID int64
	secret  string
	log     *slog.Logger
	locks   *keyedMutex
}

func NewWebhookHandler(brain Brain, writer Writer, dedup Dedup, inboxID int64, secret string, log *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		brain: brain, writer: writer, dedup: dedup,
		inboxID: inboxID, secret: secret, log: log, locks: newKeyedMutex(),
	}
}

// chatwootEvent is the message_created payload (fields vary by version — verify).
type chatwootEvent struct {
	Event        string `json:"event"`
	ID           int64  `json:"id"`           // the message id
	MessageType  string `json:"message_type"` // incoming | outgoing | template
	Private      bool   `json:"private"`
	Content      string `json:"content"`
	Conversation struct {
		ID   int64 `json:"id"`
		Meta struct {
			Sender struct {
				ID int64 `json:"id"`
			} `json:"sender"`
		} `json:"meta"`
	} `json:"conversation"`
	Inbox struct {
		ID int64 `json:"id"`
	} `json:"inbox"`
}

// ServeHTTP always returns 200 (even on ignore); never 500 on a recoverable
// error — it posts an escalation note instead (docs/06 · webhook receiver).
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.verifySecret(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var ev chatwootEvent
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&ev); err != nil {
		h.log.Warn("webhook decode failed", "err", err)
		writeOK(w)
		return
	}

	if !h.shouldProcess(ev) {
		writeOK(w)
		return
	}

	// Idempotency: skip a message id we've already handled.
	if ok, err := h.dedup.MarkProcessed(fmt.Sprintf("%d", ev.ID)); err != nil {
		h.log.Error("dedup failed", "err", err)
		writeOK(w)
		return
	} else if !ok {
		h.log.Debug("duplicate webhook ignored", "message_id", ev.ID)
		writeOK(w)
		return
	}

	h.process(r.Context(), ev)
	writeOK(w)
}

func (h *WebhookHandler) shouldProcess(ev chatwootEvent) bool {
	return ev.Event == "message_created" &&
		ev.MessageType == "incoming" &&
		!ev.Private &&
		ev.Inbox.ID == h.inboxID
}

func (h *WebhookHandler) process(ctx context.Context, ev chatwootEvent) {
	chatID := domain.ChatID{
		ConversationID: ev.Conversation.ID,
		ContactID:      ev.Conversation.Meta.Sender.ID,
		InboxID:        ev.Inbox.ID,
	}
	// Serialize per contact so concurrent messages don't clobber the profile.
	unlock := h.locks.Lock(chatID.ContactID)
	defer unlock()

	inbound := domain.Message{Role: domain.RoleCustomer, Content: ev.Content}
	draft, err := h.brain.HandleMessage(ctx, chatID, inbound)
	if err != nil {
		if errors.Is(err, assistant.ErrNoPublishedConfig) {
			h.log.Warn("no published config; skipping draft", "chat", chatID.ConversationID)
			return
		}
		h.log.Error("HandleMessage failed; posting escalation note", "chat", chatID.ConversationID, "err", err)
		_ = h.writer.PostPrivateNote(ctx, chatID, "⚠️ Ассистент не смог подготовить черновик. Ответьте, пожалуйста, вручную.")
		return
	}

	h.write(ctx, chatID, draft)
}

// write performs the Chatwoot side effects (suggest-only v1).
func (h *WebhookHandler) write(ctx context.Context, chatID domain.ChatID, d domain.Draft) {
	if err := h.writer.PostPrivateNote(ctx, chatID, renderNote(d)); err != nil {
		h.log.Error("post private note failed", "chat", chatID.ConversationID, "err", err)
	}
	if d.Escalate {
		return // escalation: no profile/label writes
	}
	if len(d.ProfilePatch) > 0 {
		if err := h.writer.MergeContactAttributes(ctx, chatID, d.ProfilePatch); err != nil {
			h.log.Error("merge attributes failed", "chat", chatID.ConversationID, "err", err)
		}
	}
	if labels := statusLabels(d); len(labels) > 0 {
		if err := h.writer.SetLabels(ctx, chatID, labels); err != nil {
			h.log.Error("set labels failed", "chat", chatID.ConversationID, "err", err)
		}
	}
}

// renderNote builds the private-note body a human reads before sending.
func renderNote(d domain.Draft) string {
	var b strings.Builder
	if d.Escalate {
		b.WriteString("🚨 Требуется человек")
		if d.EscalationReason != "" {
			b.WriteString(": " + d.EscalationReason)
		}
		b.WriteString("\n\n")
		b.WriteString(d.ReplyText)
		return b.String()
	}
	fmt.Fprintf(&b, "🤖 Черновик (confidence %.2f):\n\n%s", d.Confidence, d.ReplyText)
	for _, m := range d.Media {
		fmt.Fprintf(&b, "\n📎 %s — %s", m.Ref, m.URL)
	}
	if d.SuggestedCallback != nil && d.SuggestedCallback.Note != "" {
		fmt.Fprintf(&b, "\n\n⏰ Напоминание: %s (%s)", d.SuggestedCallback.Note, d.SuggestedCallback.DueAt)
	}
	return b.String()
}

func statusLabels(d domain.Draft) []string {
	var out []string
	if d.SuggestedStatus != "" {
		out = append(out, d.SuggestedStatus)
	}
	return out
}

func (h *WebhookHandler) verifySecret(r *http.Request) bool {
	if h.secret == "" {
		return true // no secret configured (dev)
	}
	got := r.Header.Get("X-Chatwoot-Webhook-Secret")
	if got == "" {
		got = r.URL.Query().Get("secret")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.secret)) == 1
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
