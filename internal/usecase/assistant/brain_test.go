package assistant

import (
	"context"
	"errors"
	"testing"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
)

// --- mocked ports ---

type stubContent struct{ snap *domain.Snapshot }

func (s stubContent) Get() *domain.Snapshot { return s.snap }

type stubReader struct {
	window  []domain.Message
	profile map[string]any
}

func (s stubReader) Window(context.Context, domain.ChatID) ([]domain.Message, error) {
	return s.window, nil
}
func (s stubReader) Profile(context.Context, domain.ChatID) (map[string]any, error) {
	return s.profile, nil
}

type stubDrafter struct {
	raw domain.RawDraft
	err error
}

func (s stubDrafter) Draft(context.Context, Prompt) (domain.RawDraft, error) {
	return s.raw, s.err
}

func testSnapshot() *domain.Snapshot {
	return &domain.Snapshot{
		Config: domain.AssistantConfig{Persona: "p", Guardrails: "g"},
		Prices: domain.PriceBook{Tariffs: map[string]domain.Tariff{
			"growth": {Key: "growth", PriceTenge: 19900, CashierLimit: 5},
		}},
		Topics: []domain.Topic{{Slug: "tariffs", Language: "ru", BodyMD: "{{price.growth}}"}},
		Assets: []domain.Asset{{Ref: "tariffs_table_ru", Kind: "image", URL: "/media/t.png"}},
	}
}

func newBrain(reader ChatwootReader, drafter Drafter) *Brain {
	return New(stubContent{snap: testSnapshot()}, reader, drafter, nil)
}

// --- tests ---

func TestHandleMessage_NormalPricingAnswer(t *testing.T) {
	reader := stubReader{profile: map[string]any{"business_type": "интернет-магазин"}}
	drafter := stubDrafter{raw: domain.RawDraft{
		ReplyText:       "Growth — до {{limit.growth}} касс, {{price.growth}}/мес.",
		ReplyLanguage:   "ru",
		AssetRefs:       []string{"tariffs_table_ru", "hallucinated_ref"},
		ProfilePatch:    map[string]any{"interested_tariff": "growth", "stage": "qualifying"},
		SuggestedStatus: &domain.StageWrapper{Stage: "qualifying"},
		Confidence:      0.82,
	}}
	d, err := newBrain(reader, drafter).HandleMessage(context.Background(), domain.ChatID{}, domain.Message{Content: "цена?"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.ReplyText != "Growth — до 5 касс, 19 900 ₸/мес." {
		t.Fatalf("prices not injected: %q", d.ReplyText)
	}
	if len(d.Media) != 1 || d.Media[0].Ref != "tariffs_table_ru" {
		t.Fatalf("media not resolved: %+v", d.Media)
	}
	if len(d.DroppedRefs) != 1 || d.DroppedRefs[0] != "hallucinated_ref" {
		t.Fatalf("hallucinated ref not dropped: %v", d.DroppedRefs)
	}
	if _, ok := d.ProfilePatch["stage"]; ok {
		t.Fatal("stage key must be stripped from profile_patch")
	}
	if d.ProfilePatch["interested_tariff"] != "growth" {
		t.Fatalf("profile_patch lost field: %+v", d.ProfilePatch)
	}
	if d.SuggestedStatus != "qualifying" {
		t.Fatalf("status not flattened: %q", d.SuggestedStatus)
	}
	if d.Escalate {
		t.Fatal("should not escalate")
	}
}

func TestHandleMessage_EscalateGateStops(t *testing.T) {
	drafter := stubDrafter{raw: domain.RawDraft{
		ReplyText:        "Уточню у коллеги.",
		Escalate:         true,
		EscalationReason: "off-KB",
		AssetRefs:        []string{"tariffs_table_ru"},
	}}
	d, _ := newBrain(stubReader{}, drafter).HandleMessage(context.Background(), domain.ChatID{}, domain.Message{})
	if !d.Escalate {
		t.Fatal("expected escalate")
	}
	if len(d.Media) != 0 {
		t.Fatal("escalation must carry no media")
	}
}

func TestHandleMessage_PriceRenderFailurePostsManualNote(t *testing.T) {
	drafter := stubDrafter{raw: domain.RawDraft{
		ReplyText:     "{{price.enterprise}}", // unknown tariff
		ReplyLanguage: "ru",
		AssetRefs:     []string{"tariffs_table_ru"},
	}}
	d, _ := newBrain(stubReader{}, drafter).HandleMessage(context.Background(), domain.ChatID{}, domain.Message{})
	if !d.PricingError {
		t.Fatal("expected PricingError")
	}
	if d.ReplyText != pricingManualNote {
		t.Fatalf("expected manual-check note, got %q", d.ReplyText)
	}
	if len(d.Media) != 0 {
		t.Fatal("must not ship media with a pricing failure")
	}
}

func TestHandleMessage_LLMErrorEscalates(t *testing.T) {
	drafter := stubDrafter{err: errors.New("boom")}
	d, err := newBrain(stubReader{}, drafter).HandleMessage(context.Background(), domain.ChatID{}, domain.Message{})
	if err != nil {
		t.Fatalf("LLM error must not bubble as error: %v", err)
	}
	if !d.Escalate {
		t.Fatal("LLM failure should produce an escalation draft")
	}
}

func TestHandleMessage_NoSnapshot(t *testing.T) {
	b := New(stubContent{snap: nil}, stubReader{}, stubDrafter{}, nil)
	if _, err := b.HandleMessage(context.Background(), domain.ChatID{}, domain.Message{}); !errors.Is(err, ErrNoPublishedConfig) {
		t.Fatalf("expected ErrNoPublishedConfig, got %v", err)
	}
}
