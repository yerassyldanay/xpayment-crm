// Package domain holds the brain's core types. It has no external dependencies
// (Decision 10): the core is built and tested before any integration exists.
package domain

import "time"

// ChatID identifies a conversation. Channel-agnostic (Decision 4): the brain
// never learns which transport produced it. It bundles the Chatwoot contact id
// because the profile lives on the contact, not the conversation (06 · contact id path).
type ChatID struct {
	ConversationID int64
	ContactID      int64
	InboxID        int64
}

// Role of a message author within the window transcript.
type Role string

const (
	RoleCustomer Role = "customer" // incoming
	RoleAgent    Role = "agent"    // outgoing human reply
)

// Message is the neutral form of a Chatwoot message used in the window.
type Message struct {
	ID        string
	Role      Role
	Content   string
	CreatedAt time.Time
}
