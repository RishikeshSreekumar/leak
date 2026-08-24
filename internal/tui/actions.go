package tui

import (
	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
	tea "github.com/charmbracelet/bubbletea"
)

// loadedMsg carries a freshly computed snapshot of the registry and its
// aggregates back to the model.
type loadedMsg struct {
	subs    []model.Subscription
	report  insights.Report
	zombies []audit.Zombie
	savings audit.Savings
}

// actionDoneMsg reports a completed mutation with a human-readable status.
type actionDoneMsg struct{ status string }

// errMsg carries a background error to the model.
type errMsg struct{ err error }

// loadCmd reads the store and recomputes every aggregate off the UI thread, so
// the FX network calls inside insights.Build never block rendering.
func (m Model) loadCmd() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		data, err := cfg.Store.Load()
		if err != nil {
			return errMsg{err}
		}
		now := cfg.Clock.Now()
		subs := data.Subscriptions
		report := insights.Build(subs, cfg.Profile, cfg.FX, now)
		zs := audit.Zombies(subs, now, cfg.Profile.StaleAfterDays)
		sav := audit.SavingsFor(zs, func(z audit.Zombie) float64 { return monthlyInDefault(cfg, z.Sub) })
		return loadedMsg{subs: subs, report: report, zombies: zs, savings: sav}
	}
}

// markCmd confirms a subscription as in use — same semantics as `leak mark`.
func (m Model) markCmd(id string) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		sub, err := cfg.Store.GetSub(id)
		if err != nil {
			return errMsg{err}
		}
		sub.LastConfirmed = model.Date{Time: cfg.Clock.Now()}
		sub.Touch(cfg.Clock.Now())
		if err := cfg.Store.UpdateSub(sub); err != nil {
			return errMsg{err}
		}
		return actionDoneMsg{status: "Confirmed " + sub.Name}
	}
}

// cancelCmd marks a subscription cancelled — same semantics as `leak remove`.
func (m Model) cancelCmd(id string) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		sub, err := cfg.Store.GetSub(id)
		if err != nil {
			return errMsg{err}
		}
		sub.Status = model.StatusCancelled
		sub.Touch(cfg.Clock.Now())
		if err := cfg.Store.UpdateSub(sub); err != nil {
			return errMsg{err}
		}
		return actionDoneMsg{status: "Cancelled " + sub.Name}
	}
}

// reactivateCmd flips a cancelled/paused subscription back to active.
func (m Model) reactivateCmd(id string) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		sub, err := cfg.Store.GetSub(id)
		if err != nil {
			return errMsg{err}
		}
		sub.Status = model.StatusActive
		sub.LastConfirmed = model.Date{Time: cfg.Clock.Now()}
		sub.Touch(cfg.Clock.Now())
		if err := cfg.Store.UpdateSub(sub); err != nil {
			return errMsg{err}
		}
		return actionDoneMsg{status: "Reactivated " + sub.Name}
	}
}

// saveFormCmd persists an add/edit overlay. Add mirrors `leak add` (default
// last_confirmed, an FX billing record, Touch); edit mirrors `leak edit`.
func (m Model) saveFormCmd(sf *subForm) tea.Cmd {
	cfg := m.cfg
	mode := sf.mode
	sub := sf.result()
	return func() tea.Msg {
		now := cfg.Clock.Now()
		if mode == formAdd {
			if sub.LastConfirmed.IsZero() {
				sub.LastConfirmed = model.Date{Time: now}
			}
			attachFXRecord(cfg, &sub)
			sub.Touch(now)
			saved, err := cfg.Store.AddSub(sub)
			if err != nil {
				return errMsg{err}
			}
			return actionDoneMsg{status: "Added " + saved.Name}
		}
		sub.Touch(now)
		if err := cfg.Store.UpdateSub(sub); err != nil {
			return errMsg{err}
		}
		return actionDoneMsg{status: "Updated " + sub.Name}
	}
}

// attachFXRecord captures the FX rate for the subscription's currency vs the
// default reporting currency (mirrors cmd.attachFXRecord).
func attachFXRecord(cfg Config, sub *model.Subscription) {
	target := cfg.Profile.DefaultCurrency
	billedOn := sub.RenewalDate
	if billedOn.IsZero() {
		billedOn = model.Date{Time: cfg.Clock.Now()}
	}
	rate, estimated, err := cfg.FX.Rate(sub.Currency, target, billedOn)
	rec := model.BillingRecord{
		BilledOn: billedOn,
		Amount:   sub.Amount,
		Currency: sub.Currency,
		FXRate:   1.0,
	}
	if err == nil {
		rec.FXRate = rate
		if s, ok := cfg.FX.(fx.Sourced); ok {
			rec.FXSource = s.SourceName()
		}
		rec.FXDate = billedOn
		rec.Estimated = estimated
	}
	sub.BillingHistory = append(sub.BillingHistory, rec)
}

// deleteCmd permanently removes a subscription — same semantics as
// `leak remove --hard`.
func (m Model) deleteCmd(id string) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		sub, err := cfg.Store.GetSub(id)
		if err != nil {
			return errMsg{err}
		}
		if err := cfg.Store.RemoveSub(id, cfg.Clock.Now()); err != nil {
			return errMsg{err}
		}
		return actionDoneMsg{status: "Deleted " + sub.Name}
	}
}

// monthlyInDefault converts a subscription's normalized monthly cost into the
// profile default currency (mirrors cmd.monthlyInDefault; kept local to avoid
// an import cycle with the cmd package).
func monthlyInDefault(cfg Config, sub model.Subscription) float64 {
	rate, _, err := cfg.FX.Rate(sub.Currency, cfg.Profile.DefaultCurrency, sub.RenewalDate)
	monthly := money.NormalizedMonthly(sub.Amount, sub.BillingCycle)
	if err != nil {
		return monthly
	}
	return monthly * rate
}
