// Package settings manages the runtime-editable Evolution/Chatwoot bridge
// connection config exposed by the admin Settings page. Values are persisted in
// a key/value store (keys mirror the env var names) and overlaid on the env
// defaults; saving them also reconfigures the live clients via an Applier.
package settings

import "strings"

// Storage keys — mirror the env var names so the table is self-documenting.
const (
	keyEvolutionBaseURL         = "EVOLUTION_API_BASE_URL"
	keyEvolutionAPIKey          = "EVOLUTION_API_KEY"
	keyEvolutionChatwootURL     = "EVOLUTION_CHATWOOT_URL"
	keyEvolutionOrganization    = "EVOLUTION_ORGANIZATION"
	keyEvolutionEventWebhookURL = "EVOLUTION_EVENT_WEBHOOK_URL"
	keyChatwootBaseURL          = "CHATWOOT_BASE_URL"
	keyChatwootAccountID        = "CHATWOOT_ACCOUNT_ID"
	keyChatwootAPIToken         = "CHATWOOT_API_TOKEN"
	keyChatwootWebhookBase      = "CHATWOOT_TO_EVOLUTION_WEBHOOK_BASE_URL"
)

// Bridge is the full set of connection settings the Settings page manages.
type Bridge struct {
	EvolutionBaseURL               string
	EvolutionAPIKey                string
	EvolutionChatwootURL           string
	EvolutionOrganization          string
	EvolutionEventWebhookURL       string
	ChatwootBaseURL                string
	ChatwootAccountID              string
	ChatwootAPIToken               string
	ChatwootToEvolutionWebhookBase string
}

// Store persists the settings key/value pairs.
type Store interface {
	BridgeSettings() (map[string]string, error)
	SaveBridgeSettings(values map[string]string, actor string) error
}

// Applier pushes a saved Bridge onto the live clients (no restart needed).
type Applier interface {
	ApplyBridge(Bridge)
}

// Service reads/writes the bridge settings.
type Service struct {
	store    Store
	applier  Applier
	defaults Bridge // env-derived fallback for keys not (yet) stored
}

func NewService(store Store, applier Applier, defaults Bridge) *Service {
	return &Service{store: store, applier: applier, defaults: defaults}
}

// Current returns the effective settings: stored values overlaid on the env
// defaults.
func (s *Service) Current() (Bridge, error) {
	kv, err := s.store.BridgeSettings()
	if err != nil {
		return Bridge{}, err
	}
	return s.defaults.merge(kv), nil
}

// Save normalizes, persists, then hot-applies the settings.
func (s *Service) Save(b Bridge, actor string) error {
	b = b.normalized()
	if err := s.store.SaveBridgeSettings(b.toMap(), actor); err != nil {
		return err
	}
	if s.applier != nil {
		s.applier.ApplyBridge(b)
	}
	return nil
}

// merge overlays non-empty stored values onto the receiver (env defaults).
func (b Bridge) merge(kv map[string]string) Bridge {
	set := func(dst *string, key string) {
		if v, ok := kv[key]; ok && v != "" {
			*dst = v
		}
	}
	set(&b.EvolutionBaseURL, keyEvolutionBaseURL)
	set(&b.EvolutionAPIKey, keyEvolutionAPIKey)
	set(&b.EvolutionChatwootURL, keyEvolutionChatwootURL)
	set(&b.EvolutionOrganization, keyEvolutionOrganization)
	set(&b.EvolutionEventWebhookURL, keyEvolutionEventWebhookURL)
	set(&b.ChatwootBaseURL, keyChatwootBaseURL)
	set(&b.ChatwootAccountID, keyChatwootAccountID)
	set(&b.ChatwootAPIToken, keyChatwootAPIToken)
	set(&b.ChatwootToEvolutionWebhookBase, keyChatwootWebhookBase)
	return b
}

func (b Bridge) toMap() map[string]string {
	return map[string]string{
		keyEvolutionBaseURL:         b.EvolutionBaseURL,
		keyEvolutionAPIKey:          b.EvolutionAPIKey,
		keyEvolutionChatwootURL:     b.EvolutionChatwootURL,
		keyEvolutionOrganization:    b.EvolutionOrganization,
		keyEvolutionEventWebhookURL: b.EvolutionEventWebhookURL,
		keyChatwootBaseURL:          b.ChatwootBaseURL,
		keyChatwootAccountID:        b.ChatwootAccountID,
		keyChatwootAPIToken:         b.ChatwootAPIToken,
		keyChatwootWebhookBase:      b.ChatwootToEvolutionWebhookBase,
	}
}

// normalized trims surrounding whitespace, and trailing slashes on URL fields,
// to match how config.Load treats the same values.
func (b Bridge) normalized() Bridge {
	trim := func(s string) string { return strings.TrimSpace(s) }
	trimURL := func(s string) string { return strings.TrimRight(strings.TrimSpace(s), "/") }
	return Bridge{
		EvolutionBaseURL:               trimURL(b.EvolutionBaseURL),
		EvolutionAPIKey:                trim(b.EvolutionAPIKey),
		EvolutionChatwootURL:           trimURL(b.EvolutionChatwootURL),
		EvolutionOrganization:          trim(b.EvolutionOrganization),
		EvolutionEventWebhookURL:       trimURL(b.EvolutionEventWebhookURL),
		ChatwootBaseURL:                trimURL(b.ChatwootBaseURL),
		ChatwootAccountID:              trim(b.ChatwootAccountID),
		ChatwootAPIToken:               trim(b.ChatwootAPIToken),
		ChatwootToEvolutionWebhookBase: trimURL(b.ChatwootToEvolutionWebhookBase),
	}
}
