package assistant

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
)

// frame is the code-owned [A] block — the hard rules, never editable (docs/10 · [A]).
const frame = `You are the drafting engine for xpayment's WhatsApp sales assistant. You write ONE reply
draft that a human will review and send. You never send messages yourself.

Rules (hard, non-negotiable):
1. Answer ONLY from the KNOWLEDGE BASE below. If the answer is not there, do not guess —
   set "escalate": true with a short escalation_reason and a brief holding reply.
2. NEVER write a price or a number of cashiers as a digit. Use the tokens exactly as written
   in the knowledge base, e.g. {{price.growth}}, {{limit.growth}}. Code fills the real values after you.
3. Attach media ONLY by returning refs that exist in the MEDIA CATALOG. Maximum 3. If none fit, [].
4. Reply in the customer's language. If the latest message mixes Kazakh and Russian, reply in Russian.
5. Keep the reply under ~120 words, warm and concrete. One clear next step or question.
6. Never ask for or repeat passwords. When trust comes up, use the "cashier role" explanation from the KB.
7. Extract into profile_patch ONLY facts you are newly confident about. Do not invent fields.

You MUST respond by calling the emit_draft tool with the required JSON. No prose outside the tool call.`

// BuildSystem renders the cache-stable system prefix [A]–[E] from the snapshot
// (docs/10 · implementation checklist step 1). Rebuilt only on publish.
func BuildSystem(s *domain.Snapshot) string {
	var b strings.Builder
	b.WriteString(frame)
	b.WriteString("\n\n# IDENTITY\n")
	b.WriteString(s.Config.Persona)
	if s.Config.Mission != "" {
		b.WriteString("\nMission: ")
		b.WriteString(s.Config.Mission)
	}
	b.WriteString("\n\n# GUARDRAILS\n")
	b.WriteString(s.Config.Guardrails)
	if s.Config.LanguagePolicy != "" {
		b.WriteString("\n")
		b.WriteString(s.Config.LanguagePolicy)
	}

	b.WriteString("\n\nKNOWLEDGE BASE:\n")
	for _, t := range s.Topics {
		fmt.Fprintf(&b, "\n# topic: %s (%s)\n%s\n", t.Slug, t.Language, t.BodyMD)
	}

	b.WriteString("\nMEDIA CATALOG:\n")
	b.WriteString("ref | kind | topic | description\n")
	for _, a := range s.Assets {
		fmt.Fprintf(&b, "%s | %s | %s | %s\n", a.Ref, a.Kind, a.TopicSlug, a.Description)
	}
	return b.String()
}

// BuildUser builds the dynamic per-message block: PROFILE + the window transcript
// (oldest first, one user turn — docs/10 · why one user turn) + the current message.
func BuildUser(profile map[string]any, window []domain.Message, current domain.Message) string {
	var b strings.Builder
	b.WriteString("PROFILE (what we already know about this contact):\n")
	b.WriteString(marshalProfile(profile))

	b.WriteString("\n\nCONVERSATION (most recent messages, oldest first):\n")
	for _, m := range window {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
	}

	b.WriteString("\nCURRENT MESSAGE:\n")
	fmt.Fprintf(&b, "%s: %s\n", domain.RoleCustomer, current.Content)
	return b.String()
}

func marshalProfile(profile map[string]any) string {
	if len(profile) == 0 {
		return "{}"
	}
	// json.Marshal sorts map keys, so the block is deterministic across calls.
	out, err := json.Marshal(profile)
	if err != nil {
		return "{}"
	}
	return string(out)
}
