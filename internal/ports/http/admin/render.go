// Package adminui is the server-rendered admin UI (docs/08): Go html/template +
// htmx, same-service login. It is mounted under /admin by the main router.
package adminui

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	adminuc "github.com/yessaliyev/xpayment-crm/internal/usecase/admin"
	settingsuc "github.com/yessaliyev/xpayment-crm/internal/usecase/settings"
	whatsappuc "github.com/yessaliyev/xpayment-crm/internal/usecase/whatsapp"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Handler renders and serves the admin UI.
type Handler struct {
	svc      *adminuc.Service
	whatsapp *whatsappuc.Service
	settings *settingsuc.Service
	auth     *auth
	pages    map[string]*template.Template
	login    *template.Template
	mediaDir string
	log      *slog.Logger
}

// New builds the admin handler and parses the embedded templates.
func New(svc *adminuc.Service, whatsappSvc *whatsappuc.Service, settingsSvc *settingsuc.Service, user, password, sessionSecret, mediaDir string, log *slog.Logger) (*Handler, error) {
	pageNames := []string{"dashboard", "config", "topics", "assets", "prices", "playground", "audit", "whatsapp", "settings"}
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		t, err := template.New("layout").ParseFS(assets, "templates/layout.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	login, err := template.ParseFS(assets, "templates/login.html")
	if err != nil {
		return nil, err
	}
	return &Handler{
		svc:      svc,
		whatsapp: whatsappSvc,
		settings: settingsSvc,
		auth:     newAuth(user, password, sessionSecret),
		pages:    pages,
		login:    login,
		mediaDir: mediaDir,
		log:      log,
	}, nil
}

// render executes a page template inside the layout shell.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Page"] = page
	if msg := r.URL.Query().Get("ok"); msg != "" {
		data["Flash"], data["FlashKind"] = msg, "ok"
	}
	if msg := r.URL.Query().Get("err"); msg != "" {
		data["Flash"], data["FlashKind"] = msg, "error"
	}
	t, ok := h.pages[page]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		h.log.Error("template render failed", "page", page, "err", err)
	}
}

// staticFS serves the embedded /admin/static/* assets.
func (h *Handler) staticFS() http.Handler {
	sub, _ := fs.Sub(assets, "static")
	return http.StripPrefix("/admin/static/", http.FileServer(http.FS(sub)))
}
