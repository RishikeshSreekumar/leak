package fx

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newServer returns a fake exchangerate.host that counts hits.
func newServer(t *testing.T, rate float64, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		to := r.URL.Query().Get("symbols")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"rates":{"` + to + `":` + strconv.FormatFloat(rate, 'f', -1, 64) + `}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSameCurrencyIsIdentity(t *testing.T) {
	p := &HTTPProvider{}
	r, est, err := p.Rate("INR", "INR", model.NewDate(2026, 7, 14))
	require.NoError(t, err)
	assert.Equal(t, 1.0, r)
	assert.False(t, est)
}

func TestHistoricalFetchThenCacheHit(t *testing.T) {
	var hits int32
	srv := newServer(t, 86.73, &hits)
	p := NewHTTP(filepath.Join(t.TempDir(), "fx.yaml"))
	p.BaseURL = srv.URL

	on := model.NewDate(2026, 7, 14)
	r, est, err := p.Rate("USD", "INR", on)
	require.NoError(t, err)
	assert.InDelta(t, 86.73, r, 1e-6)
	assert.False(t, est)
	assert.EqualValues(t, 1, atomic.LoadInt32(&hits))

	// Second call: served from in-memory cache, no new request.
	r2, _, err := p.Rate("USD", "INR", on)
	require.NoError(t, err)
	assert.InDelta(t, 86.73, r2, 1e-6)
	assert.EqualValues(t, 1, atomic.LoadInt32(&hits), "cache should prevent a second fetch")
}

func TestCachePersistsAcrossInstances(t *testing.T) {
	var hits int32
	srv := newServer(t, 90.0, &hits)
	cache := filepath.Join(t.TempDir(), "fx.yaml")

	p1 := NewHTTP(cache)
	p1.BaseURL = srv.URL
	on := model.NewDate(2026, 6, 1)
	_, _, err := p1.Rate("USD", "INR", on)
	require.NoError(t, err)

	// New provider over same cache file must not hit the network.
	p2 := NewHTTP(cache)
	p2.BaseURL = srv.URL
	r, _, err := p2.Rate("USD", "INR", on)
	require.NoError(t, err)
	assert.InDelta(t, 90.0, r, 1e-6)
	assert.EqualValues(t, 1, atomic.LoadInt32(&hits))
}

func TestStaticProvider(t *testing.T) {
	s := Static{Rates: map[string]float64{"USD:INR": 86.5, "EUR:INR": 94.0}}
	r, est, err := s.Rate("usd", "inr", model.Date{})
	require.NoError(t, err)
	assert.Equal(t, 86.5, r)
	assert.False(t, est)

	_, _, err = s.Rate("GBP", "INR", model.Date{})
	assert.Error(t, err)
}
