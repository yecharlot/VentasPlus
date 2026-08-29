package whatsapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OpenWA REST client — https://docs.open-wa.org/
type Client struct {
	BaseURL   string
	APIKey    string
	SessionID string
	HTTP      *http.Client
}

func NewFromEnv() *Client {
	base := os.Getenv("OPENWA_URL")
	if base == "" {
		base = "http://localhost:2785"
	}
	return &Client{
		BaseURL:   strings.TrimRight(base, "/"),
		APIKey:    os.Getenv("OPENWA_API_KEY"),
		SessionID: os.Getenv("OPENWA_SESSION_ID"),
		HTTP:      &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c.BaseURL != "" && c.APIKey != ""
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
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]interface{}{}
	}
	return res.StatusCode, out, raw, nil
}

// EnsureSession creates a session if OPENWA_SESSION_ID empty; returns session id.
func (c *Client) EnsureSession(name string) (string, map[string]interface{}, error) {
	if c.SessionID != "" {
		code, out, _, err := c.do(http.MethodGet, "/api/sessions/"+c.SessionID, nil)
		if err == nil && code < 300 {
			return c.SessionID, out, nil
		}
	}
	if name == "" {
		name = "ventasplus"
	}
	code, out, raw, err := c.do(http.MethodPost, "/api/sessions", map[string]string{"name": name})
	if err != nil {
		return "", nil, err
	}
	if code >= 300 {
		return "", out, fmt.Errorf("create session HTTP %d: %s", code, string(raw))
	}
	id, _ := out["id"].(string)
	if id == "" {
		if d, ok := out["data"].(map[string]interface{}); ok {
			id, _ = d["id"].(string)
		}
	}
	if id == "" {
		return "", out, fmt.Errorf("no session id in response")
	}
	c.SessionID = id
	return id, out, nil
}

func (c *Client) StartSession(sessionID string) (map[string]interface{}, error) {
	if sessionID == "" {
		sessionID = c.SessionID
	}
	code, out, raw, err := c.do(http.MethodPost, "/api/sessions/"+sessionID+"/start", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if code >= 300 && code != 400 { // 400 may mean already started
		return out, fmt.Errorf("start HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) Status(sessionID string) (map[string]interface{}, error) {
	if sessionID == "" {
		sessionID = c.SessionID
	}
	code, out, raw, err := c.do(http.MethodGet, "/api/sessions/"+sessionID, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("status HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

// RequestPairingCode — alternativa al QR: código de 8 caracteres.
// phoneNumber: solo dígitos con código país (ej. 56912345678).
func (c *Client) RequestPairingCode(sessionID, phoneNumber string) (map[string]interface{}, error) {
	if sessionID == "" {
		sessionID = c.SessionID
	}
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
	if sessionID == "" {
		sessionID = c.SessionID
	}
	code, out, raw, err := c.do(http.MethodGet, "/api/sessions/"+sessionID+"/qr", nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("qr HTTP %d: %s", code, string(raw))
	}
	return out, nil
}

func (c *Client) SendText(chatID, text string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/sessions/%s/messages/send-text", c.SessionID)
	code, out, raw, err := c.do(http.MethodPost, path, map[string]string{"chatId": chatID, "text": text})
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		out["ok"] = false
		return out, fmt.Errorf("openwa HTTP %d: %s", code, string(raw))
	}
	out["ok"] = true
	return out, nil
}

func (c *Client) SendImage(chatID, caption, imageDataURL string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/sessions/%s/messages/send-image", c.SessionID)
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
		return out, fmt.Errorf("openwa HTTP %d: %s", code, string(raw))
	}
	out["ok"] = true
	return out, nil
}

func (c *Client) ListGroups() (map[string]interface{}, error) {
	if c.SessionID == "" {
		return nil, fmt.Errorf("OPENWA_SESSION_ID not set")
	}
	code, out, raw, err := c.do(http.MethodGet, "/api/sessions/"+c.SessionID+"/groups", nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return out, fmt.Errorf("groups HTTP %d: %s", code, string(raw))
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


func (c *Client) ResetSession(sessionID string) error {
	if sessionID == "" {
		sessionID = c.SessionID
	}
	if sessionID == "" {
		sessionID = "ventasplus"
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

func (c *Client) Destinations(sessionID string) (map[string]interface{}, error) {
	if sessionID == "" {
		sessionID = c.SessionID
	}
	if sessionID == "" {
		sessionID = "ventasplus"
	}
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
	if sessionID == "" {
		sessionID = c.SessionID
	}
	if sessionID == "" {
		sessionID = "ventasplus"
	}
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
