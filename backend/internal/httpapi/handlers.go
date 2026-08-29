package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yecharlot/VentasPlus/backend/internal/cloudflare"
	"github.com/yecharlot/VentasPlus/backend/internal/publish"
	"github.com/yecharlot/VentasPlus/backend/internal/whatsapp"
)

type Handlers struct {
	CF *cloudflare.Client
	WA *whatsapp.Client
}

func New() *Handlers {
	return &Handlers{
		CF: cloudflare.NewFromEnv(),
		WA: whatsapp.NewFromEnv(),
	}
}

func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.health)
	mux.HandleFunc("/api/v1/info", h.info)
	mux.HandleFunc("/api/publish", h.publish)
	mux.HandleFunc("/api/whatsapp/status", h.waStatus)
	mux.HandleFunc("/api/whatsapp/qr", h.waQR)
	mux.HandleFunc("/api/whatsapp/groups", h.waGroups)
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
		"mode":    "share-facebook + openwa-whatsapp",
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
		"ok":             true,
		"mode":           "user-auth",
		"facebook":       "Web Share / abrir app del usuario (sin token servidor)",
		"whatsapp":       "OpenWA QR del usuario",
		"openwa_url":     os.Getenv("OPENWA_URL"),
		"openwa_session": os.Getenv("OPENWA_SESSION_ID") != "",
		"cf_workers":     os.Getenv("CLOUDFLARE_WORKERS_URL"),
	})
}

func (h *Handlers) proxyOpenWA(method, path string, body io.Reader) (int, []byte, error) {
	base := strings.TrimRight(os.Getenv("OPENWA_URL"), "/")
	if base == "" {
		return 0, nil, fmt.Errorf("OPENWA_URL no configurada — conecta OpenWA o deja WhatsApp solo en modo compartir")
	}
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if k := os.Getenv("OPENWA_API_KEY"); k != "" {
		req.Header.Set("X-API-Key", k)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	return res.StatusCode, raw, nil
}

func (h *Handlers) waStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := os.Getenv("OPENWA_SESSION_ID")
	if sid == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "connected": false,
			"hint": "Define OPENWA_URL y OPENWA_SESSION_ID, o usa Compartir de WhatsApp en la PWA",
		})
		return
	}
	code, raw, err := h.proxyOpenWA(http.MethodGet, "/api/sessions/"+sid, nil)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "connected": false, "error": err.Error()})
		return
	}
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	status, _ := out["status"].(string)
	connected := code < 300 && (status == "ready" || status == "authenticated" || status == "connected")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": code < 300, "connected": connected, "status": status, "sessionId": sid, "raw": out,
	})
}

func (h *Handlers) waQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := os.Getenv("OPENWA_SESSION_ID")
	if sid == "" {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "OPENWA_SESSION_ID no configurada"})
		return
	}
	// try start then qr
	_, _, _ = h.proxyOpenWA(http.MethodPost, "/api/sessions/"+sid+"/start", strings.NewReader("{}"))
	code, raw, err := h.proxyOpenWA(http.MethodGet, "/api/sessions/"+sid+"/qr", nil)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(raw)
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

	result := publish.Result{
		Success: true,
		Message: "WhatsApp procesado; Facebook vía compartir en el dispositivo",
		Total:   len(req.Products),
	}

	// Solo WhatsApp automático (OpenWA). Facebook lo hace el usuario con Web Share en la PWA.
	for _, p := range req.Products {
		caption := formatCaption(p)
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

	persist, err := h.CF.SavePublication(map[string]interface{}{
		"agentId":      req.AgentID,
		"products":     req.Products,
		"destinations": req.Destinations,
		"timestamp":    req.Timestamp,
		"total":        req.Total,
		"whatsapp":     result.WhatsApp,
		"channel":      "wa+share-fb",
	})
	if err != nil {
		result.PersistError = err.Error()
	} else if id, ok := persist["id"].(string); ok {
		result.PublicationID = id
	}

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
