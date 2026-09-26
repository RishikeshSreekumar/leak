package detect

import (
	"math"
	"sort"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
)

// Mismatch kinds reported by Reconcile.
const (
	// MismatchStillCharging: a subscription marked cancelled or paused is still
	// being charged inside the statement window.
	MismatchStillCharging = "still_charging"
	// MismatchStopped: an active subscription appears in the statement, but its
	// last charge is more than two cycles before the statement ends. It was
	// probably cancelled at the provider and never marked here.
	MismatchStopped = "stopped"
	// MismatchUnseen: an active subscription never appears in a statement that
	// spans at least two of its cycles. Paid another way, or already gone.
	MismatchUnseen = "unseen"
)

// Mismatch is one disagreement between the registry and a statement.
type Mismatch struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Kind        string    `json:"kind"`
	LastCharged time.Time `json:"last_charged,omitempty"`
	Charges     int       `json:"charges"`
}

// Reconcile compares tracked subscriptions against a statement. It is the
// other half of scan: scan finds what you pay for but do not track, Reconcile
// finds what you track but no longer pay for (or still pay for after
// cancelling). Both are hints for the user, never applied automatically.
func Reconcile(subs []model.Subscription, txns []Txn, now time.Time) []Mismatch {
	if len(txns) == 0 {
		return nil
	}
	first, last := txns[0].Date, txns[0].Date
	byKey := map[string][]Txn{}
	for _, t := range txns {
		if t.Date.Before(first) {
			first = t.Date
		}
		if t.Date.After(last) {
			last = t.Date
		}
		if k := MerchantKey(t.Description); k != "" {
			byKey[k] = append(byKey[k], t)
		}
	}
	window := last.Sub(first).Hours() / 24

	var out []Mismatch
	for _, s := range subs {
		key := MerchantKey(s.Name)
		if key == "" {
			continue
		}
		seen := byKey[key]
		sort.Slice(seen, func(i, j int) bool { return seen[i].Date.Before(seen[j].Date) })
		nominal := cycleDays(s.BillingCycle)
		m := Mismatch{ID: s.ID, Name: s.Name, Status: s.Status, Charges: len(seen)}
		if len(seen) > 0 {
			m.LastCharged = seen[len(seen)-1].Date
		}
		switch {
		case !s.Active():
			if len(seen) > 0 && last.Sub(m.LastCharged).Hours()/24 <= nominal*1.5 {
				m.Kind = MismatchStillCharging
				out = append(out, m)
			}
		case len(seen) == 0:
			if window >= nominal*2 {
				m.Kind = MismatchUnseen
				out = append(out, m)
			}
		case last.Sub(m.LastCharged).Hours()/24 > nominal*2:
			m.Kind = MismatchStopped
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// cycleDays is the nominal length of a billing cycle in days.
func cycleDays(cycle string) float64 {
	for _, s := range cycleShapes {
		if s.cycle == cycle {
			return s.nominal
		}
	}
	return math.Round(cycleShapes[1].nominal) // monthly
}
