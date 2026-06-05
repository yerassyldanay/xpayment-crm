package assistant

import (
	"context"
	"log/slog"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
)

// Brain is the assistant core. It decides WHAT to respond and returns a Draft;
// it never writes to Chatwoot (the caller does) — which keeps it trivially
// testable (docs/02 · the contract).
type Brain struct {
	content ContentSource
	reader  ChatwootReader
	drafter Drafter
	log     *slog.Logger
}

// New builds a Brain from its ports.
func New(content ContentSource, reader ChatwootReader, drafter Drafter, log *slog.Logger) *Brain {
	if log == nil {
		log = slog.Default()
	}
	return &Brain{content: content, reader: reader, drafter: drafter, log: log}
}

// HandleMessage is the brain's only entry point. Channel-agnostic (Decision 4):
// it takes a chatID and never learns the transport. Stateless about conversations
// (Decision 2): all context is read live from Chatwoot. It returns a Draft and
// performs no writes (Decision 6).
func (b *Brain) HandleMessage(ctx context.Context, chatID domain.ChatID, inbound domain.Message) (domain.Draft, error) {
	snap := b.content.Get()
	if snap == nil {
		return domain.Draft{}, ErrNoPublishedConfig
	}

	// Context-on-read (docs/02): first-message vs mid-conversation is not a branch,
	// just how much Chatwoot returns.
	window, err := b.reader.Window(ctx, chatID)
	if err != nil {
		return domain.Draft{}, err
	}
	profile, err := b.reader.Profile(ctx, chatID)
	if err != nil {
		return domain.Draft{}, err
	}

	prompt := Prompt{System: BuildSystem(snap), User: BuildUser(profile, window, inbound)}
	raw, err := b.drafter.Draft(ctx, prompt)
	if err != nil {
		// A malformed/failed LLM response is a low-confidence escalation, not a
		// crash (docs/02 · post-processing step 1). No infra error bubbles up.
		b.log.WarnContext(ctx, "llm draft failed; escalating", "chat", chatID.ConversationID, "err", err)
		return escalationDraft(holdingReply, "llm_error: "+err.Error()), nil
	}

	return postProcess(raw, snap, b.log), nil
}

// postProcess runs the pipeline (docs/02 · post-processing), in order.
func postProcess(raw domain.RawDraft, snap *domain.Snapshot, log *slog.Logger) domain.Draft {
	// 2. Escalate gate — flag for a human and stop (no media, no auto-send).
	if raw.Escalate {
		d := escalationDraft(raw.ReplyText, raw.EscalationReason)
		d.Confidence = raw.Confidence
		return d
	}

	// 3. Validate + resolve asset_refs (drop unknown, cap 3).
	resolved, unknown := snap.ResolveAssets(raw.AssetRefs)
	if len(unknown) > 0 {
		log.Warn("dropped unknown asset_refs", "refs", unknown)
	}

	d := domain.Draft{
		Media:             resolved,
		DroppedRefs:       unknown,
		Confidence:        raw.Confidence,
		SuggestedCallback: raw.SuggestedCallback,
	}

	// 4. Inject prices — never ship a half-rendered price (Decision 8).
	lang := raw.ReplyLanguage
	if lang == "" {
		lang = "ru"
	}
	rendered, err := snap.Prices.Render(raw.ReplyText, lang)
	if err != nil {
		log.Warn("price render failed; posting check-pricing note", "err", err)
		d.PricingError = true
		d.ReplyText = pricingManualNote
		d.Media = nil
		return d
	}
	d.ReplyText = rendered

	// 5. profile_patch — drop the stage key (that is status, handled next).
	d.ProfilePatch = cleanProfilePatch(raw.ProfilePatch)

	// 6. status — flatten suggested_status.stage to a label.
	if raw.SuggestedStatus != nil {
		d.SuggestedStatus = raw.SuggestedStatus.Stage
	}

	return d
}

// cleanProfilePatch copies the patch minus the reserved "stage" key (Decision 9).
func cleanProfilePatch(patch map[string]any) map[string]any {
	if len(patch) == 0 {
		return nil
	}
	out := make(map[string]any, len(patch))
	for k, v := range patch {
		if k == "stage" {
			continue
		}
		out[k] = v
	}
	return out
}

func escalationDraft(reply, reason string) domain.Draft {
	return domain.Draft{ReplyText: reply, Escalate: true, EscalationReason: reason}
}

const (
	holdingReply      = "Уточню это у коллеги и вернусь с точным ответом — буквально пару минут."
	pricingManualNote = "⚠️ Не удалось подставить цену автоматически — проверьте тариф вручную перед отправкой."
)
