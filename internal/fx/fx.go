// Package fx resolves currency exchange rates, preferring stored historical
// rates so past spending stays stable, and caching lookups to avoid the network.
package fx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"gopkg.in/yaml.v3"
)

// Provider resolves the rate to multiply a `from` amount by to get `to`.
// estimated is true when a non-historical (latest) rate was substituted.
type Provider interface {
	Rate(from, to string, on model.Date) (rate float64, estimated bool, err error)
}

// Sourced is optionally implemented by providers that can name themselves, so
// billing records can record which source a rate came from.
type Sourced interface {
	SourceName() string
}

// SourceName returns the provider label (e.g. "frankfurter.dev").
func (p *HTTPProvider) SourceName() string { return p.Source }

// SourceName reports a static provider.
func (Static) SourceName() string { return "static" }

// cacheKey builds the map key for a from/to/date triple.
func cacheKey(from, to, date string) string {
	return strings.ToUpper(from) + ":" + strings.ToUpper(to) + ":" + date
}

// HTTPProvider fetches from an exchangerate.host-compatible API and caches
// results to a YAML file. Same-currency lookups short-circuit to 1.0.
type HTTPProvider struct {
	BaseURL   string // e.g. https://api.exchangerate.host
	Source    string // label stored on records, e.g. "exchangerate.host"
	CachePath string
	Client    *http.Client

	mu    sync.Mutex
	cache map[string]float64
}

// NewHTTP builds an HTTPProvider with sane defaults. It targets frankfurter.dev
// — a free, key-less, ECB-backed API that supports historical lookups by date
// (endpoint shape: /v1/{date|latest}?base=USD&symbols=INR).
func NewHTTP(cachePath string) *HTTPProvider {
	return &HTTPProvider{
		BaseURL:   "https://api.frankfurter.dev/v1",
		Source:    "frankfurter.dev",
		CachePath: cachePath,
		Client:    &http.Client{Timeout: 5 * time.Second},
	}
}

// Rate implements Provider with the fallback chain:
// same-currency → cache → live historical → live latest (estimated).
func (p *HTTPProvider) Rate(from, to string, on model.Date) (float64, bool, error) {
	from, to = strings.ToUpper(from), strings.ToUpper(to)
	if from == to {
		return 1.0, false, nil
	}
	p.loadCache()

	dateStr := ""
	if !on.IsZero() {
		dateStr = on.String()
	}
	// Cache hit (historical dates are immutable, so cache is authoritative).
	if dateStr != "" {
		if r, ok := p.get(cacheKey(from, to, dateStr)); ok {
			return r, false, nil
		}
	}

	// Live historical.
	if dateStr != "" {
		if r, err := p.fetch(from, to, dateStr); err == nil {
			p.put(cacheKey(from, to, dateStr), r)
			return r, false, nil
		}
	}

	// Live latest — flagged estimated.
	if r, ok := p.get(cacheKey(from, to, "latest")); ok {
		return r, true, nil
	}
	r, err := p.fetch(from, to, "latest")
	if err != nil {
		return 0, false, fmt.Errorf("fx: no rate for %s->%s and network unavailable: %w", from, to, err)
	}
	p.put(cacheKey(from, to, "latest"), r)
	return r, true, nil
}

// fetch calls the API for a specific date ("latest" for the current rate).
func (p *HTTPProvider) fetch(from, to, date string) (float64, error) {
	url := fmt.Sprintf("%s/%s?base=%s&symbols=%s", strings.TrimRight(p.BaseURL, "/"), date, from, to)
	resp, err := p.Client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fx: status %d", resp.StatusCode)
	}
	var body struct {
		Success *bool              `json:"success"`
		Rates   map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	r, ok := body.Rates[to]
	if !ok || r == 0 {
		return 0, fmt.Errorf("fx: rate %s->%s missing in response", from, to)
	}
	return r, nil
}

func (p *HTTPProvider) get(key string) (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.cache[key]
	return r, ok
}

func (p *HTTPProvider) put(key string, r float64) {
	p.mu.Lock()
	if p.cache == nil {
		p.cache = map[string]float64{}
	}
	p.cache[key] = r
	p.mu.Unlock()
	p.saveCache()
}

func (p *HTTPProvider) loadCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cache != nil || p.CachePath == "" {
		return
	}
	p.cache = map[string]float64{}
	b, err := os.ReadFile(p.CachePath)
	if err != nil {
		return
	}
	_ = yaml.Unmarshal(b, &p.cache)
}

func (p *HTTPProvider) saveCache() {
	if p.CachePath == "" {
		return
	}
	p.mu.Lock()
	b, err := yaml.Marshal(p.cache)
	p.mu.Unlock()
	if err == nil {
		_ = os.WriteFile(p.CachePath, b, 0o644)
	}
}

// Static is a fixed-rate Provider for tests and offline use.
type Static struct {
	Rates map[string]float64 // key "FROM:TO"
}

// Rate implements Provider from the static table (1.0 for same currency).
func (s Static) Rate(from, to string, _ model.Date) (float64, bool, error) {
	from, to = strings.ToUpper(from), strings.ToUpper(to)
	if from == to {
		return 1.0, false, nil
	}
	if r, ok := s.Rates[from+":"+to]; ok {
		return r, false, nil
	}
	return 0, false, fmt.Errorf("fx: no static rate for %s->%s", from, to)
}
