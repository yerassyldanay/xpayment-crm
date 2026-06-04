package adminui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/yessaliyev/xpayment-crm/internal/domain"
	adminuc "github.com/yessaliyev/xpayment-crm/internal/usecase/admin"
)

// Routes returns the /admin sub-router (mounted under /admin by the main router).
func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	r.Get("/login", h.loginForm)
	r.Post("/login", h.loginSubmit)
	r.Post("/logout", h.logout)
	r.Handle("/static/*", h.staticFS())

	// everything below requires a session
	gated := func(fn http.HandlerFunc) http.HandlerFunc { return h.auth.requireAuth(fn) }
	r.Get("/", gated(h.dashboard))
	r.Post("/publish", gated(h.publish))
	r.Post("/rollback", gated(h.rollback))
	r.Get("/config", gated(h.configForm))
	r.Post("/config", gated(h.configSave))
	r.Get("/topics", gated(h.topics))
	r.Post("/topics", gated(h.topicSave))
	r.Post("/topics/delete", gated(h.topicDelete))
	r.Get("/assets", gated(h.assetsPage))
	r.Post("/assets", gated(h.assetSave))
	r.Post("/assets/delete", gated(h.assetDelete))
	r.Post("/assets/upload", gated(h.assetUpload))
	r.Get("/prices", gated(h.prices))
	r.Post("/prices/tariff", gated(h.tariffSave))
	r.Post("/prices/tariff/delete", gated(h.tariffDelete))
	r.Post("/prices/placeholder", gated(h.placeholderSave))
	r.Post("/prices/placeholder/delete", gated(h.placeholderDelete))
	r.Get("/playground", gated(h.playgroundForm))
	r.Post("/playground", gated(h.playgroundRun))
	r.Get("/audit", gated(h.audit))
	return r
}

func (h *Handler) actor(r *http.Request) string { return h.auth.user }

func redirect(w http.ResponseWriter, r *http.Request, path, okMsg string, err error) {
	if err != nil {
		http.Redirect(w, r, path+"?err="+urlEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, path+"?ok="+urlEscape(okMsg), http.StatusSeeOther)
}

func urlEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "+"), "&", "%26")
}

// --- auth ---

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{}
	if r.URL.Query().Get("err") != "" {
		data["Error"] = "Invalid credentials"
	}
	_ = h.login.ExecuteTemplate(w, "login", data)
}

func (h *Handler) loginSubmit(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if !h.auth.check(r.FormValue("user"), r.FormValue("password")) {
		http.Redirect(w, r, "/admin/login?err=1", http.StatusSeeOther)
		return
	}
	h.auth.issue(w)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.auth.clear(w)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// --- dashboard / lifecycle ---

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	published, _ := h.svc.PublishedConfig()
	draft, _ := h.svc.DraftConfig()
	versions, _ := h.svc.ConfigVersions()
	h.render(w, r, "dashboard", map[string]any{
		"Published": published, "Draft": draft, "Versions": versions,
	})
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	warnings, err := h.svc.Publish(h.actor(r))
	msg := "Published"
	if len(warnings) > 0 {
		msg = "Published with warnings: " + strings.Join(warnings, "; ")
	}
	redirect(w, r, "/admin", msg, err)
}

func (h *Handler) rollback(w http.ResponseWriter, r *http.Request) {
	v, _ := strconv.Atoi(r.FormValue("version"))
	err := h.svc.Rollback(v, h.actor(r))
	redirect(w, r, "/admin", "Rolled back", err)
}

// --- config ---

func (h *Handler) configForm(w http.ResponseWriter, r *http.Request) {
	form, _ := h.svc.DraftConfig()
	if form == nil {
		form, _ = h.svc.PublishedConfig()
	}
	if form == nil {
		form = &adminuc.ConfigView{ReplyMaxWords: 120}
	}
	h.render(w, r, "config", map[string]any{"Form": form})
}

func (h *Handler) configSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	maxWords, _ := strconv.Atoi(r.FormValue("reply_max_words"))
	err := h.svc.SaveDraftConfig(adminuc.ConfigInput{
		Persona:        r.FormValue("persona"),
		Mission:        r.FormValue("mission"),
		Guardrails:     r.FormValue("guardrails"),
		LanguagePolicy: r.FormValue("language_policy"),
		ReplyMaxWords:  maxWords,
	}, h.actor(r))
	redirect(w, r, "/admin/config", "Draft saved", err)
}

// --- topics ---

func (h *Handler) topics(w http.ResponseWriter, r *http.Request) {
	topics, _ := h.svc.Topics()
	edit := adminuc.TopicRow{Language: "ru", Active: true}
	if id := r.URL.Query().Get("edit"); id != "" {
		for _, t := range topics {
			if strconv.FormatInt(t.ID, 10) == id {
				edit = t
				break
			}
		}
	}
	h.render(w, r, "topics", map[string]any{"Topics": topics, "Edit": edit})
}

func (h *Handler) topicSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	err := h.svc.SaveTopic(adminuc.TopicRow{
		ID: id, Slug: r.FormValue("slug"), Language: r.FormValue("language"),
		Title: r.FormValue("title"), Summary: r.FormValue("summary"),
		BodyMD: r.FormValue("body_md"), Active: r.FormValue("active") != "",
	}, h.actor(r))
	redirect(w, r, "/admin/topics", "Topic saved", err)
}

