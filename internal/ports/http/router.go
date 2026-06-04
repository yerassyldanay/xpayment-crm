package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RouterDeps wires the one public port: webhook + /admin + /media + health.
type RouterDeps struct {
	Webhook      http.Handler // POST /v1/assistant/webhook/chatwoot
	Admin        http.Handler // mounted at /admin
	MediaDir     string       // served at /media
	MetricsToken string       // if set, /metrics requires Bearer
}

// NewRouter builds the single HTTP surface (docs/06 · brain HTTP surface).
func NewRouter(d RouterDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", ok)
	r.Get("/ready", ok)
	r.Get("/metrics", metrics(d.MetricsToken))

	r.Method(http.MethodPost, "/v1/assistant/webhook/chatwoot", d.Webhook)

	if d.Admin != nil {
		r.Mount("/admin", d.Admin)
	}
	if d.MediaDir != "" {
		fs := http.StripPrefix("/media/", http.FileServer(http.Dir(d.MediaDir)))
		r.Handle("/media/*", fs)
	}
	return r
}

func ok(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// metrics is a placeholder RED-metrics endpoint, Bearer-gated when a token is set.
func metrics(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# metrics endpoint (extend with a Prometheus registry)\n"))
	}
}
