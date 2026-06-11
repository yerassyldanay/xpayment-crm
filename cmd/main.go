// Command xpayment-crm is the standalone brain: Chatwoot webhook + admin UI +
// embedded SQLite, one binary on one port, calling the LLM via OpenRouter.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
	"github.com/yessaliyev/xpayment-crm/internal/infrastructure/chatwoot"
	"github.com/yessaliyev/xpayment-crm/internal/infrastructure/config"
	"github.com/yessaliyev/xpayment-crm/internal/infrastructure/evolution"
	"github.com/yessaliyev/xpayment-crm/internal/infrastructure/llm"
	"github.com/yessaliyev/xpayment-crm/internal/infrastructure/sqlite"
	porthttp "github.com/yessaliyev/xpayment-crm/internal/ports/http"
	adminui "github.com/yessaliyev/xpayment-crm/internal/ports/http/admin"
	"github.com/yessaliyev/xpayment-crm/internal/usecase/admin"
	"github.com/yessaliyev/xpayment-crm/internal/usecase/assistant"
	settingsuc "github.com/yessaliyev/xpayment-crm/internal/usecase/settings"
	whatsappuc "github.com/yessaliyev/xpayment-crm/internal/usecase/whatsapp"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)

	// Ensure the data directories exist (DB_PATH parent + MEDIA_DIR) so the
	// service runs out of the box with the default ./data paths.
	if dir := filepath.Dir(cfg.DBPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create db dir: %w", err)
		}
	}
	if err := os.MkdirAll(cfg.Media.Dir, 0o755); err != nil {
		return fmt.Errorf("create media dir: %w", err)
	}

	// Store + migrations.
	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	// Load the published snapshot into the live pointer.
	content := &domain.Content{}
	if snap, err := store.LoadSnapshot(); err != nil {
		if errors.Is(err, sqlite.ErrNoPublished) {
			log.Warn("no published config yet — open /admin to publish before the brain can draft")
		} else {
			return err
		}
	} else {
		if _, verr := domain.ValidateSnapshot(snap); verr != nil {
			return verr // refuse to boot on an invalid published config (docs/04)
		}
		content.Set(snap)
		log.Info("snapshot loaded", "version", snap.Config.Version, "topics", len(snap.Topics), "assets", len(snap.Assets))
	}

	// Adapters.
	drafter := llm.New(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.FastModel, cfg.LLM.ThinkingModel, cfg.LLM.MaxTokens, cfg.LLM.Temperature)
	log.Info("llm configured", "provider", cfg.LLM.Provider, "base_url", cfg.LLM.BaseURL, "fast_model", cfg.LLM.FastModel, "thinking_model", cfg.LLM.ThinkingModel)
	cw := chatwoot.New(cfg.Chatwoot.BaseURL, cfg.Chatwoot.AccountID, cfg.Chatwoot.APIToken, cfg.Media.Dir)
	evo := evolution.New(cfg.Evolution.BaseURL, cfg.Evolution.APIKey)

	// Usecases.
	brain := assistant.New(content, cw, drafter, log)
	adminSvc := admin.NewService(store, content, drafter)
	brainWebhookURL := cfg.Chatwoot.BrainBaseURL + "/v1/assistant/webhook/chatwoot?secret=" + cfg.Chatwoot.WebhookSecret
	whatsappSvc := whatsappuc.NewService(evo, cw, store, whatsappuc.Config{
		ChatwootAccountID:              cfg.Chatwoot.AccountID,
		ChatwootToken:                  cfg.Chatwoot.APIToken,
		EvolutionChatwootURL:           cfg.Evolution.ChatwootURL,
		EvolutionOrganization:          cfg.Evolution.Organization,
		EvolutionEventWebhookURL:       cfg.Evolution.EventWebhookURL,
		ChatwootToEvolutionWebhookBase: cfg.Chatwoot.ToEvolutionWebhookBaseURL,
		BrainWebhookURL:                brainWebhookURL,
	})

	// Settings: env values are the defaults; the admin Settings page overrides
	// them in app_settings and hot-applies onto the live clients (no restart).
	applier := &bridgeApplier{evo: evo, cw: cw, wa: whatsappSvc, brainWebhookURL: brainWebhookURL}
	settingsSvc := settingsuc.NewService(store, applier, settingsuc.Bridge{
		EvolutionBaseURL:               cfg.Evolution.BaseURL,
		EvolutionAPIKey:                cfg.Evolution.APIKey,
		EvolutionChatwootURL:           cfg.Evolution.ChatwootURL,
		EvolutionOrganization:          cfg.Evolution.Organization,
		EvolutionEventWebhookURL:       cfg.Evolution.EventWebhookURL,
		ChatwootBaseURL:                cfg.Chatwoot.BaseURL,
		ChatwootAccountID:              cfg.Chatwoot.AccountID,
		ChatwootAPIToken:               cfg.Chatwoot.APIToken,
		ChatwootToEvolutionWebhookBase: cfg.Chatwoot.ToEvolutionWebhookBaseURL,
	})
	if eff, err := settingsSvc.Current(); err != nil {
		log.Warn("load saved settings failed; using env config", "err", err)
	} else {
		applier.ApplyBridge(eff) // overlay any persisted overrides at boot
	}

	// HTTP handlers.
	inboxGate := porthttp.NewManagedInboxGate(store, cfg.Chatwoot.InboxIDs)
	webhook := porthttp.NewWebhookHandler(brain, cw, store, inboxGate, cfg.Chatwoot.WebhookSecret, log)
	adminH, err := adminui.New(adminSvc, whatsappSvc, settingsSvc, cfg.Admin.User, cfg.Admin.Password, cfg.Admin.SessionSecret, cfg.Media.Dir, log)
	if err != nil {
		return err
	}
	router := porthttp.NewRouter(porthttp.RouterDeps{
		Webhook:      webhook,
		Admin:        adminH.Routes(),
		MediaDir:     cfg.Media.Dir,
		MetricsToken: cfg.MetricsToken,
	})

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

// bridgeApplier hot-applies saved Settings onto the live clients so changes
// take effect without restarting the process.
type bridgeApplier struct {
	evo             *evolution.Client
	cw              *chatwoot.Client
	wa              *whatsappuc.Service
	brainWebhookURL string
}

func (a *bridgeApplier) ApplyBridge(b settingsuc.Bridge) {
	a.evo.Configure(b.EvolutionBaseURL, b.EvolutionAPIKey)
	a.cw.Configure(b.ChatwootBaseURL, b.ChatwootAccountID, b.ChatwootAPIToken)
	a.wa.SetConfig(whatsappuc.Config{
		ChatwootAccountID:              b.ChatwootAccountID,
		ChatwootToken:                  b.ChatwootAPIToken,
		EvolutionChatwootURL:           b.EvolutionChatwootURL,
		EvolutionOrganization:          b.EvolutionOrganization,
		EvolutionEventWebhookURL:       b.EvolutionEventWebhookURL,
		ChatwootToEvolutionWebhookBase: b.ChatwootToEvolutionWebhookBase,
		BrainWebhookURL:                a.brainWebhookURL,
	})
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}
