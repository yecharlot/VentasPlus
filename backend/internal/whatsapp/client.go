package whatsapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

func (c *Client) SendText(chatID, text string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/sessions/%s/messages/send-text", c.SessionID)
	return c.post(path, map[string]string{"chatId": chatID, "text": text})
}

func (c *Client) SendImage(chatID, caption, imageDataURL string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/sessions/%s/messages/send-image", c.SessionID)
	// OpenWA accepts image as url object or base64 depending on version — send both-friendly payload
	body := map[string]interface{}{
		"chatId":  chatID,
		"caption": caption,
	}
	if strings.HasPrefix(imageDataURL, "http") {
		body["image"] = map[string]string{"url": imageDataURL}
	} else {
		// strip data URL prefix if present
		img := imageDataURL
		if i := strings.Index(img, ","); i >= 0 && strings.Contains(img[:i], "base64") {
			img = img[i+1:]
		}
		body["image"] = map[string]string{"base64": img}
	}
	return c.post(path, body)
}

func (c *Client) ListGroups() (map[string]interface{}, error) {
	if c.SessionID == "" {
		return nil, fmt.Errorf("OPENWA_SESSION_ID not set")
	}
	path := fmt.Sprintf("/api/sessions/%s/groups", c.SessionID)
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	var out map[string]interface{}
	if json.Unmarshal(raw, &out) != nil {
		var arr []interface{}
		if json.Unmarshal(raw, &arr) == nil {
			return map[string]interface{}{"ok": true, "groups": arr}, nil
		}
		return map[string]interface{}{"raw": string(raw)}, nil
	}
	return out, nil
}

func (c *Client) post(path string, body any) (map[string]interface{}, error) {
	if c.SessionID == "" {
		return map[string]interface{}{"ok": false, "error": "OPENWA_SESSION_ID not set"}, fmt.Errorf("no session")
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]interface{}{"body": string(raw)}
	}
	if res.StatusCode >= 300 {
		out["ok"] = false
		return out, fmt.Errorf("openwa HTTP %d: %s", res.StatusCode, string(raw))
	}
	out["ok"] = true
	return out, nil
}
