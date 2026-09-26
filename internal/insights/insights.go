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
// expressed in the profile's default currency. The JSON tags are a public
// contract: `leak stats --json` output is meant to be piped into other tools.
type Report struct {
	Currency        string          `json:"currency"`
	MonthlyTotal    float64         `json:"monthly_total"`
	YearlyTotal     float64         `json:"yearly_total"`
	ByCategory      []CategorySpend `json:"by_category"`
	ByPaymentMethod []MethodSpend   `json:"by_payment_method"`
	Top             []NamedSpend    `json:"top"`
	Heatmap         []WeekBucket    `json:"heatmap"`
	// Upcoming is the next few charges (renewals and trial conversions), soonest first.
	Upcoming []Upcoming `json:"upcoming"`
}

// Upcoming is one dated event Leak wants the user to see coming: a renewal, or
// a trial converting to paid. Dates are rolled forward from the stored anchor,
// so a subscription added months ago still surfaces on its next charge.
type Upcoming struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Date     model.Date `json:"date"`
	Days     int        `json:"days"` // days from today (0 = today)
	Amount   float64    `json:"amount"`
	Currency string     `json:"currency"`
	Trial    bool       `json:"trial"` // true when Date is a trial ending, not a renewal
}

// UpcomingWithin lists active subscriptions with a renewal or trial end inside
// the next `days` days, soonest first. A subscription still in trial reports
// the trial end instead of its renewal, since that is the day money moves.
func UpcomingWithin(subs []model.Subscription, now time.Time, days int) []Upcoming {
	var out []Upcoming
	for _, s := range subs {
		if !s.Active() {
			continue
		}
		u := Upcoming{ID: s.ID, Name: s.Name, Amount: s.Amount, Currency: s.Currency}
		switch {
		case s.InTrial(now):
			u.Date, u.Trial = s.TrialEnds, true
		default:
			u.Date = s.NextRenewal(now)
		}
		if u.Date.IsZero() {
			continue
		}
		u.Days = u.Date.DaysUntil(now)
		if u.Days < 0 || u.Days > days {
			continue
		}
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date.Time) {
			return out[i].Date.Before(out[j].Date.Time)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// CategorySpend is monthly spend for a category with its share of the total.
type CategorySpend struct {
	Category string  `json:"category"`
	Monthly  float64 `json:"monthly"`
	Percent  float64 `json:"percent"`
}

// MethodSpend is monthly spend and count for a payment method.
type MethodSpend struct {
	Method  string  `json:"method"`
	Count   int     `json:"count"`
	Monthly float64 `json:"monthly"`
}

// NamedSpend is a subscription's monthly spend (for top-N lists).
type NamedSpend struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Monthly float64 `json:"monthly"`
}

// WeekBucket counts upcoming renewals within a forward week window.
type WeekBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
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
	r.Upcoming = UpcomingWithin(subs, now, 60)
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
		days := s.NextRenewal(now).DaysUntil(now)
		if days < 0 || days >= 28 {
			continue
		}
		buckets[days/7].Count++
	}
	return buckets
}
