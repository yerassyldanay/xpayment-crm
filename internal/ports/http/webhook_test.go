package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
)

type fakeBrain struct {
	called bool
	draft  domain.Draft
}

func (f *fakeBrain) HandleMessage(context.Context, domain.ChatID, domain.Message) (domain.Draft, error) {
	f.called = true
	return f.draft, nil
}

type fakeWriter struct {
	notes  []string
	media  [][]domain.ResolvedAsset
	attrs  map[string]any
	labels []string
}

func (w *fakeWriter) PostPrivateNote(_ context.Context, _ domain.ChatID, text string, media []domain.ResolvedAsset) error {
	w.notes = append(w.notes, text)
	w.media = append(w.media, media)
	return nil
}
func (w *fakeWriter) MergeContactAttributes(_ context.Context, _ domain.ChatID, a map[string]any) error {
	w.attrs = a
	return nil
}
func (w *fakeWriter) SetLabels(_ context.Context, _ domain.ChatID, l []string) error {
	w.labels = l
	return nil
}

type fakeDedup struct{ seen map[string]bool }

func (d *fakeDedup) MarkProcessed(id string) (bool, error) {
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	if d.seen[id] {
		return false, nil
	}
	d.seen[id] = true
	return true, nil
}

const incomingPayload = `{"event":"message_created","id":99,"message_type":"incoming","private":false,
	"content":"цена?","conversation":{"id":123,"meta":{"sender":{"id":456}}},"inbox":{"id":7}}`

func newHandler(brain Brain, w Writer, d Dedup) *WebhookHandler {
	return NewWebhookHandler(brain, w, d, NewManagedInboxGate(nil, []int64{7}), "", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/v1/assistant/webhook/chatwoot", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestWebhook_ProcessesIncomingAndWrites(t *testing.T) {
	brain := &fakeBrain{draft: domain.Draft{
		ReplyText:       "ответ",
		ProfilePatch:    map[string]any{"interested_tariff": "growth"},
		SuggestedStatus: "qualifying",
	}}
	w := &fakeWriter{}
	rec := post(newHandler(brain, w, &fakeDedup{}), incomingPayload)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	if !brain.called {
		t.Fatal("brain should be called for incoming message")
	}
	if len(w.notes) != 1 || !strings.Contains(w.notes[0], "ответ") {
		t.Fatalf("private note not posted: %v", w.notes)
	}
	if w.attrs["interested_tariff"] != "growth" {
		t.Fatalf("attrs not merged: %v", w.attrs)
	}
	if len(w.labels) != 1 || w.labels[0] != "qualifying" {
		t.Fatalf("labels not set: %v", w.labels)
	}
}

func TestWebhook_WarnsOnLidSender(t *testing.T) {
	// A "<digits>@lid" sender (hidden WhatsApp id) gets the delivery warning appended.
	lid := strings.Replace(incomingPayload, `"sender":{"id":456}`, `"sender":{"id":456,"identifier":"5231387607239@lid"}`, 1)
	w := &fakeWriter{}
	post(newHandler(&fakeBrain{draft: domain.Draft{ReplyText: "ответ"}}, w, &fakeDedup{}), lid)
	if len(w.notes) != 1 || !strings.Contains(w.notes[0], lidReplyWarning) {
		t.Fatalf("expected LID warning in note, got %v", w.notes)
	}

	// A normal @s.whatsapp.net sender must NOT get the warning.
	pn := strings.Replace(incomingPayload, `"sender":{"id":456}`, `"sender":{"id":456,"identifier":"77051234567@s.whatsapp.net"}`, 1)
	w2 := &fakeWriter{}
	post(newHandler(&fakeBrain{draft: domain.Draft{ReplyText: "ответ"}}, w2, &fakeDedup{}), pn)
	if len(w2.notes) != 1 || strings.Contains(w2.notes[0], lidReplyWarning) {
		t.Fatalf("normal contact should not get LID warning, got %v", w2.notes)
	}
}

func TestWebhook_IgnoresOutgoing(t *testing.T) {
	brain := &fakeBrain{}
	out := strings.Replace(incomingPayload, `"message_type":"incoming"`, `"message_type":"outgoing"`, 1)
	post(newHandler(brain, &fakeWriter{}, &fakeDedup{}), out)
	if brain.called {
		t.Fatal("outgoing messages must be ignored (loop prevention)")
	}
}

func TestWebhook_IgnoresWrongInbox(t *testing.T) {
	brain := &fakeBrain{}
	other := strings.Replace(incomingPayload, `"inbox":{"id":7}`, `"inbox":{"id":99}`, 1)
	post(newHandler(brain, &fakeWriter{}, &fakeDedup{}), other)
	if brain.called {
		t.Fatal("messages on other inboxes must be ignored")
	}
}

func TestWebhook_AcceptsMultipleFallbackInboxes(t *testing.T) {
	brain := &fakeBrain{}
	h := NewWebhookHandler(brain, &fakeWriter{}, &fakeDedup{}, NewManagedInboxGate(nil, []int64{7, 99}), "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	other := strings.Replace(incomingPayload, `"inbox":{"id":7}`, `"inbox":{"id":99}`, 1)
	post(h, other)
	if !brain.called {
		t.Fatal("configured fallback inbox should be accepted")
	}
}

func TestWebhook_DeduplicatesByMessageID(t *testing.T) {
	brain := &fakeBrain{}
	d := &fakeDedup{}
	h := newHandler(brain, &fakeWriter{}, d)
	post(h, incomingPayload)
	brain.called = false
	post(h, incomingPayload) // same message id 99
	if brain.called {
		t.Fatal("duplicate message id must be skipped")
	}
}
