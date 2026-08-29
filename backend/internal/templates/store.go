package templates

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Template struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store struct {
	mu   sync.RWMutex
	list []Template
	path string
}

func NewStore() *Store {
	s := &Store{path: os.Getenv("TEMPLATES_PATH")}
	if s.path == "" {
		s.path = "data/templates.json"
	}
	_ = s.load()
	if len(s.list) == 0 {
		s.list = defaultTemplates()
	}
	return s
}

func defaultTemplates() []Template {
	now := time.Now().UTC()
	return []Template{
		{ID: "t1", Name: "Oferta del día", Body: "🔥 Oferta del día\n{{nombre}}\n{{precio}}\n{{detalle}}\nEscribe para pedir.", CreatedAt: now},
		{ID: "t2", Name: "Últimas unidades", Body: "Últimas unidades\n{{nombre}} — {{precio}}\n{{detalle}}", CreatedAt: now},
		{ID: "t3", Name: "Envío incluido", Body: "{{nombre}}\n{{precio}} (envío incluido)\n{{detalle}}", CreatedAt: now},
	}
}

func (s *Store) List() []Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Template, len(s.list))
	copy(out, s.list)
	return out
}

func (s *Store) Add(name, body string) Template {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := Template{
		ID:        "t" + time.Now().Format("150405.000"),
		Name:      name,
		Body:      body,
		CreatedAt: time.Now().UTC(),
	}
	s.list = append(s.list, t)
	_ = s.save()
	return t
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &s.list)
}

func (s *Store) save() error {
	_ = os.MkdirAll("data", 0o755)
	b, _ := json.MarshalIndent(s.list, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}
