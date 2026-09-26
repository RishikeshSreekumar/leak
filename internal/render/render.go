// Package render turns Leak's data structures into terminal output. Functions
// are pure (data -> string) so they can be golden-tested; styling is stripped
// when color is disabled (NO_COLOR or a non-TTY stdout).
package render

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Styler carries whether ANSI styling is active. Build one with New.
type Styler struct {
	color   bool
	accent  lipgloss.Style
	warn    lipgloss.Style
	muted   lipgloss.Style
	heading lipgloss.Style
}

// New returns a Styler. color=false yields plain, deterministic text.
func New(color bool) *Styler {
	s := &Styler{color: color}
	if color {
		s.accent = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
		s.warn = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
		s.muted = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		s.heading = lipgloss.NewStyle().Bold(true).Underline(true)
	}
	return s
}

// IsTTY reports whether the given file is an interactive terminal. It uses a
// real isatty check (not just os.ModeCharDevice, which is also set for device
// files like /dev/null) so callers can safely gate a full-screen program on it.
func IsTTY(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// ColorEnabled reports whether color should be used for the given file,
// honoring NO_COLOR and TTY detection.
func ColorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return IsTTY(f)
}

func (s *Styler) style(st lipgloss.Style, txt string) string {
	if !s.color {
		return txt
	}
	return st.Render(txt)
}

func (s *Styler) Accent(t string) string  { return s.style(s.accent, t) }
func (s *Styler) Warn(t string) string    { return s.style(s.warn, t) }
func (s *Styler) Muted(t string) string   { return s.style(s.muted, t) }
func (s *Styler) Heading(t string) string { return s.style(s.heading, t) }

