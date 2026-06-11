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
	PostPrivateNote(ctx context.Context, chatID domain.ChatID, text string, media []domain.ResolvedAsset) error
	MergeContactAttributes(ctx context.Context, chatID domain.ChatID, attrs map[string]any) error
	SetLabels(ctx context.Context, chatID domain.ChatID, labels []string) error
}

// Dedup records processed Chatwoot message ids (idempotency across restarts).
type Dedup interface {
	MarkProcessed(messageID string) (ok bool, err error)
}

type InboxGate interface {
	AllowsInbox(ctx context.Context, inboxID int64) (bool, error)
}

type ManagedInboxStore interface {
	AIEnabledInboxIDs() ([]int64, error)
}

type ManagedInboxGate struct {
	store    ManagedInboxStore
	fallback map[int64]bool
}

func NewManagedInboxGate(store ManagedInboxStore, fallbackIDs []int64) *ManagedInboxGate {
	fallback := make(map[int64]bool, len(fallbackIDs))
	for _, id := range fallbackIDs {
		if id > 0 {
			fallback[id] = true
		}
	}
	return &ManagedInboxGate{store: store, fallback: fallback}
}

func (g *ManagedInboxGate) AllowsInbox(_ context.Context, inboxID int64) (bool, error) {
	if g.store != nil {
		ids, err := g.store.AIEnabledInboxIDs()
		if err != nil {
			return false, err
		}
		if len(ids) > 0 {
			for _, id := range ids {
				if id == inboxID {
					return true, nil
				}
			}
			return false, nil
		}
	}
	return g.fallback[inboxID], nil
}

// WebhookHandler receives Chatwoot account-webhook events.
type WebhookHandler struct {
	brain   Brain
	writer  Writer
	dedup   Dedup
	inboxes InboxGate
	secret  string
	log     *slog.Logger
	locks   *keyedMutex
}

func NewWebhookHandler(brain Brain, writer Writer, dedup Dedup, inboxes InboxGate, secret string, log *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		brain: brain, writer: writer, dedup: dedup,
		inboxes: inboxes, secret: secret, log: log, locks: newKeyedMutex(),
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
				ID         int64  `json:"id"`
				Identifier string `json:"identifier"` // WhatsApp JID; "<digits>@lid" = no real number
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

	ok, err := h.shouldProcess(r.Context(), ev)
	if err != nil {
		h.log.Error("inbox gate failed", "inbox_id", ev.Inbox.ID, "err", err)
		writeOK(w)
		return
	}
	if !ok {
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

func (h *WebhookHandler) shouldProcess(ctx context.Context, ev chatwootEvent) (bool, error) {
	if !(ev.Event == "message_created" &&
		ev.MessageType == "incoming" &&
		!ev.Private) {
		return false, nil
	}
	if h.inboxes == nil {
		return true, nil
	}
	return h.inboxes.AllowsInbox(ctx, ev.Inbox.ID)
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
		_ = h.writer.PostPrivateNote(ctx, chatID, "⚠️ Ассистент не смог подготовить черновик. Ответьте, пожалуйста, вручную.", nil)
		return
	}

	// A "<digits>@lid" sender has no real WhatsApp number, so any reply will fail
	// to deliver until the contact is saved on the linked phone — warn the agent.
	lid := strings.HasSuffix(ev.Conversation.Meta.Sender.Identifier, "@lid")
	h.write(ctx, chatID, draft, lid)
}

// write performs the Chatwoot side effects (suggest-only v1).
func (h *WebhookHandler) write(ctx context.Context, chatID domain.ChatID, d domain.Draft, lid bool) {
	note := renderNote(d)
	if lid {
		note += "\n\n" + lidReplyWarning
	}
	if err := h.writer.PostPrivateNote(ctx, chatID, note, d.Media); err != nil {
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

// lidReplyWarning is appended to the draft note when the customer reached us via a
// WhatsApp LID (hidden id): replies won't deliver until their number is saved on the
// linked business phone, so the agent should know before pressing Send.
const lidReplyWarning = "⚠️ Контакт написал через скрытый WhatsApp-ID (LID) — ответ не доставится, пока его номер не сохранён в контактах рабочего телефона."

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
	if len(d.Media) > 0 {
		b.WriteString("\n")
		for _, m := range d.Media {
			fmt.Fprintf(&b, "\n📎 %s (%s)", m.Ref, m.Kind)
		}
		b.WriteString("\n(файлы прикреплены ниже)")
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