func (h *Handler) topicDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	redirect(w, r, "/admin/topics", "Topic deleted", h.svc.DeleteTopic(id, h.actor(r)))
}

// --- assets ---

func (h *Handler) assetsPage(w http.ResponseWriter, r *http.Request) {
	list, _ := h.svc.Assets()
	edit := adminuc.AssetRow{Language: "any", Kind: "image", Active: true}
	if id := r.URL.Query().Get("edit"); id != "" {
		for _, a := range list {
			if strconv.FormatInt(a.ID, 10) == id {
				edit = a
				break
			}
		}
	}
	h.render(w, r, "assets", map[string]any{
		"Assets": list, "Edit": edit,
		"Kinds": []string{"image", "video", "screen_recording", "gif", "link", "document"},
		"Langs": []string{"any", "ru", "kk"},
	})
}

func (h *Handler) assetSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	err := h.svc.SaveAsset(adminuc.AssetRow{
		ID: id, Ref: r.FormValue("ref"), TopicSlug: r.FormValue("topic_slug"),
		Kind: r.FormValue("kind"), URL: r.FormValue("url"), Title: r.FormValue("title"),
		Description: r.FormValue("description"), Language: r.FormValue("language"),
		Active: r.FormValue("active") != "",
	}, h.actor(r))
	redirect(w, r, "/admin/assets", "Asset saved", err)
}

func (h *Handler) assetDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	redirect(w, r, "/admin/assets", "Asset deleted", h.svc.DeleteAsset(id, h.actor(r)))
}

func (h *Handler) assetUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		redirect(w, r, "/admin/assets", "", err)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		redirect(w, r, "/admin/assets", "", err)
		return
	}
	defer file.Close()
	name := filepath.Base(hdr.Filename)
	if err := os.MkdirAll(h.mediaDir, 0o755); err != nil {
		redirect(w, r, "/admin/assets", "", err)
		return
	}
	dst, err := os.Create(filepath.Join(h.mediaDir, name))
	if err != nil {
		redirect(w, r, "/admin/assets", "", err)
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		redirect(w, r, "/admin/assets", "", err)
		return
	}
	redirect(w, r, "/admin/assets", "Uploaded — URL: /media/"+name, nil)
}

// --- prices ---

func (h *Handler) prices(w http.ResponseWriter, r *http.Request) {
	tariffs, _ := h.svc.Tariffs()
	placeholders, _ := h.svc.Placeholders()
	h.render(w, r, "prices", map[string]any{"Tariffs": tariffs, "Placeholders": placeholders})
}

func (h *Handler) tariffSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	price, _ := strconv.ParseInt(r.FormValue("price_tenge"), 10, 64)
	limit, _ := strconv.ParseInt(r.FormValue("cashier_limit"), 10, 64)
	err := h.svc.SaveTariff(domain.Tariff{Key: r.FormValue("key"), PriceTenge: price, CashierLimit: limit}, h.actor(r))
	redirect(w, r, "/admin/prices", "Tariff saved", err)
}

func (h *Handler) tariffDelete(w http.ResponseWriter, r *http.Request) {
	redirect(w, r, "/admin/prices", "Tariff deleted", h.svc.DeleteTariff(r.FormValue("key"), h.actor(r)))
}

func (h *Handler) placeholderSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	err := h.svc.SavePlaceholder(domain.Placeholder{
		Token: r.FormValue("token"), ValueRU: r.FormValue("value_ru"), ValueKK: r.FormValue("value_kk"),
	}, h.actor(r))
	redirect(w, r, "/admin/prices", "Placeholder saved", err)
}

func (h *Handler) placeholderDelete(w http.ResponseWriter, r *http.Request) {
	redirect(w, r, "/admin/prices", "Placeholder deleted", h.svc.DeletePlaceholder(r.FormValue("token"), h.actor(r)))
}

// --- playground ---

func (h *Handler) playgroundForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "playground", map[string]any{})
}

func (h *Handler) playgroundRun(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	msg := r.FormValue("message")
	profileJSON := strings.TrimSpace(r.FormValue("profile"))
	data := map[string]any{"Ran": true, "Message": msg, "ProfileJSON": profileJSON}

	var profile map[string]any
	if profileJSON != "" {
		if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
			data["RunError"] = "Profile is not valid JSON: " + err.Error()
			h.render(w, r, "playground", data)
			return
		}
	}
	res, err := h.svc.Playground(context.Background(), profile, nil, msg)
	if err != nil {
		data["RunError"] = err.Error()
		h.render(w, r, "playground", data)
		return
	}
	data["Result"] = res
	if len(res.Draft.ProfilePatch) > 0 {
		if b, err := json.Marshal(res.Draft.ProfilePatch); err == nil {
			data["ProfilePatchJSON"] = string(b)
		}
	}
	h.render(w, r, "playground", data)
}

// --- audit ---

func (h *Handler) audit(w http.ResponseWriter, r *http.Request) {
	rows, _ := h.svc.Audit(200)
	h.render(w, r, "audit", map[string]any{"Audit": rows})
}
