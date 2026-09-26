package detect

import (
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcile(t *testing.T) {
	now := time.Date(2026, time.September, 10, 0, 0, 0, 0, time.UTC)
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	txns := []Txn{
		{Date: day(3, 5), Description: "UPI/NETFLIX COM/123", Amount: 649},
		{Date: day(4, 5), Description: "UPI/NETFLIX COM/124", Amount: 649},
		{Date: day(5, 5), Description: "UPI/NETFLIX COM/125", Amount: 649},
		{Date: day(8, 20), Description: "SPOTIFY AB", Amount: 119},
		{Date: day(9, 1), Description: "GROCERY MART", Amount: 1200},
	}
	subs := []model.Subscription{
		{ID: "netflix", Name: "Netflix", Status: model.StatusActive, BillingCycle: model.CycleMonthly},
		{ID: "spotify", Name: "Spotify", Status: model.StatusCancelled, BillingCycle: model.CycleMonthly},
		{ID: "github", Name: "GitHub", Status: model.StatusActive, BillingCycle: model.CycleMonthly},
		{ID: "hosting", Name: "Hosting", Status: model.StatusActive, BillingCycle: model.CycleYearly},
	}
	got := Reconcile(subs, txns, now)
	require.Len(t, got, 3)
	byID := map[string]Mismatch{}
	for _, m := range got {
		byID[m.ID] = m
	}
	assert.Equal(t, MismatchStopped, byID["netflix"].Kind, "charges stopped in May")
	assert.Equal(t, day(5, 5), byID["netflix"].LastCharged)
	assert.Equal(t, MismatchStillCharging, byID["spotify"].Kind, "cancelled but still billed")
	assert.Equal(t, MismatchUnseen, byID["github"].Kind, "six-month statement never shows it")
	_, hosting := byID["hosting"]
	assert.False(t, hosting, "a yearly sub cannot be judged from a six-month window")

	assert.Nil(t, Reconcile(subs, nil, now))
}
