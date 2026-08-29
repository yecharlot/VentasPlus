package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yecharlot/VentasPlus/backend/internal/cloudflare"
	"github.com/yecharlot/VentasPlus/backend/internal/facebook"
	"github.com/yecharlot/VentasPlus/backend/internal/publish"
	"github.com/yecharlot/VentasPlus/backend/internal/whatsapp"
)

type Handlers struct {
	CF *cloudflare.Client
	FB *facebook.Client
	WA *whatsapp.Client
}

func New() *Handlers {
	return &Handlers{
		CF: cloudflare.NewFromEnv(),
		FB: facebook.NewFromEnv(),
		WA: whatsapp.NewFromEnv(),
	}
}

func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.health)
	mux.HandleFunc("/api/v1/info", h.info)
	mux.HandleFunc("/api/publish", h.publish)
	mux.HandleFunc("/api/destinations/whatsapp", h.waGroups)
	mux.HandleFunc("/", h.root)
}

func (h *Handlers) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "VentasPlus API",
		"health":  "/health",
		"publish": "POST /api/publish",
	})
}

func (h *Handlers) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "service": "ventasplus-api", "ts": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) info(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
		"facebook_token": os.Getenv("FACEBOOK_ACCESS_TOKEN") != "",
		"openwa_url":     os.Getenv("OPENWA_URL"),
		"openwa_session": os.Getenv("OPENWA_SESSION_ID") != "",
		"cf_workers":     os.Getenv("CLOUDFLARE_WORKERS_URL"),
	})
}

func (h *Handlers) waGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	out, err := h.WA.ListGroups()
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": out})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) publish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var req publish.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	if req.AgentID == "" {
		req.AgentID = r.Header.Get("X-Agent-Id")
	}
	if req.AgentID == "" {
		req.AgentID = "agent-default"
	}
	if len(req.Products) == 0 {
		http.Error(w, "products required", 400)
		return
	}
	if req.Timestamp == "" {
		req.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if req.Total == 0 {
		req.Total = len(req.Products)
	}

	result := publish.Result{Success: true, Message: "Publicación procesada", Total: len(req.Products)}

	for _, p := range req.Products {
		caption := formatCaption(p)
		// Facebook
		for _, dest := range req.Destinations.Facebook {
			var fr map[string]interface{}
			var err error
			if p.ImageURL != "" {
				fr, err = h.FB.PublishPhoto(dest, caption, p.ImageURL)
			} else {
				fr, err = h.FB.PublishFeed(dest, caption, p.ImageURL)
			}
			if err != nil {
				if fr == nil {
					fr = map[string]interface{}{}
				}
				fr["ok"] = false
				fr["error"] = err.Error()
				fr["target"] = dest
			}
			result.Facebook = append(result.Facebook, fr)
		}
		// WhatsApp
		for _, dest := range req.Destinations.WhatsApp {
			var wr map[string]interface{}
			var err error
			img := p.ImageURL
			if img == "" {
				img = p.ImageBase64
			}
			if img != "" {
				wr, err = h.WA.SendImage(dest, caption, img)
			} else {
				wr, err = h.WA.SendText(dest, caption)
			}
			if err != nil {
				if wr == nil {
					wr = map[string]interface{}{}
				}
				wr["ok"] = false
				wr["error"] = err.Error()
				wr["chatId"] = dest
			}
			result.WhatsApp = append(result.WhatsApp, wr)
		}
	}

	// Persist to Cloudflare DO via Workers
	persist, err := h.CF.SavePublication(map[string]interface{}{
		"agentId":      req.AgentID,
		"products":     req.Products,
		"destinations": req.Destinations,
		"timestamp":    req.Timestamp,
		"total":        req.Total,
		"facebook":     result.Facebook,
		"whatsapp":     result.WhatsApp,
	})
	if err != nil {
		result.PersistError = err.Error()
	} else if id, ok := persist["id"].(string); ok {
		result.PublicationID = id
	}

	// success if at least persist worked OR any channel attempted
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func formatCaption(p publish.Product) string {
	var b strings.Builder
	if p.Name != "" {
		b.WriteString(p.Name)
		b.WriteString("\n")
	}
	if p.Price != "" {
		b.WriteString("Precio: ")
		b.WriteString(p.Price)
		b.WriteString("\n")
	}
	if p.Description != "" {
		b.WriteString(p.Description)
	}
	return strings.TrimSpace(b.String())
}

// silence unused in some builds
var _ = fmt.Sprintf
