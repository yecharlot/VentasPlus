package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yecharlot/VentasPlus/backend/internal/cloudflare"
	"github.com/yecharlot/VentasPlus/backend/internal/publish"
	"github.com/yecharlot/VentasPlus/backend/internal/safety"
	"github.com/yecharlot/VentasPlus/backend/internal/templates"
	"github.com/yecharlot/VentasPlus/backend/internal/whatsapp"
)

type Handlers struct {
	CF    *cloudflare.Client
	WA    *whatsapp.Client
	Cloud *whatsapp.CloudClient
	Lim   *safety.Limiter
	Tpl   *templates.Store
}

func New() *Handlers {
	return &Handlers{
		CF:    cloudflare.NewFromEnv(),
		WA:    whatsapp.NewFromEnv(),
		Cloud: whatsapp.NewCloudFromEnv(),
		Lim:   safety.NewLimiterFromEnv(),
		Tpl:   templates.NewStore(),
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
	mux.HandleFunc("/api/whatsapp/destinations", h.waDestinations)
	mux.HandleFunc("/api/whatsapp/media", h.waMedia)
	mux.HandleFunc("/api/whatsapp/chats", h.waChats)
	mux.HandleFunc("/api/whatsapp/messages", h.waMessages)
	mux.HandleFunc("/api/whatsapp/send", h.waSend)
	mux.HandleFunc("/api/whatsapp/status-post", h.waStatusPost)
	mux.HandleFunc("/api/whatsapp/limits", h.waLimits)
	mux.HandleFunc("/api/whatsapp/unlink", h.waUnlink)
	mux.HandleFunc("/api/templates", h.templates)
	mux.HandleFunc("/api/publications", h.publications)
	mux.HandleFunc("/", h.root)
}

// sessionFromRequest: cada teléfono/navegador trae su propio id (X-Device-Id).
// Nunca se comparte la sesión "ventasplus" global entre dispositivos.
func sessionFromRequest(r *http.Request) string {
	id := r.Header.Get("X-Device-Id")
	if id == "" {
		id = r.URL.Query().Get("deviceId")
	}
	if id == "" {
		id = r.Header.Get("X-Agent-Id")
	}
	return whatsapp.SanitizeSessionID(id)
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
	provider := whatsapp.ActiveProvider()
	sid := sessionFromRequest(r)
	connected := false
	if provider == "cloud" {
		connected = h.Cloud.Enabled()
	} else if provider == "openwa" && sid != "" {
		if st, err := h.WA.Status(sid); err == nil {
			connected = whatsapp.IsConnected(st)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":                true,
		"whatsapp_provider": provider,
		"openwa_url":        os.Getenv("OPENWA_URL"),
		"device_session":    sid,
		"openwa_session":    sid != "",
		"openwa_connected":  connected && provider == "openwa",
		"cloud_enabled":     h.Cloud.Enabled(),
		"cloud_connected":   connected && provider == "cloud",
		"pairing_code":      provider == "openwa",
		"cf_workers":        os.Getenv("CLOUDFLARE_WORKERS_URL"),
		"safety":            h.Lim.Stats(),
		"features": []string{
			"per-device-session", "pairing-code", "destinations", "group-media",
			"multi-dest", "templates", "history", "rate-limits",
		},
	})
}

func (h *Handlers) waLimits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "limits": h.Lim.Stats()})
}

func (h *Handlers) waStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	provider := whatsapp.ActiveProvider()
	sid := sessionFromRequest(r)
	if provider == "cloud" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "provider": "cloud", "connected": h.Cloud.Enabled(), "sessionId": sid,
		})
		return
	}
	if !h.WA.Enabled() {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "provider": "none", "connected": false, "sessionId": sid,
		})
		return
	}
	if sid == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "provider": "openwa", "connected": false, "sessionId": "",
			"hint": "Falta X-Device-Id: cada teléfono debe tener su propio id",
		})
		return
	}
	st, err := h.WA.Status(sid)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "provider": "openwa", "connected": false, "sessionId": sid, "error": err.Error(),
		})
		return
	}
	connected := whatsapp.IsConnected(st)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "provider": "openwa", "connected": connected,
		"status": st["status"], "sessionId": sid, "raw": st,
	})
}

func (h *Handlers) waConnect(w http.ResponseWriter, r *http.Request) {
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
	sid := sessionFromRequest(r)
	if sid == "" {
		http.Error(w, `{"ok":false,"error":"X-Device-Id required"}`, 400)
		return
	}
	id, created, err := h.WA.EnsureSession(sid)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": created})
		return
	}
	started, _ := h.WA.StartSession(id)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "sessionId": id, "started": started})
}

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
	sid := sessionFromRequest(r)
	var req struct {
		PhoneNumber string `json:"phoneNumber"`
		SessionID   string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PhoneNumber == "" {
		http.Error(w, `{"ok":false,"error":"phoneNumber required"}`, 400)
		return
	}
	if req.SessionID != "" {
		sid = whatsapp.SanitizeSessionID(req.SessionID)
	}
	if sid == "" {
		http.Error(w, `{"ok":false,"error":"X-Device-Id required"}`, 400)
		return
	}
	_ = h.WA.ResetSession(sid)
	out, err := h.WA.RequestPairingCode(sid, req.PhoneNumber)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": out, "sessionId": sid})
		return
	}
	code, _ := out["pairingCode"].(string)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "pairingCode": code, "sessionId": sid, "raw": out,
		"instructions": "WhatsApp → Dispositivos vinculados → Vincular con número → código",
	})
}

func (h *Handlers) waUnlink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	sid := sessionFromRequest(r)
	if sid == "" {
		http.Error(w, `{"ok":false,"error":"X-Device-Id required"}`, 400)
		return
	}
	if err := h.WA.ResetSession(sid); err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "sessionId": sid})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "sessionId": sid, "status": "unlinked"})
}

