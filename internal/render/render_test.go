package render

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

var now = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run `go test ./internal/render -update` to create golden files")
	assert.Equal(t, string(want), got)
}

func sampleSubs() []model.Subscription {
	return []model.Subscription{
		{ID: "netflix", Name: "Netflix", Amount: 649, Currency: "INR", BillingCycle: model.CycleMonthly,
			Category: "Entertainment", PaymentMethod: "ICICI Amazon Pay", Status: model.StatusActive,
			RenewalDate: model.Date{Time: now.AddDate(0, 0, 6)}, LastConfirmed: model.Date{Time: now.AddDate(0, 0, -10)}},
		{ID: "copilot", Name: "GitHub Copilot", Amount: 10, Currency: "USD", BillingCycle: model.CycleMonthly,
			Category: "Development", PaymentMethod: "ICICI Amazon Pay", Status: model.StatusActive,
			RenewalDate: model.Date{Time: now.AddDate(0, 0, 9)}, LastConfirmed: model.Date{Time: now.AddDate(0, 0, -5)}},
		{ID: "adobe", Name: "Adobe CC", Amount: 1675, Currency: "INR", BillingCycle: model.CycleMonthly,
			Category: "Development", PaymentMethod: "HDFC Millennia", Status: model.StatusActive,
			RenewalDate: model.Date{Time: now.AddDate(0, 0, 20)}, LastConfirmed: model.Date{Time: now.AddDate(0, 0, -200)}},
	}
}

func fxp() fx.Provider { return fx.Static{Rates: map[string]float64{"USD:INR": 86.0}} }

func TestGoldenList(t *testing.T) {
	s := New(false)
	golden(t, "list", s.List(sampleSubs()))
}

func TestGoldenStats(t *testing.T) {
	r := insights.Build(sampleSubs(), model.Profile{DefaultCurrency: "INR"}, fxp(), now)
	golden(t, "stats", New(false).Stats(r))
}

func TestGoldenDue(t *testing.T) {
	golden(t, "due", New(false).Due(sampleSubs(), now, 30))
}

func TestGoldenSweep(t *testing.T) {
	zs := audit.Zombies(sampleSubs(), now, 180)
	sav := audit.SavingsFor(zs, func(z audit.Zombie) float64 { return z.MonthlySavings })
	golden(t, "sweep", New(false).Sweep(zs, "INR", sav))
}

func TestGoldenInsights(t *testing.T) {
	r := insights.Build(sampleSubs(), model.Profile{DefaultCurrency: "INR"}, fxp(), now)
	zs := audit.Zombies(sampleSubs(), now, 180)
	sav := audit.SavingsFor(zs, func(z audit.Zombie) float64 { return z.MonthlySavings })
	golden(t, "insights", New(false).Insights(r, len(zs), sav))
}

func TestColorDisabledIsPlain(t *testing.T) {
	// With color off, styling must not inject ANSI escapes.
	out := New(false).Warn("danger")
	assert.Equal(t, "danger", out)
}

func TestColorEnabledHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	assert.False(t, ColorEnabled(os.Stdout))
}
