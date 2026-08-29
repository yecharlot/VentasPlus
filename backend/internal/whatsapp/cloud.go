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

// CloudClient — WhatsApp Cloud API oficial (Meta).
// No usa QR: el número de negocio se verifica una vez en Meta Business.
// Ideal cuando todos los envíos salen del número de la empresa.
type CloudClient struct {
	Token         string
	PhoneNumberID string
	APIVersion    string
	HTTP          *http.Client
}

func NewCloudFromEnv() *CloudClient {
	v := os.Getenv("WHATSAPP_CLOUD_API_VERSION")
	if v == "" {
		v = "v19.0"
	}
	return &CloudClient{
		Token:         os.Getenv("WHATSAPP_CLOUD_TOKEN"),
		PhoneNumberID: os.Getenv("WHATSAPP_CLOUD_PHONE_NUMBER_ID"),
		APIVersion:    v,
		HTTP:          &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *CloudClient) Enabled() bool {
	return c.Token != "" && c.PhoneNumberID != ""
}

func (c *CloudClient) SendText(to, body string) (map[string]interface{}, error) {
	to = digitsOnly(to)
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": body},
	}
	return c.post(payload)
}

// SendImageLink — Cloud API prefiere URL pública de la imagen.
func (c *CloudClient) SendImageLink(to, caption, imageURL string) (map[string]interface{}, error) {
	to = digitsOnly(to)
	img := map[string]string{"link": imageURL}
	if caption != "" {
		img["caption"] = caption
	}
	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "image",
		"image":             img,
	}
	return c.post(payload)
}

func (c *CloudClient) post(payload map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("https://graph.facebook.com/%s/%s/messages", c.APIVersion, c.PhoneNumberID)
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
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
		return out, fmt.Errorf("cloud API HTTP %d: %s", res.StatusCode, string(raw))
	}
	out["ok"] = true
	return out, nil
}

// Provider returns "cloud" | "openwa" | "none"
func ActiveProvider() string {
	if strings.EqualFold(os.Getenv("WHATSAPP_PROVIDER"), "cloud") || NewCloudFromEnv().Enabled() {
		if NewCloudFromEnv().Enabled() {
			return "cloud"
		}
	}
	if NewFromEnv().Enabled() {
		return "openwa"
	}
	return "none"
}
