// Package insights aggregates subscriptions into the totals, breakdowns, and
// heatmaps that power `leak stats` and `leak insights`.
package insights

import (
	"sort"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
)

// Report is the full aggregation over the active registry, all monetary values
// expressed in the profile's default currency.
type Report struct {
	Currency        string
	MonthlyTotal    float64
	YearlyTotal     float64
	ByCategory      []CategorySpend
	ByPaymentMethod []MethodSpend
	Top             []NamedSpend
	Heatmap         []WeekBucket
}

// CategorySpend is monthly spend for a category with its share of the total.
type CategorySpend struct {
	Category string
	Monthly  float64
	Percent  float64
}

// MethodSpend is monthly spend and count for a payment method.
type MethodSpend struct {
	Method  string
	Count   int
	Monthly float64
}

// NamedSpend is a subscription's monthly spend (for top-N lists).
type NamedSpend struct {
	ID      string
	Name    string
	Monthly float64
}

// WeekBucket counts upcoming renewals within a forward week window.
type WeekBucket struct {
	Label string
	Count int
}

// convertMonthly returns a subscription's normalized monthly cost in target
// currency, using the FX provider (falls back to raw amount on FX error).
func convertMonthly(s model.Subscription, target string, p fx.Provider) float64 {
	monthly := money.NormalizedMonthly(s.Amount, s.BillingCycle)
	rate, _, err := p.Rate(s.Currency, target, s.RenewalDate)
	if err != nil {
		return monthly
	}
	return money.Convert(monthly, rate)
}

// Build computes the full Report over active subscriptions.
func Build(subs []model.Subscription, profile model.Profile, p fx.Provider, now time.Time) Report {
	target := profile.DefaultCurrency
	r := Report{Currency: target}

	catMonthly := map[string]float64{}
	methodMonthly := map[string]float64{}
	methodCount := map[string]int{}

	for _, s := range subs {
		if !s.Active() {
			continue
		}
		m := convertMonthly(s, target, p)
		r.MonthlyTotal += m
		if s.Category != "" {
			catMonthly[s.Category] += m
		}
		if s.PaymentMethod != "" {
			methodMonthly[s.PaymentMethod] += m
			methodCount[s.PaymentMethod]++
		}
		r.Top = append(r.Top, NamedSpend{ID: s.ID, Name: s.Name, Monthly: money.Round2(m)})
	}

	r.MonthlyTotal = money.Round2(r.MonthlyTotal)
	r.YearlyTotal = money.Round2(r.MonthlyTotal * 12)

	for cat, m := range catMonthly {
		pct := 0.0
		if r.MonthlyTotal > 0 {
			pct = m / r.MonthlyTotal * 100
		}
		r.ByCategory = append(r.ByCategory, CategorySpend{Category: cat, Monthly: money.Round2(m), Percent: money.Round2(pct)})
	}
	sortByMonthlyThenName(r.ByCategory)

	for method, m := range methodMonthly {
		r.ByPaymentMethod = append(r.ByPaymentMethod, MethodSpend{Method: method, Count: methodCount[method], Monthly: money.Round2(m)})
	}
	sort.SliceStable(r.ByPaymentMethod, func(i, j int) bool {
		if r.ByPaymentMethod[i].Monthly != r.ByPaymentMethod[j].Monthly {
			return r.ByPaymentMethod[i].Monthly > r.ByPaymentMethod[j].Monthly
		}
		return r.ByPaymentMethod[i].Method < r.ByPaymentMethod[j].Method
	})

	sort.SliceStable(r.Top, func(i, j int) bool {
		if r.Top[i].Monthly != r.Top[j].Monthly {
			return r.Top[i].Monthly > r.Top[j].Monthly
		}
		return r.Top[i].Name < r.Top[j].Name
	})

	r.Heatmap = heatmap(subs, now)
	return r
}

func sortByMonthlyThenName(cs []CategorySpend) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Monthly != cs[j].Monthly {
			return cs[i].Monthly > cs[j].Monthly
		}
		return cs[i].Category < cs[j].Category
	})
}

// TopN returns the first n entries of the (already sorted) Top slice.
func (r Report) TopN(n int) []NamedSpend {
	if n > len(r.Top) {
		n = len(r.Top)
	}
	return r.Top[:n]
}

// heatmap buckets active renewals into the next four forward week windows.
func heatmap(subs []model.Subscription, now time.Time) []WeekBucket {
	labels := []string{"This week", "Next week", "Week 3", "Week 4"}
	buckets := make([]WeekBucket, len(labels))
	for i, l := range labels {
		buckets[i] = WeekBucket{Label: l}
	}
	for _, s := range subs {
		if !s.Active() || s.RenewalDate.IsZero() {
			continue
		}
		days := int(s.RenewalDate.Sub(now).Hours() / 24)
		if days < 0 || days >= 28 {
			continue
		}
		buckets[days/7].Count++
	}
	return buckets
}
