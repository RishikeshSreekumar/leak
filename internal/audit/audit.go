// Package audit implements Leak's mark-and-sweep workflow: detecting stale
// ("zombie") subscriptions and computing potential savings.
package audit

import (
	"sort"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
)

// Zombie is an active subscription unconfirmed for longer than the stale
// threshold — a cancellation candidate.
type Zombie struct {
	Sub            model.Subscription
	DaysSince      int     // days since last confirmation (or since add if never)
	MonthlySavings float64 // normalized monthly amount, original currency
}

// confirmationAnchor returns the date we measure staleness from: last_confirmed
// if set, else the renewal date (best available proxy for "known active").
func confirmationAnchor(s model.Subscription) model.Date {
	if !s.LastConfirmed.IsZero() {
		return s.LastConfirmed
	}
	return s.RenewalDate
}

// Zombies returns active subscriptions unconfirmed for > staleDays, most stale
// first. Subscriptions with no anchor date are treated as maximally stale.
func Zombies(subs []model.Subscription, now time.Time, staleDays int) []Zombie {
	var out []Zombie
	for _, s := range subs {
		if !s.Active() {
			continue
		}
		anchor := confirmationAnchor(s)
		days := anchor.DaysSince(now)
		if anchor.IsZero() {
			days = staleDays + 1 // unknown -> flag it
		}
		if days > staleDays {
			out = append(out, Zombie{
				Sub:            s,
				DaysSince:      days,
				MonthlySavings: money.NormalizedMonthly(s.Amount, s.BillingCycle),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DaysSince > out[j].DaysSince })
	return out
}

// NeedsReview returns active subscriptions confirmed longer ago than
// reviewDays — the queue for `leak review`.
func NeedsReview(subs []model.Subscription, now time.Time, reviewDays int) []model.Subscription {
	var out []model.Subscription
	for _, s := range subs {
		if !s.Active() {
			continue
		}
		anchor := confirmationAnchor(s)
		if anchor.IsZero() || anchor.DaysSince(now) > reviewDays {
			out = append(out, s)
		}
	}
	return out
}

// Savings holds roll-up totals in the caller's chosen currency.
type Savings struct {
	Monthly float64
	Yearly  float64
}

// SavingsFor sums per-zombie monthly savings using convert to bring each into
// the reporting currency, and derives the yearly figure.
func SavingsFor(zs []Zombie, convert func(z Zombie) float64) Savings {
	var monthly float64
	for _, z := range zs {
		monthly += convert(z)
	}
	monthly = money.Round2(monthly)
	return Savings{Monthly: monthly, Yearly: money.Round2(monthly * 12)}
}
