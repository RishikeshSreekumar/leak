package detect

import (
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// byName finds a candidate in a result set.
func byName(cs []Candidate, name string) (Candidate, bool) {
	for _, c := range cs {
		if c.Name == name {
			return c, true
		}
	}
	return Candidate{}, false
}

func TestMerchantKeyStripsRailNoise(t *testing.T) {
	cases := map[string]string{
		"UPI/NETFLIX COM/9876543210/AXIS":    "netflix",
		"POS 4321 NETFLIX.COM MUMBAI":        "netflix",
		"NETFLIX":                            "netflix",
		"ACH DEBIT SPOTIFY INDIA PVT LTD":    "spotify",
		"GITHUB.COM/BILLING  SAN FRANCISCO":  "github",
		"AUTOPAY MANDATE GOOGLE ONE 123456":  "google one",
		"POS 9911 CAFE BLUE TOKAI BENGALURU": "cafe blue",
	}
	for desc, want := range cases {
		assert.Equal(t, want, MerchantKey(desc), desc)
	}
}

func TestRecurringDetectsMonthlyCharge(t *testing.T) {
	txns := []Txn{
		{Date: day(2026, 4, 12), Description: "UPI/NETFLIX COM/1111", Amount: 649, Currency: "INR"},
		{Date: day(2026, 5, 12), Description: "UPI/NETFLIX COM/2222", Amount: 649, Currency: "INR"},
		{Date: day(2026, 6, 12), Description: "POS 4321 NETFLIX.COM", Amount: 649, Currency: "INR"},
		{Date: day(2026, 7, 12), Description: "UPI/NETFLIX COM/4444", Amount: 649, Currency: "INR"},
	}
	got := Recurring(txns, Options{Now: now})
	require.Len(t, got, 1)
	c := got[0]
	assert.Equal(t, "Netflix", c.Name)
	assert.Equal(t, model.CycleMonthly, c.Cycle)
	assert.Equal(t, "Entertainment", c.Category)
	assert.InDelta(t, 649.0, c.Amount, 1e-9)
	assert.Equal(t, 4, c.Occurrences)
	assert.Equal(t, day(2026, 8, 11), c.NextRenewal, "projects one cadence past the last charge")
	assert.Greater(t, c.Confidence, 0.7)
}

func TestRecurringIgnoresIrregularSpend(t *testing.T) {
	// Same merchant, wildly different amounts and gaps: groceries, not a sub.
	txns := []Txn{
		{Date: day(2026, 4, 3), Description: "BIGBASKET", Amount: 1200, Currency: "INR"},
		{Date: day(2026, 4, 19), Description: "BIGBASKET", Amount: 430, Currency: "INR"},
		{Date: day(2026, 6, 2), Description: "BIGBASKET", Amount: 2890, Currency: "INR"},
		{Date: day(2026, 7, 1), Description: "BIGBASKET", Amount: 640, Currency: "INR"},
	}
	assert.Empty(t, Recurring(txns, Options{Now: now}))
}

func TestRecurringRespectsMinOccurrences(t *testing.T) {
	txns := []Txn{
		{Date: day(2026, 6, 10), Description: "SPOTIFY", Amount: 119, Currency: "INR"},
		{Date: day(2026, 7, 10), Description: "SPOTIFY", Amount: 119, Currency: "INR"},
	}
	assert.Empty(t, Recurring(txns, Options{Now: now}), "two charges is below the default threshold")

	got := Recurring(txns, Options{Now: now, MinOccurrences: 2})
	require.Len(t, got, 1)
	assert.Equal(t, "Spotify", got[0].Name)
}

func TestRecurringToleratesSmallPriceDrift(t *testing.T) {
	// Taxes and FX move the charge a little; that must not break detection.
	txns := []Txn{
		{Date: day(2026, 4, 5), Description: "OPENAI SUBSCRIPTION", Amount: 20.00, Currency: "USD"},
		{Date: day(2026, 5, 5), Description: "OPENAI SUBSCRIPTION", Amount: 20.62, Currency: "USD"},
		{Date: day(2026, 6, 5), Description: "OPENAI SUBSCRIPTION", Amount: 21.10, Currency: "USD"},
	}
	got := Recurring(txns, Options{Now: now})
	require.Len(t, got, 1)
	assert.Equal(t, "OpenAI", got[0].Name)
	assert.Equal(t, "AI", got[0].Category)
	assert.InDelta(t, 20.62, got[0].Amount, 1e-9, "median charge, not the first or last")
}

func TestRecurringDetectsYearlyAndWeekly(t *testing.T) {
	txns := []Txn{
		{Date: day(2024, 3, 1), Description: "JETBRAINS", Amount: 9900, Currency: "INR"},
		{Date: day(2025, 3, 2), Description: "JETBRAINS", Amount: 9900, Currency: "INR"},
		{Date: day(2026, 3, 1), Description: "JETBRAINS", Amount: 9900, Currency: "INR"},

		{Date: day(2026, 6, 15), Description: "THE HINDU EPAPER", Amount: 50, Currency: "INR"},
		{Date: day(2026, 6, 22), Description: "THE HINDU EPAPER", Amount: 50, Currency: "INR"},
		{Date: day(2026, 6, 29), Description: "THE HINDU EPAPER", Amount: 50, Currency: "INR"},
		{Date: day(2026, 7, 6), Description: "THE HINDU EPAPER", Amount: 50, Currency: "INR"},
	}
	got := Recurring(txns, Options{Now: now})
	require.Len(t, got, 2)

	yearly, ok := byName(got, "Jetbrains")
	if !ok {
		yearly, ok = byName(got, "JetBrains")
	}
	require.True(t, ok, "expected a JetBrains candidate in %+v", got)
	assert.Equal(t, model.CycleYearly, yearly.Cycle)

	weekly, ok := byName(got, "The Hindu")
	require.True(t, ok, "expected a weekly candidate in %+v", got)
	assert.Equal(t, model.CycleWeekly, weekly.Cycle)
}

func TestRecurringDropsStaleSeriesConfidence(t *testing.T) {
	// Charges that stopped a year ago: still recurring in shape, but the low
	// recency score should rank them below a live subscription.
	stale := []Txn{
		{Date: day(2025, 1, 10), Description: "ADOBE CC", Amount: 1675, Currency: "INR"},
		{Date: day(2025, 2, 10), Description: "ADOBE CC", Amount: 1675, Currency: "INR"},
		{Date: day(2025, 3, 10), Description: "ADOBE CC", Amount: 1675, Currency: "INR"},
	}
	live := []Txn{
		{Date: day(2026, 5, 10), Description: "NOTION", Amount: 800, Currency: "INR"},
		{Date: day(2026, 6, 10), Description: "NOTION", Amount: 800, Currency: "INR"},
		{Date: day(2026, 7, 10), Description: "NOTION", Amount: 800, Currency: "INR"},
	}
	got := Recurring(append(stale, live...), Options{Now: now})
	require.Len(t, got, 2)
	assert.Equal(t, "Notion", got[0].Name, "live subscriptions rank first")
	assert.Less(t, got[1].Confidence, got[0].Confidence)
}

func TestRecurringCollapsesSameDayDuplicates(t *testing.T) {
	txns := []Txn{
		{Date: day(2026, 5, 1), Description: "GITHUB", Amount: 400, Currency: "INR"},
		{Date: day(2026, 5, 1), Description: "GITHUB", Amount: 400, Currency: "INR"},
		{Date: day(2026, 6, 1), Description: "GITHUB", Amount: 400, Currency: "INR"},
		{Date: day(2026, 7, 1), Description: "GITHUB", Amount: 400, Currency: "INR"},
	}
	got := Recurring(txns, Options{Now: now})
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].Occurrences, "a duplicated statement line is one charge")
	assert.Equal(t, model.CycleMonthly, got[0].Cycle)
}
