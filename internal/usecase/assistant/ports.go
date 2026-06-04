// Package assistant is the brain's core (Decision 10): HandleMessage plus the
// ports it talks to the outside through. Each port is mocked in tests, so the
// whole pipeline runs with zero external services.
package assistant

import (
	"context"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
)

// ContentSource is the immutable content snapshot, hot-swapped on publish.
type ContentSource interface {
	Get() *domain.Snapshot
}

// ChatwootReader reads conversation context from Chatwoot (the hub).
type ChatwootReader interface {
	Window(ctx context.Context, chatID domain.ChatID) ([]domain.Message, error) // last ~15 msgs / 48h
	Profile(ctx context.Context, chatID domain.ChatID) (map[string]any, error)  // contact custom attributes
}

// ChatwootWriter writes results back to Chatwoot.
type ChatwootWriter interface {
	PostPrivateNote(ctx context.Context, chatID domain.ChatID, text string) error
	MergeContactAttributes(ctx context.Context, chatID domain.ChatID, attrs map[string]any) error
	SetLabels(ctx context.Context, chatID domain.ChatID, labels []string) error
	SendOutgoing(ctx context.Context, chatID domain.ChatID, text string, media []domain.ResolvedAsset) error // Phase 3
}

// Prompt is the assembled LLM input: a cache-stable System prefix + a dynamic User block.
type Prompt struct {
	System string
	User   string
}

// Drafter calls the LLM with the assembled prompt and returns the parsed contract.
type Drafter interface {
	Draft(ctx context.Context, prompt Prompt) (domain.RawDraft, error)
}
