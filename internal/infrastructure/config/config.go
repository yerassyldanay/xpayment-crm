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

	LLM      LLM
	Chatwoot Chatwoot
	Admin    Admin
	Media    Media
	OTel     OTel
}

type LLM struct {
	APIKey      string
	BaseURL     string
	Model       string
	MaxTokens   int
	Temperature float64
}

type Chatwoot struct {
	BaseURL       string
	AccountID     string
	APIToken      string
	InboxID       int64
	WebhookSecret string
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
			APIKey:      getEnv("LLM_API_KEY", ""),
			BaseURL:     getEnv("LLM_BASE_URL", "https://openrouter.ai/api/v1"),
			Model:       getEnv("LLM_MODEL", "anthropic/claude-sonnet-4"),
			MaxTokens:   getEnvInt("LLM_MAX_TOKENS", 1024),
			Temperature: getEnvFloat("LLM_TEMPERATURE", 0.3),
		},
		Chatwoot: Chatwoot{
			BaseURL:       getEnv("CHATWOOT_BASE_URL", ""),
			AccountID:     getEnv("CHATWOOT_ACCOUNT_ID", ""),
			APIToken:      getEnv("CHATWOOT_API_TOKEN", ""),
			InboxID:       int64(getEnvInt("CHATWOOT_INBOX_ID", 0)),
			WebhookSecret: getEnv("CHATWOOT_WEBHOOK_SECRET", ""),
		},
		Admin: Admin{
			User:          getEnv("ADMIN_USER", "admin"),
			Password:      getEnv("ADMIN_PASSWORD", ""),
			SessionSecret: getEnv("SESSION_SECRET", ""),
		},
		Media: Media{
			Dir:     getEnv("MEDIA_DIR", "./data/media"),
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
	if c.Chatwoot.InboxID == 0 {
		missing = append(missing, "CHATWOOT_INBOX_ID")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	return c, nil
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
