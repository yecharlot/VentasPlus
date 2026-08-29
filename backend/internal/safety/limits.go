package safety

import (
	"os"
	"strconv"
	"sync"
	"time"
)

// Límites orientados a reducir riesgo de restricción de WhatsApp.
// Todo envío debe ser iniciado por el usuario; no hay bots de respuesta automática.

const (
	DefaultMaxDestinations = 5
	DefaultMinDelayMs      = 3500
	DefaultMaxPerHour      = 25
	DefaultMaxPerDay       = 80
)

type Limiter struct {
	mu       sync.Mutex
	events   []time.Time
	maxHour  int
	maxDay   int
	minDelay time.Duration
	maxDest  int
	lastSend time.Time
}

func NewLimiterFromEnv() *Limiter {
	return &Limiter{
		maxHour:  envInt("WA_MAX_SENDS_PER_HOUR", DefaultMaxPerHour),
		maxDay:   envInt("WA_MAX_SENDS_PER_DAY", DefaultMaxPerDay),
		minDelay: time.Duration(envInt("WA_MIN_DELAY_MS", DefaultMinDelayMs)) * time.Millisecond,
		maxDest:  envInt("WA_MAX_DESTINATIONS", DefaultMaxDestinations),
	}
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func (l *Limiter) MaxDestinations() int { return l.maxDest }

func (l *Limiter) CapDestinations(in []string) []string {
	if len(in) <= l.maxDest {
		return in
	}
	return in[:l.maxDest]
}

// AllowSend bloquea si hay que esperar; devuelve error si se superó la cuota.
func (l *Limiter) AllowSend() (wait time.Duration, errMsg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// limpiar > 24h
	cutDay := now.Add(-24 * time.Hour)
	cutHour := now.Add(-1 * time.Hour)
	fresh := l.events[:0]
	hourCount := 0
	for _, t := range l.events {
		if t.After(cutDay) {
			fresh = append(fresh, t)
			if t.After(cutHour) {
				hourCount++
			}
		}
	}
	l.events = fresh
	if hourCount >= l.maxHour {
		return 0, "Límite horario de envíos alcanzado. Espera un rato para cuidar la cuenta."
	}
	if len(l.events) >= l.maxDay {
		return 0, "Límite diario de envíos alcanzado. Continúa mañana."
	}
	if !l.lastSend.IsZero() {
		elapsed := now.Sub(l.lastSend)
		if elapsed < l.minDelay {
			return l.minDelay - elapsed, ""
		}
	}
	return 0, ""
}

func (l *Limiter) RecordSend() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, time.Now())
	l.lastSend = time.Now()
}

func (l *Limiter) Stats() map[string]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	hour, day := 0, 0
	for _, t := range l.events {
		if t.After(now.Add(-24 * time.Hour)) {
			day++
		}
		if t.After(now.Add(-1 * time.Hour)) {
			hour++
		}
	}
	return map[string]interface{}{
		"sendsLastHour":    hour,
		"sendsLastDay":     day,
		"maxPerHour":       l.maxHour,
		"maxPerDay":        l.maxDay,
		"minDelayMs":       int(l.minDelay / time.Millisecond),
		"maxDestinations":  l.maxDest,
	}
}