func (h *Handlers) waQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := sessionFromRequest(r)
	if sid == "" {
		http.Error(w, `{"ok":false,"error":"X-Device-Id required"}`, 400)
		return
	}
	out, err := h.WA.GetQR(sid)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) waGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := sessionFromRequest(r)
	out, err := h.WA.ListGroups(sid)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) waDestinations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := sessionFromRequest(r)
	if sid == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "X-Device-Id required"})
		return
	}
	out, err := h.WA.Destinations(sid)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": out})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}


func (h *Handlers) waChats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := sessionFromRequest(r)
	if sid == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "X-Device-Id required"})
		return
	}
	out, err := h.WA.Chats(sid)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) waMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := sessionFromRequest(r)
	jid := r.URL.Query().Get("jid")
	if sid == "" || jid == "" {
		http.Error(w, `{"ok":false,"error":"device and jid required"}`, 400)
		return
	}
	out, err := h.WA.Messages(sid, jid, 50, 0)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) waSend(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	sid := sessionFromRequest(r)
	var req struct {
		ChatID       string `json:"chatId"`
		Text         string `json:"text"`
		ImageBase64  string `json:"imageBase64"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ChatID == "" {
		http.Error(w, `{"ok":false,"error":"chatId required"}`, 400)
		return
	}
	// límites silenciosos
	wait, msg := h.Lim.AllowSend()
	if msg != "" {
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": msg})
		return
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	out, err := h.WA.SendChat(sid, req.ChatID, req.Text, req.ImageBase64)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	h.Lim.RecordSend()
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) waStatusPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	sid := sessionFromRequest(r)
	var req struct {
		Text        string `json:"text"`
		ImageBase64 string `json:"imageBase64"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	wait, msg := h.Lim.AllowSend()
	if msg != "" {
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": msg})
		return
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	out, err := h.WA.PostStatus(sid, req.Text, req.ImageBase64)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	h.Lim.RecordSend()
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) waMedia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sid := sessionFromRequest(r)
	jid := r.URL.Query().Get("jid")
	if jid == "" {
		http.Error(w, `{"ok":false,"error":"jid required"}`, 400)
		return
	}
	out, err := h.WA.ChatMedia(sid, jid, 30)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error(), "data": out})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) templates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "templates": h.Tpl.List()})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
			http.Error(w, "invalid", 400)
			return
		}
		if req.Name == "" {
			req.Name = "Plantilla"
		}
		t := h.Tpl.Add(req.Name, req.Body)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "template": t})
	default:
		http.Error(w, "method", 405)
	}
}

func (h *Handlers) publications(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	base := strings.TrimRight(os.Getenv("CLOUDFLARE_WORKERS_URL"), "/")
	if base == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "items": []interface{}{}})
		return
	}
	req, err := http.NewRequest(http.MethodGet, base+"/publications?limit=30", nil)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	defer res.Body.Close()
	var out interface{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "data": out})
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
	sid := sessionFromRequest(r)
	if req.AgentID == "" {
		req.AgentID = sid
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

	req.Destinations.WhatsApp = h.Lim.CapDestinations(req.Destinations.WhatsApp)

	result := publish.Result{
		Success: true,
		Message: "Publicación procesada",
		Total:   len(req.Products),
	}
	provider := whatsapp.ActiveProvider()
	var skipped string

	for _, p := range req.Products {
		caption := formatCaption(p)
		for i, dest := range req.Destinations.WhatsApp {
			wait, msg := h.Lim.AllowSend()
			if msg != "" {
				skipped = msg
				result.WhatsApp = append(result.WhatsApp, map[string]interface{}{
					"ok": false, "error": msg, "chatId": dest,
				})
				break
			}
			if wait > 0 {
				time.Sleep(wait)
			}

			var wr map[string]interface{}
			var err error
			chat := whatsapp.NormalizeChatID(dest)
			img := p.ImageURL
			if img == "" {
				img = p.ImageBase64
			}

			switch provider {
			case "cloud":
				if p.ImageURL != "" {
					wr, err = h.Cloud.SendImageLink(chat, caption, p.ImageURL)
				} else {
					wr, err = h.Cloud.SendText(chat, caption)
					if wr == nil {
						wr = map[string]interface{}{}
					}
				}
			case "openwa":
				if sid == "" {
					wr = map[string]interface{}{"ok": false, "error": "X-Device-Id required"}
				} else if img != "" {
					wr, err = h.WA.SendImage(sid, chat, caption, img)
				} else {
					wr, err = h.WA.SendText(sid, chat, caption)
				}
			default:
				wr = map[string]interface{}{"ok": false, "error": "WhatsApp no configurado"}
			}
			if err != nil {
				if wr == nil {
					wr = map[string]interface{}{}
				}
				wr["ok"] = false
				wr["error"] = err.Error()
			}
			if wr == nil {
				wr = map[string]interface{}{}
			}
			wr["provider"] = provider
			wr["chatId"] = chat
			wr["sessionId"] = sid
			result.WhatsApp = append(result.WhatsApp, wr)
			if ok, _ := wr["ok"].(bool); ok {
				h.Lim.RecordSend()
			}
			if i < len(req.Destinations.WhatsApp)-1 {
				time.Sleep(time.Duration(safety.DefaultMinDelayMs) * time.Millisecond)
			}
		}
	}

	if skipped != "" {
		result.Message = skipped
	}

	persist, err := h.CF.SavePublication(map[string]interface{}{
		"agentId": req.AgentID, "sessionId": sid, "products": req.Products, "destinations": req.Destinations,
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
