package whatsapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var sessionSafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// OpenWA / wa-bridge REST client
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func NewFromEnv() *Client {
	base := os.Getenv("OPENWA_URL")
	if base == "" {
		base = "http://localhost:2785"
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		APIKey:  os.Getenv("OPENWA_API_KEY"),
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c.BaseURL != "" && c.APIKey != ""
}

// SanitizeSessionID convierte un device id en id de sesión seguro.
func SanitizeSessionID(id string) string {
	id = strings.TrimSpace(id)
	id = sessionSafe.ReplaceAllString(id, "")
	if len(id) > 64 {
		id = id[:64]
	}
	if id == "" {
		return ""
	}
	return id
}

func (c *Client) do(method, path string, body any) (int, map[string]interface{}, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]interface{}{}
	}
	return res.StatusCode, out, raw, nil
}

func (c *Client) EnsureSession(name string) (string, map[string]interface{}, error) {
	name = SanitizeSessionID(name)
	if name == "" {
		return "", nil, fmt.Errorf("session id vacío")
	}
	code, out, raw, err := c.do(http.MethodPost, "/api/sessions", map[string]string{"name": name})
	if err != nil {
		return "", nil, err
	}
	// 201 or 200 ok
	if code >= 300 {
		// puede existir: GET
		code2, out2, _, err2 := c.do(http.MethodGet, "/api/sessions/"+name, nil)
		if err2 == nil && code2 < 300 {
			return name, out2, nil
		}
		return "", out, fmt.Errorf("create session HTTP %d: %s", code, string(raw))
	}
	id, _ := out["id"].(string)
	if id == "" {
		id = name
	}
	return id, out, nil
}

func (c *Client) StartSession(sessionID string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	code, out, raw, err := c.do(http.MethodPost, "/api/sessions/"+sessionID+"/start", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if code >= 300 && code != 400 {
		return out, fmt.Errorf("start HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) Status(sessionID string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	if sessionID == "" {
		return map[string]interface{}{"status": "none", "connected": false}, nil
	}
	code, out, raw, err := c.do(http.MethodGet, "/api/sessions/"+sessionID, nil)
	if err != nil {
		return nil, err
	}
	if code == 404 {
		return map[string]interface{}{"status": "not_found", "connected": false}, nil
	}
	if code >= 300 {
		return out, fmt.Errorf("status HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) RequestPairingCode(sessionID, phoneNumber string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	phoneNumber = digitsOnly(phoneNumber)
	code, out, raw, err := c.do(http.MethodPost, "/api/sessions/"+sessionID+"/pairing-code", map[string]string{
		"phoneNumber": phoneNumber,
	})
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("pairing-code HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) GetQR(sessionID string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	code, out, raw, err := c.do(http.MethodGet, "/api/sessions/"+sessionID+"/qr", nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("qr HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) ResetSession(sessionID string) error {
	sessionID = SanitizeSessionID(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id vacío")
	}
	code, _, raw, err := c.do(http.MethodPost, "/api/sessions/"+sessionID+"/reset", map[string]interface{}{})
	if err != nil {
		return err
	}
	if code >= 300 {
		return fmt.Errorf("reset HTTP %d: %s", code, string(raw))
	}
	return nil
}

func (c *Client) SendText(sessionID, chatID, text string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	path := fmt.Sprintf("/api/sessions/%s/messages/send-text", sessionID)
	code, out, raw, err := c.do(http.MethodPost, path, map[string]string{"chatId": chatID, "text": text})
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		out["ok"] = false
		return out, fmt.Errorf("send-text HTTP %d: %s", code, string(raw))
	}
	out["ok"] = true
	return out, nil
}

func (c *Client) SendImage(sessionID, chatID, caption, imageDataURL string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	path := fmt.Sprintf("/api/sessions/%s/messages/send-image", sessionID)
	body := map[string]interface{}{
		"chatId":  chatID,
		"caption": caption,
	}
	if strings.HasPrefix(imageDataURL, "http") {
		body["image"] = map[string]string{"url": imageDataURL}
	} else {
		img := imageDataURL
		if i := strings.Index(img, ","); i >= 0 && strings.Contains(img[:i], "base64") {
			img = img[i+1:]
		}
		body["image"] = map[string]string{"base64": img}
	}
	code, out, raw, err := c.do(http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		out["ok"] = false
		return out, fmt.Errorf("send-image HTTP %d: %s", code, string(raw))
	}
	out["ok"] = true
	return out, nil
}

func (c *Client) ListGroups(sessionID string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	code, out, raw, err := c.do(http.MethodGet, "/api/sessions/"+sessionID+"/groups", nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("groups HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) Destinations(sessionID string) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	code, out, raw, err := c.do(http.MethodGet, "/api/sessions/"+sessionID+"/destinations", nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("destinations HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) ChatMedia(sessionID, jid string, limit int) (map[string]interface{}, error) {
	sessionID = SanitizeSessionID(sessionID)
	if limit <= 0 {
		limit = 30
	}
	q := url.Values{}
	q.Set("jid", jid)
	q.Set("limit", strconv.Itoa(limit))
	path := "/api/sessions/" + sessionID + "/media?" + q.Encode()
	code, out, raw, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("media HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func NormalizeChatID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if strings.Contains(s, "@") {
		return s
	}
	d := digitsOnly(s)
	if d == "" {
		return s
	}
	return d + "@c.us"
}

// IsConnected interpreta el status del bridge.
func IsConnected(st map[string]interface{}) bool {
	if st == nil {
		return false
	}
	if c, ok := st["connected"].(bool); ok {
		return c
	}
	s, _ := st["status"].(string)
	s = strings.ToLower(s)
	return s == "ready" || s == "authenticated" || s == "connected"
}
