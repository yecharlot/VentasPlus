package facebook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Client struct {
	Token string
	HTTP  *http.Client
}

func NewFromEnv() *Client {
	return &Client{
		Token: os.Getenv("FACEBOOK_ACCESS_TOKEN"),
		HTTP:  &http.Client{Timeout: 20 * time.Second},
	}
}

// PublishFeed posts a text (+ optional link) to a page/group feed using Graph API.
func (c *Client) PublishFeed(targetID, message, link string) (map[string]interface{}, error) {
	if c.Token == "" {
		return map[string]interface{}{"ok": false, "error": "FACEBOOK_ACCESS_TOKEN not set"}, fmt.Errorf("no facebook token")
	}
	if targetID == "" {
		return nil, fmt.Errorf("empty target")
	}
	endpoint := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/feed", url.PathEscape(targetID))
	form := url.Values{}
	form.Set("message", message)
	form.Set("access_token", c.Token)
	if link != "" {
		form.Set("link", link)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	if res.StatusCode >= 300 {
		return out, fmt.Errorf("facebook HTTP %d: %s", res.StatusCode, string(raw))
	}
	out["ok"] = true
	out["target"] = targetID
	return out, nil
}

// PublishPhoto posts multipart-less via URL if image URL available.
func (c *Client) PublishPhoto(targetID, caption, imageURL string) (map[string]interface{}, error) {
	if c.Token == "" {
		return map[string]interface{}{"ok": false, "error": "FACEBOOK_ACCESS_TOKEN not set"}, fmt.Errorf("no facebook token")
	}
	endpoint := fmt.Sprintf("https://graph.facebook.com/v19.0/%s/photos", url.PathEscape(targetID))
	body := map[string]string{
		"caption":      caption,
		"url":          imageURL,
		"access_token": c.Token,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	if res.StatusCode >= 300 {
		return out, fmt.Errorf("facebook photo HTTP %d: %s", res.StatusCode, string(raw))
	}
	out["ok"] = true
	return out, nil
}