// List renders the subscription table: name, amount, next charge, and the id
// every other command takes.
func (s *Styler) List(subs []model.Subscription, now time.Time) string {
	if len(subs) == 0 {
		return s.Muted("No subscriptions yet. Add one with `leak add`, or find them "+
			"automatically with `leak scan <statement.csv>`.") + "\n"
	}
	nameW, amtW := 4, 0
	amts := make([]string, len(subs))
	for i, sub := range subs {
		nameW = max(nameW, runeLen(sub.Name))
		amts[i] = money.Format(sub.Amount, sub.Currency) + cycleSuffix(sub.BillingCycle)
		amtW = max(amtW, runeLen(amts[i]))
	}
	var b strings.Builder
	for i, sub := range subs {
		line := fmt.Sprintf("%-*s  %-*s  %-10s  ", nameW, sub.Name, amtW, amts[i], NextChargeLabel(sub, now))
		line += s.Muted(sub.ID)
		if sub.Status != model.StatusActive {
			line += s.Muted(" (" + sub.Status + ")")
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
}

// NextChargeLabel is the compact "when does money move next" cell used by list
// views: the trial end while in trial, else the rolled-forward renewal date,
// else a dash.
func NextChargeLabel(sub model.Subscription, now time.Time) string {
	if !sub.Active() {
		return "—"
	}
	if sub.InTrial(now) {
		return "trial " + sub.TrialEnds.Format("Jan 02")
	}
	if next := sub.NextRenewal(now); !next.IsZero() {
		return next.String()
	}
	return "—"
}

// runeLen counts display characters, so currency symbols pad correctly.
func runeLen(s string) int { return len([]rune(s)) }

// cycleSuffix renders a compact cadence suffix like "/mo".
func cycleSuffix(cycle string) string {
	switch strings.ToLower(cycle) {
	case model.CycleMonthly:
		return "/mo"
	case model.CycleYearly:
		return "/yr"
	case model.CycleWeekly:
		return "/wk"
	case model.CycleQuarterly:
		return "/qtr"
	default:
		return ""
	}
}

// Due renders upcoming renewals and trial conversions (spec §5.3).
func (s *Styler) Due(items []insights.Upcoming, days int) string {
	var b strings.Builder
	b.WriteString(s.Heading(fmt.Sprintf("Next %d Days", days)) + "\n\n")
	if len(items) == 0 {
		b.WriteString(s.Muted("Nothing due. Enjoy the quiet.\n"))
		return b.String()
	}
	for _, u := range items {
		b.WriteString(s.UpcomingLine(u) + "\n")
	}
	return b.String()
}

// UpcomingLine renders one due row: date, name, amount, and a trial marker.
func (s *Styler) UpcomingLine(u insights.Upcoming) string {
	line := fmt.Sprintf("%s  %-18s %s", u.Date.Format("Jan 02"), u.Name, money.Format(u.Amount, u.Currency))
	if u.Trial {
		line += "  " + s.Warn("trial ends — cancel before this to pay nothing")
	}
	return line
}

// DueOneLine is the shell-prompt form of Due: a single line naming what is
// coming, or empty when nothing is.
func (s *Styler) DueOneLine(items []insights.Upcoming, days int) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, u := range items {
		when := fmt.Sprintf("in %dd", u.Days)
		switch u.Days {
		case 0:
			when = "today"
		case 1:
			when = "tomorrow"
		}
		p := fmt.Sprintf("%s %s %s", u.Name, money.Format(u.Amount, u.Currency), when)
		if u.Trial {
			p = fmt.Sprintf("%s trial ends %s", u.Name, when)
		}
		parts = append(parts, p)
	}
	return fmt.Sprintf("leak: %d due in %dd — %s\n", len(items), days, strings.Join(parts, ", "))
}

// Stats renders monthly spend + category breakdown (spec §5.4).
func (s *Styler) Stats(r insights.Report) string {
	var b strings.Builder
	b.WriteString(s.Heading("Monthly Spend") + "\n\n")
	b.WriteString("Default Currency: " + r.Currency + "\n\n")
	b.WriteString("Total:\n" + s.Accent(money.Format(r.MonthlyTotal, r.Currency)) + "\n\n")
	b.WriteString(s.Heading("By Category") + "\n\n")
	for _, c := range r.ByCategory {
		b.WriteString(fmt.Sprintf("%-16s%s\n", c.Category, money.Format(c.Monthly, r.Currency)))
	}
	return b.String()
}

// Sweep renders detected zombies and potential savings (spec §5.5).
func (s *Styler) Sweep(zs []audit.Zombie, currency string, sav audit.Savings) string {
	var b strings.Builder
	b.WriteString(s.Heading("Zombie Subscriptions") + "\n\n")
	if len(zs) == 0 {
		b.WriteString(s.Accent("✓ No zombies. Clean house.") + "\n")
		return b.String()
	}
	for _, z := range zs {
		b.WriteString(s.Warn(z.Sub.Name) + "\n")
		b.WriteString("  Last confirmed: " + fmt.Sprintf("%d days ago", z.DaysSince) + "\n")
		b.WriteString("  Potential savings: " +
			money.Format(money.NormalizedMonthly(z.Sub.Amount, z.Sub.BillingCycle), z.Sub.Currency) + "/month\n\n")
	}
	b.WriteString(s.Warn(fmt.Sprintf("⚠ %d zombie subscription(s) detected", len(zs))) + "\n")
	b.WriteString("Potential leak: " + s.Accent(money.Format(sav.Monthly, currency)+"/month") + "\n")
	return b.String()
}

// Insights renders the full insights view (spec §6).
func (s *Styler) Insights(r insights.Report, zombieCount int, sav audit.Savings) string {
	var b strings.Builder
	b.WriteString(s.Heading("Spending Overview") + "\n\n")
	b.WriteString(fmt.Sprintf("Monthly spend: %s\n", s.Accent(money.Format(r.MonthlyTotal, r.Currency))))
	b.WriteString(fmt.Sprintf("Yearly spend:  %s\n\n", money.Format(r.YearlyTotal, r.Currency)))

	b.WriteString(s.Heading("Top Subscriptions") + "\n\n")
	for _, t := range r.TopN(3) {
		b.WriteString(fmt.Sprintf("%-16s%s\n", t.Name, money.Format(t.Monthly, r.Currency)))
	}
	b.WriteString("\n")

	b.WriteString(s.Heading("Category Breakdown") + "\n\n")
	for _, c := range r.ByCategory {
		b.WriteString(fmt.Sprintf("%-16s%.0f%%\n", c.Category, c.Percent))
	}
	b.WriteString("\n")

	b.WriteString(s.Heading("Coming Up") + "\n\n")
	if len(r.Upcoming) == 0 {
		b.WriteString(s.Muted("Nothing due in the next 60 days.") + "\n")
	}
	for i, u := range r.Upcoming {
		if i == 5 {
			b.WriteString(s.Muted(fmt.Sprintf("… and %d more — see `leak due --days 60`", len(r.Upcoming)-5)) + "\n")
			break
		}
		b.WriteString(s.UpcomingLine(u) + "\n")
	}
	b.WriteString("\n")

	if len(r.ByPaymentMethod) > 0 {
		b.WriteString(s.Heading("Payment Methods") + "\n\n")
		for _, m := range r.ByPaymentMethod {
			b.WriteString(fmt.Sprintf("%-18s %d subs  %s\n", m.Method, m.Count, money.Format(m.Monthly, r.Currency)))
		}
		b.WriteString("\n")
	}

	b.WriteString(s.Warn(fmt.Sprintf("%d subscription(s) unconfirmed past the stale threshold.", zombieCount)) + "\n")
	b.WriteString("Potential yearly savings: " + s.Accent(money.Format(sav.Yearly, r.Currency)) + "\n")
	return b.String()
}
