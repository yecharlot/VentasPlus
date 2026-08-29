package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewFromEnv() *Client {
	base := os.Getenv("CLOUDFLARE_WORKERS_URL")
	if base == "" {
		base = "https://ventasplus-workers.yecharlot.workers.dev"
	}
	return &Client{
		BaseURL: base,
		Token:   os.Getenv("CLOUDFLARE_API_TOKEN"),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) SavePublication(payload map[string]interface{}) (map[string]interface{}, error) {
	return c.post("/api/publications", payload)
}

func (c *Client) post(path string, body any) (map[string]interface{}, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	if res.StatusCode >= 300 {
		return out, fmt.Errorf("cloudflare HTTP %d: %s", res.StatusCode, string(raw))
	}
	return out, nil
}
