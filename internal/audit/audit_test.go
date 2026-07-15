package audit

import (
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

func sub(name string, confirmedDaysAgo int, amount float64) model.Subscription {
	return model.Subscription{
		ID: name, Name: name, Amount: amount, Currency: "INR",
		BillingCycle: model.CycleMonthly, Status: model.StatusActive,
		LastConfirmed: model.Date{Time: now.AddDate(0, 0, -confirmedDaysAgo)},
	}
}

func TestZombiesThresholdBoundary(t *testing.T) {
	subs := []model.Subscription{
		sub("just-under", 180, 100), // exactly at threshold -> not a zombie
		sub("just-over", 181, 200),  // one day past -> zombie
		sub("fresh", 5, 300),
	}
	zs := Zombies(subs, now, 180)
	require.Len(t, zs, 1)
	assert.Equal(t, "just-over", zs[0].Sub.Name)
	assert.Equal(t, 181, zs[0].DaysSince)
	assert.InDelta(t, 200, zs[0].MonthlySavings, 1e-9)
}

func TestZombiesSkipsInactive(t *testing.T) {
	s := sub("cancelled", 400, 100)
	s.Status = model.StatusCancelled
	zs := Zombies([]model.Subscription{s}, now, 180)
	assert.Empty(t, zs)
}

func TestZombiesSortedMostStaleFirst(t *testing.T) {
	subs := []model.Subscription{sub("a", 200, 1), sub("b", 400, 1), sub("c", 300, 1)}
	zs := Zombies(subs, now, 180)
	require.Len(t, zs, 3)
	assert.Equal(t, []string{"b", "c", "a"}, []string{zs[0].Sub.Name, zs[1].Sub.Name, zs[2].Sub.Name})
}

func TestZombieNoAnchorIsStale(t *testing.T) {
	s := model.Subscription{ID: "x", Name: "x", Amount: 50, Currency: "INR", Status: model.StatusActive}
	zs := Zombies([]model.Subscription{s}, now, 180)
	require.Len(t, zs, 1)
	assert.Equal(t, 181, zs[0].DaysSince)
}

func TestNeedsReview(t *testing.T) {
	subs := []model.Subscription{sub("stale", 100, 1), sub("fresh", 10, 1)}
	got := NeedsReview(subs, now, 90)
	require.Len(t, got, 1)
	assert.Equal(t, "stale", got[0].Name)
}

func TestSavingsFor(t *testing.T) {
	zs := []Zombie{
		{MonthlySavings: 100}, {MonthlySavings: 50},
	}
	// convert doubles (e.g. fx into default currency)
	s := SavingsFor(zs, func(z Zombie) float64 { return z.MonthlySavings * 2 })
	assert.InDelta(t, 300, s.Monthly, 1e-9)
	assert.InDelta(t, 3600, s.Yearly, 1e-9)
}

func TestSavingsEmpty(t *testing.T) {
	s := SavingsFor(nil, func(Zombie) float64 { return 0 })
	assert.Zero(t, s.Monthly)
	assert.Zero(t, s.Yearly)
}
