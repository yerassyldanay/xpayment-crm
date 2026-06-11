// Package config loads the brain's environment (docs/05-configuration.md).
// Naming rule (Decision 13): the LLM is reached via OpenRouter but vars are
// provider-neutral — LLM_*, never OPENROUTER_*/OPENAI_*/ANTHROPIC_*.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Env          string
	LogLevel     string
	HTTPAddr     string
	DBPath       string
	MetricsToken string

	LLM       LLM
	Chatwoot  Chatwoot
	Evolution Evolution
	Admin     Admin
	Media     Media
	OTel      OTel
}

type LLM struct {
	Provider      string // openrouter | openai | gemini (switchable)
	APIKey        string
	BaseURL       string // resolved from Provider; override with LLM_BASE_URL
	FastModel     string // cheap/quick model used for drafting
	ThinkingModel string // stronger model for harder reasoning (available to callers)
	MaxTokens     int
	Temperature   float64
}

type Chatwoot struct {
	BaseURL                   string
	AccountID                 string
	APIToken                  string
	InboxID                   int64
	InboxIDs                  []int64
	WebhookSecret             string
	BrainBaseURL              string
	ToEvolutionWebhookBaseURL string
}

type Evolution struct {
	BaseURL         string
	APIKey          string
	ChatwootURL     string
	EventWebhookURL string
	Organization    string
}

type Admin struct {
	User          string
	Password      string
	SessionSecret string
}

type Media struct {
	Dir     string
	BaseURL string
}

type OTel struct {
	Enabled      bool
	OTLPEndpoint string
	ServiceName  string
	SampleRate   float64
}

// Load reads the catalog from the environment, applying defaults and validating
// required values — it refuses to start if a required value is missing.
func Load() (Config, error) {
	c := Config{
		Env:          getEnv("APP_ENV", "prod"),
		LogLevel:     getEnv("LOG_LEVEL", "info"),
		HTTPAddr:     getEnv("HTTP_ADDR", ":8080"),
		DBPath:       getEnv("DB_PATH", "./data/brain.db"),
		MetricsToken: getEnv("METRICS_TOKEN", ""),
		LLM: LLM{
			Provider:      getEnv("LLM_PROVIDER", "openrouter"),
			APIKey:        getEnv("LLM_API_KEY", ""),
			BaseURL:       resolveLLMBaseURL(getEnv("LLM_PROVIDER", "openrouter"), getEnv("LLM_BASE_URL", "")),
			FastModel:     getEnv("LLM_FAST_MODEL", "openai/gpt-4o-mini"),
			ThinkingModel: getEnv("LLM_THINKING_MODEL", "openai/o4-mini"),
			MaxTokens:     getEnvInt("LLM_MAX_TOKENS", 1024),
			Temperature:   getEnvFloat("LLM_TEMPERATURE", 0.3),
		},
		Chatwoot: Chatwoot{
			BaseURL:                   getEnv("CHATWOOT_BASE_URL", ""),
			AccountID:                 getEnv("CHATWOOT_ACCOUNT_ID", ""),
			APIToken:                  getEnv("CHATWOOT_API_TOKEN", ""),
			InboxID:                   int64(getEnvInt("CHATWOOT_INBOX_ID", 0)),
			InboxIDs:                  parseInt64List(getEnv("CHATWOOT_INBOX_IDS", "")),
			WebhookSecret:             getEnv("CHATWOOT_WEBHOOK_SECRET", ""),
			BrainBaseURL:              strings.TrimRight(getEnv("BRAIN_BASE_URL", "http://localhost:8080"), "/"),
			ToEvolutionWebhookBaseURL: strings.TrimRight(getEnv("CHATWOOT_TO_EVOLUTION_WEBHOOK_BASE_URL", "http://localhost:9700/chatwoot/webhook"), "/"),
		},
		Evolution: Evolution{
			BaseURL:         strings.TrimRight(getEnv("EVOLUTION_API_BASE_URL", "http://localhost:9700"), "/"),
			APIKey:          firstNonEmpty(getEnv("EVOLUTION_API_KEY", ""), getEnv("AUTHENTICATION_API_KEY", "")),
			ChatwootURL:     strings.TrimRight(getEnv("EVOLUTION_CHATWOOT_URL", "http://host.docker.internal:3000"), "/"),
			EventWebhookURL: strings.TrimRight(getEnv("EVOLUTION_EVENT_WEBHOOK_URL", "http://evolution-webhook:9701/evolution"), "/"),
			Organization:    getEnv("EVOLUTION_ORGANIZATION", "xpayment"),
		},
		Admin: Admin{
			User:          getEnv("ADMIN_USER", "admin"),
			Password:      getEnv("ADMIN_PASSWORD", ""),
			SessionSecret: getEnv("SESSION_SECRET", ""),
		},
		Media: Media{
			Dir:     getEnv("MEDIA_DIR", "./media"),
			BaseURL: getEnv("MEDIA_BASE_URL", ""),
		},
		OTel: OTel{
			Enabled:      getEnvBool("OTEL_ENABLED", false),
			OTLPEndpoint: getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"),
			ServiceName:  getEnv("OTEL_SERVICE_NAME", "xpayment-crm"),
			SampleRate:   getEnvFloat("OTEL_SAMPLE_RATE", 1.0),
		},
	}

	var missing []string
	require := func(name, val string) {
		if val == "" {
			missing = append(missing, name)
		}
	}
	require("LLM_API_KEY", c.LLM.APIKey)
	require("CHATWOOT_BASE_URL", c.Chatwoot.BaseURL)
	require("CHATWOOT_ACCOUNT_ID", c.Chatwoot.AccountID)
	require("CHATWOOT_API_TOKEN", c.Chatwoot.APIToken)
	require("CHATWOOT_WEBHOOK_SECRET", c.Chatwoot.WebhookSecret)
	require("ADMIN_PASSWORD", c.Admin.Password)
	require("SESSION_SECRET", c.Admin.SessionSecret)
	if len(c.Chatwoot.InboxIDs) == 0 && c.Chatwoot.InboxID > 0 {
		c.Chatwoot.InboxIDs = []int64{c.Chatwoot.InboxID}
	}
	if len(c.Chatwoot.InboxIDs) == 0 {
		missing = append(missing, "CHATWOOT_INBOX_ID or CHATWOOT_INBOX_IDS")
	}
	require("EVOLUTION_API_KEY", c.Evolution.APIKey)
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

// resolveLLMBaseURL maps the provider to its OpenAI-compatible chat/completions
// base URL. LLM_BASE_URL overrides it (e.g. a proxy or self-hosted gateway).
// All three providers speak the OpenAI wire format, so only the URL/key/model change.
func resolveLLMBaseURL(provider, override string) string {
	if override != "" {
		return strings.TrimRight(override, "/")
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		return "https://api.openai.com/v1"
	case "gemini", "google":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "openrouter", "":
		return "https://openrouter.ai/api/v1"
	default:
		return "https://openrouter.ai/api/v1"
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func parseInt64List(raw string) []int64 {
	var out []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.ParseInt(part, 10, 64)
		if err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
