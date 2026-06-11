// Package whatsapp coordinates Evolution instances, Chatwoot API inboxes, and
// the brain's managed-inbox allowlist.
package whatsapp

import "context"

type Config struct {
	ChatwootAccountID              string
	ChatwootToken                  string
	EvolutionChatwootURL           string
	EvolutionOrganization          string
	EvolutionEventWebhookURL       string
	ChatwootToEvolutionWebhookBase string
	BrainWebhookURL                string
}

type Instance struct {
	Name            string
	ID              string
	ConnectionState string
	OwnerJID        string
	ProfileName     string
}

type ChatwootConfig struct {
	Enabled                 bool     `json:"enabled"`
	AccountID               string   `json:"accountId"`
	Token                   string   `json:"token,omitempty"`
	URL                     string   `json:"url"`
	NameInbox               string   `json:"nameInbox"`
	SignMsg                 bool     `json:"signMsg"`
	SignDelimiter           string   `json:"signDelimiter"`
	ReopenConversation      bool     `json:"reopenConversation"`
	ConversationPending     bool     `json:"conversationPending"`
	MergeBrazilContacts     bool     `json:"mergeBrazilContacts"`
	ImportContacts          bool     `json:"importContacts"`
	ImportMessages          bool     `json:"importMessages"`
	DaysLimitImportMessages int      `json:"daysLimitImportMessages"`
	Organization            string   `json:"organization"`
	Logo                    string   `json:"logo"`
	IgnoreJids              []string `json:"ignoreJids"`
	WebhookURL              string   `json:"webhook_url"`
}

type EventWebhook struct {
	Enabled  bool     `json:"enabled"`
	URL      string   `json:"url"`
	Events   []string `json:"events"`
	Base64   bool     `json:"base64"`
	ByEvents bool     `json:"byEvents"`
}

type Inbox struct {
	ID          int64
	Name        string
	ChannelType string
	WebhookURL  string
}

type AccountWebhook struct {
	ID            int64
	URL           string
	Subscriptions []string
}

type ManagedInstance struct {
	InstanceName    string
	InboxID         int64
	InboxName       string
	OwnerJID        string
	ConnectionState string
	ChatwootEnabled bool
	BridgeEnabled   bool
	AIEnabled       bool
	LastAuditStatus string
	LastAuditDetail string
	LastCheckedAt   string
	AttachedAt      string
	DetachedAt      string
	UpdatedAt       string
}

type InstanceView struct {
	Instance             Instance
	Managed              *ManagedInstance
	Chatwoot             *ChatwootConfig
	EventWebhook         *EventWebhook
	Inbox                *Inbox
	ExpectedInboxWebhook string
	ExpectedEventWebhook string
	BridgeOK             bool
	EventWebhookOK       bool
	AccountWebhookOK     bool
	AIEnabled            bool
	Status               string
	Warnings             []string
	LastCheckedAt        string
}

type EvolutionClient interface {
	FetchInstances(ctx context.Context) ([]Instance, error)
	ConnectionState(ctx context.Context, instance string) (string, error)
	FindChatwoot(ctx context.Context, instance string) (*ChatwootConfig, error)
	SetChatwoot(ctx context.Context, instance string, cfg ChatwootConfig) error
	FindWebhook(ctx context.Context, instance string) (*EventWebhook, error)
	SetWebhook(ctx context.Context, instance string, wh EventWebhook) error
}

type ChatwootClient interface {
	ListInboxes(ctx context.Context) ([]Inbox, error)
	UpdateInboxWebhook(ctx context.Context, inboxID int64, webhookURL string) error
	ListAccountWebhooks(ctx context.Context) ([]AccountWebhook, error)
	CreateAccountWebhook(ctx context.Context, url string, subscriptions []string) error
}

type Store interface {
	ManagedWhatsAppInstances() ([]ManagedInstance, error)
	ManagedWhatsAppInstance(instance string) (*ManagedInstance, error)
	UpsertManagedWhatsAppInstance(row ManagedInstance, actor string) error
	SetManagedWhatsAppDetached(instance string, actor string) error
	AIEnabledInboxIDs() ([]int64, error)
}
