package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yecharlot/VentasPlus/backend/internal/cloudflare"
	"github.com/yecharlot/VentasPlus/backend/internal/publish"
	"github.com/yecharlot/VentasPlus/backend/internal/whatsapp"
)

type Handlers struct {
	CF    *cloudflare.Client
	WA    *whatsapp.Client
	Cloud *whatsapp.CloudClient
}

func New() *Handlers {
	return &Handlers{
		CF:    cloudflare.NewFromEnv(),
		WA:    whatsapp.NewFromEnv(),
		Cloud: whatsapp.NewCloudFromEnv(),
	}
}

func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.health)
	mux.HandleFunc("/api/v1/info", h.info)
	mux.HandleFunc("/api/publish", h.publish)
	mux.HandleFunc("/api/whatsapp/status", h.waStatus)
	mux.HandleFunc("/api/whatsapp/connect", h.waConnect)
	mux.HandleFunc("/api/whatsapp/pairing-code", h.waPairing)
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
		"health":  "/health",
		"publish": "POST /api/publish",
		"connect": "POST /api/whatsapp/connect",
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
	provider := whatsapp.ActiveProvider()
	connected := false
	if provider == "cloud" {
		connected = h.Cloud.Enabled()
	} else if provider == "openwa" && h.WA.SessionID != "" {
		if st, err := h.WA.Status(""); err == nil {
			s, _ := st["status"].(string)
			s = strings.ToLower(s)
			connected = s == "ready" || s == "authenticated" || s == "connected"
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":               true,
		"whatsapp_provider": provider,
		"openwa_url":       os.Getenv("OPENWA_URL"),
		"openwa_session":   h.WA.SessionID != "",
		"openwa_connected": connected && provider == "openwa",
		"cloud_enabled":    h.Cloud.Enabled(),
		"cloud_connected":  connected && provider == "cloud",
		"pairing_code":     provider == "openwa", // código de 8 chars sin QR
		"cf_workers":       os.Getenv("CLOUDFLARE_WORKERS_URL"),
	})
}

func (h *Handlers) waStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	provider := whatsapp.ActiveProvider()
	if provider == "cloud" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "provider": "cloud", "connected": h.Cloud.Enabled(),
			"hint": "Cloud API: sin QR. Usa el número de negocio de Meta.",
		})
		return
	}
	if !h.WA.Enabled() {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "provider": "none", "connected": false,
			"hint": "Configura OPENWA_* o WHATSAPP_CLOUD_* en el servidor",
		})
		return
	}
	st, err := h.WA.Status("")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "provider": "openwa", "connected": false, "error": err.Error(),
		})
		return
	}
	s, _ := st["status"].(string)
	s = strings.ToLower(s)
	connected := s == "ready" || s == "authenticated" || s == "connected"
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "provider": "openwa", "connected": connected, "status": s,
		"sessionId": h.WA.SessionID, "raw": st,
	})
}

// POST { "name": "ventasplus" } — crea/inicia sesión OpenWA
func (h *Handlers) waConnect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	if !h.WA.Enabled() {
		w.WriteHeader(503)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "error": "OpenWA no configurado (OPENWA_URL / OPENWA_API_KEY)",
		})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	id, created, err := h.WA.EnsureSession(req.Name)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": created})
		return
	}
	started, _ := h.WA.StartSession(id)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "sessionId": id, "started": started,
		"next": "POST /api/whatsapp/pairing-code con phoneNumber (recomendado) o GET /api/whatsapp/qr",
	})
}

// POST { "phoneNumber": "56912345678", "sessionId": "optional" }
func (h *Handlers) waPairing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	if !h.WA.Enabled() {
		w.WriteHeader(503)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "OpenWA no configurado"})
		return
	}
	var req struct {
		PhoneNumber string `json:"phoneNumber"`
		SessionID   string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PhoneNumber == "" {
		http.Error(w, `{"ok":false,"error":"phoneNumber required"}`, 400)
		return
	}
	sid := req.SessionID
	if sid == "" {
		sid = "ventasplus"
	}
	// Sesión limpia evita códigos inválidos / "no se pueden vincular"
	_ = h.WA.ResetSession(sid)
	_, _, err := h.WA.EnsureSession(sid)
	if err != nil {
		// EnsureSession puede fallar si ya existe — seguimos
		_ = err
	}
	h.WA.SessionID = sid
	out, err := h.WA.RequestPairingCode(sid, req.PhoneNumber)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": out, "sessionId": sid})
		return
	}
	code, _ := out["pairingCode"].(string)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "pairingCode": code, "sessionId": sid, "raw": out,
		"instructions": "WhatsApp → Dispositivos vinculados → Vincular → Vincular con número → escribe este código",
	})
}

func (h *Handlers) waQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !h.WA.Enabled() {
		w.WriteHeader(503)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "OpenWA no configurado"})
		return
	}
	sid := h.WA.SessionID
	if sid == "" {
		var err error
		sid, _, err = h.WA.EnsureSession("ventasplus")
		if err != nil {
			w.WriteHeader(502)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		_, _ = h.WA.StartSession(sid)
		time.Sleep(1200 * time.Millisecond)
	}
	out, err := h.WA.GetQR(sid)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": out})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) waGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	out, err := h.WA.ListGroups()
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
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
		Message: "Publicación procesada",
		Total:   len(req.Products),
	}
	provider := whatsapp.ActiveProvider()

	for _, p := range req.Products {
		caption := formatCaption(p)
		for _, dest := range req.Destinations.WhatsApp {
			var wr map[string]interface{}
			var err error
			chat := whatsapp.NormalizeChatID(dest)
			img := p.ImageURL
			if img == "" {
				img = p.ImageBase64
			}

			switch provider {
			case "cloud":
				// Cloud API: imagen por URL pública; si solo hay base64, enviamos texto
				if p.ImageURL != "" {
					wr, err = h.Cloud.SendImageLink(chat, caption, p.ImageURL)
				} else {
					wr, err = h.Cloud.SendText(chat, caption)
					if wr == nil {
						wr = map[string]interface{}{}
					}
					wr["note"] = "Cloud API envió texto; sube la imagen a URL pública para foto automática"
				}
			case "openwa":
				if img != "" {
					wr, err = h.WA.SendImage(chat, caption, img)
				} else {
					wr, err = h.WA.SendText(chat, caption)
				}
			default:
				wr = map[string]interface{}{"ok": false, "error": "WhatsApp no configurado en servidor"}
				err = nil
			}
			if err != nil {
				if wr == nil {
					wr = map[string]interface{}{}
				}
				wr["ok"] = false
				wr["error"] = err.Error()
			}
			wr["provider"] = provider
			wr["chatId"] = chat
			result.WhatsApp = append(result.WhatsApp, wr)
		}
	}

	persist, err := h.CF.SavePublication(map[string]interface{}{
		"agentId": req.AgentID, "products": req.Products, "destinations": req.Destinations,
		"timestamp": req.Timestamp, "total": req.Total, "whatsapp": result.WhatsApp, "provider": provider,
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
		b.WriteString(p.Price)
		b.WriteString("\n")
	}
	if p.Description != "" {
		b.WriteString(p.Description)
	}
	return strings.TrimSpace(b.String())
}
