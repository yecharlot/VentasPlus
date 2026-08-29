package publish

type Product struct {
	Name        string `json:"name"`
	Price       string `json:"price"`
	Description string `json:"description"`
	ImageBase64 string `json:"imageBase64,omitempty"` // data URL or raw base64
	ImageURL    string `json:"imageUrl,omitempty"`
}

type Destinations struct {
	Facebook []string `json:"facebook"`
	WhatsApp []string `json:"whatsapp"`
}

type Request struct {
	Products     []Product    `json:"products"`
	Total        int          `json:"total"`
	Timestamp    string       `json:"timestamp"`
	Destinations Destinations `json:"destinations"`
	AgentID      string       `json:"agentId"`
	CaptionExtra string       `json:"captionExtra,omitempty"`
}

type Result struct {
	Success      bool                   `json:"success"`
	Message      string                 `json:"message"`
	Total        int                    `json:"total"`
	PublicationID string                `json:"publicationId,omitempty"`
	Facebook     []map[string]interface{} `json:"facebook,omitempty"`
	WhatsApp     []map[string]interface{} `json:"whatsapp,omitempty"`
	PersistError string                 `json:"persistError,omitempty"`
}
