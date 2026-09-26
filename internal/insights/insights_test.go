package insights

import (
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

func profile() model.Profile { return model.Profile{DefaultCurrency: "INR"} }

func fxp() fx.Provider {
	return fx.Static{Rates: map[string]float64{"USD:INR": 86.0, "EUR:INR": 90.0}}
}

func active(name, cat, method, cur string, amt float64, cycle string, renewalIn int) model.Subscription {
	return model.Subscription{
		ID: name, Name: name, Category: cat, PaymentMethod: method,
		Currency: cur, Amount: amt, BillingCycle: cycle, Status: model.StatusActive,
		RenewalDate: model.Date{Time: now.AddDate(0, 0, renewalIn)},
	}
}

func TestBuildTotalsMultiCurrency(t *testing.T) {
	subs := []model.Subscription{
		active("Netflix", "Entertainment", "ICICI", "INR", 649, model.CycleMonthly, 6),
		active("Copilot", "Development", "ICICI", "USD", 10, model.CycleMonthly, 9), // 860 INR
		active("Annual", "Storage", "UPI", "INR", 1200, model.CycleYearly, 20),      // 100 INR/mo
	}
	r := Build(subs, profile(), fxp(), now)
	assert.InDelta(t, 649+860+100, r.MonthlyTotal, 0.01)
	assert.InDelta(t, r.MonthlyTotal*12, r.YearlyTotal, 0.01)
}

func TestByCategoryPercent(t *testing.T) {
	subs := []model.Subscription{
		active("A", "Development", "m", "INR", 300, model.CycleMonthly, 3),
		active("B", "Entertainment", "m", "INR", 100, model.CycleMonthly, 3),
	}
	r := Build(subs, profile(), fxp(), now)
	require.Len(t, r.ByCategory, 2)
	assert.Equal(t, "Development", r.ByCategory[0].Category) // sorted by spend desc
	assert.InDelta(t, 75, r.ByCategory[0].Percent, 0.01)
	assert.InDelta(t, 25, r.ByCategory[1].Percent, 0.01)
}

func TestTopNOrdering(t *testing.T) {
	subs := []model.Subscription{
		active("Cheap", "x", "m", "INR", 100, model.CycleMonthly, 3),
		active("Pricey", "x", "m", "INR", 900, model.CycleMonthly, 3),
		active("Mid", "x", "m", "INR", 500, model.CycleMonthly, 3),
	}
	r := Build(subs, profile(), fxp(), now)
	top := r.TopN(2)
	require.Len(t, top, 2)
	assert.Equal(t, "Pricey", top[0].Name)
	assert.Equal(t, "Mid", top[1].Name)
}

func TestHeatmapBucketing(t *testing.T) {
	subs := []model.Subscription{
		active("wk1", "x", "m", "INR", 1, model.CycleMonthly, 2),  // this week
		active("wk2", "x", "m", "INR", 1, model.CycleMonthly, 8),  // next week
		active("wk4", "x", "m", "INR", 1, model.CycleMonthly, 25), // week 4
		active("far", "x", "m", "INR", 1, model.CycleMonthly, 40), // out of range
	}
	r := Build(subs, profile(), fxp(), now)
	require.Len(t, r.Heatmap, 4)
	assert.Equal(t, 1, r.Heatmap[0].Count)
	assert.Equal(t, 1, r.Heatmap[1].Count)
	assert.Equal(t, 0, r.Heatmap[2].Count)
	assert.Equal(t, 1, r.Heatmap[3].Count)
}

func TestByPaymentMethod(t *testing.T) {
	subs := []model.Subscription{
		active("A", "x", "ICICI", "INR", 200, model.CycleMonthly, 3),
		active("B", "x", "ICICI", "INR", 100, model.CycleMonthly, 3),
		active("C", "x", "UPI", "INR", 50, model.CycleMonthly, 3),
	}
	r := Build(subs, profile(), fxp(), now)
	require.Len(t, r.ByPaymentMethod, 2)
	assert.Equal(t, "ICICI", r.ByPaymentMethod[0].Method)
	assert.Equal(t, 2, r.ByPaymentMethod[0].Count)
	assert.InDelta(t, 300, r.ByPaymentMethod[0].Monthly, 0.01)
}

func TestInactiveExcluded(t *testing.T) {
	s := active("dead", "x", "m", "INR", 999, model.CycleMonthly, 3)
	s.Status = model.StatusCancelled
	r := Build([]model.Subscription{s}, profile(), fxp(), now)
	assert.Zero(t, r.MonthlyTotal)
}

func TestUpcomingRollsForwardAndPrefersTrialEnd(t *testing.T) {
	stale := active("Stale", "x", "m", "INR", 100, model.CycleMonthly, -40) // renewal 40 days ago
	trial := active("Trial", "x", "m", "INR", 500, model.CycleMonthly, 20)
	trial.TrialEnds = model.Date{Time: now.AddDate(0, 0, 5)}
	gone := active("Gone", "x", "m", "INR", 100, model.CycleMonthly, 3)
	gone.Status = model.StatusCancelled

	items := UpcomingWithin([]model.Subscription{stale, trial, gone}, now, 30)
	require.Len(t, items, 2)
	assert.Equal(t, "Trial", items[0].Name)
	assert.True(t, items[0].Trial)
	assert.Equal(t, 5, items[0].Days)
	assert.Equal(t, "Stale", items[1].Name, "a passed renewal rolls to next month instead of vanishing")
	assert.False(t, items[1].Trial)
	assert.Equal(t, now.AddDate(0, 0, -40).AddDate(0, 2, 0).Format("2006-01-02"), items[1].Date.String())
}
