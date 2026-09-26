package tui

import (
	"fmt"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/RishikeshSreekumar/leak/internal/render"
	"github.com/charmbracelet/lipgloss"
)

// headerLines and footerLines are the fixed chrome heights used to size the
// scrollable body.
const (
	headerLines = 7
	footerLines = 2
)

// bodyHeight is the height available to the active pane.
func (m Model) bodyHeight() int {
	h := m.height - headerLines - footerLines
	if h < 1 {
		return 1
	}
	return h
}

// View renders the whole dashboard.
func (m Model) View() string {
	if m.err != nil {
		return "Error: " + m.err.Error() + "\n"
	}
	if m.help {
		return strings.Join([]string{m.header(), m.helpBody(), m.footer()}, "\n")
	}
	if m.detail {
		return strings.Join([]string{m.header(), m.detailBody(), m.footer()}, "\n")
	}
	var body string
	switch {
	case m.loading:
		body = m.rndr.Muted("Loading…")
	case m.active == tabInsights && m.ready:
		body = m.vp.View()
	default:
		body = m.body()
	}
	screen := strings.Join([]string{m.header(), body, m.footer()}, "\n")
	if m.form != nil {
		screen = m.formModal(screen)
	}
	return screen
}

// formModal composites the add/edit huh.Form as a centered, bordered modal on
// top of the given background screen content. The title and navigation hint are
// rendered as persistent chrome above the form so they survive group transitions.
func (m Model) formModal(bg string) string {
	titleLine := m.rndr.Heading(m.form.title)
	hint := m.rndr.Muted("↑/↓ choose · Enter next · Ctrl+C cancel")
	formView := titleLine + "\n" + hint + "\n\n" + m.form.form.View()

	if !m.cfg.Color {
		// Without colour, just centre the form text over a blank screen.
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, formView)
	}

	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")).
		Padding(1, 2).
		Width(modalContentWidth + 4). // content + horizontal padding
		Render(formView)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// detailBody renders the full record for the selected subscription.
func (m Model) detailBody() string {
	id, ok := m.selectedID()
	if !ok {
		return m.rndr.Muted("Nothing selected.")
	}
	var sub model.Subscription
	for _, s := range m.subs {
		if s.ID == id {
			sub = s
			break
		}
	}
	var b strings.Builder
	b.WriteString(m.rndr.Heading(sub.Name) + "  " + m.rndr.Muted("("+sub.ID+")") + "\n\n")
	amt := money.Format(sub.Amount, sub.Currency) + cycleSuffix(sub.BillingCycle)
	fmt.Fprintf(&b, "%-14s %s\n", "Amount", amt)
	fmt.Fprintf(&b, "%-14s %s\n", "Monthly (base)", money.Format(monthlyInDefault(m.cfg, sub), m.report.Currency))
	fmt.Fprintf(&b, "%-14s %s\n", "Status", sub.Status)
	fmt.Fprintf(&b, "%-14s %s\n", "Category", orDash(sub.Category))
	fmt.Fprintf(&b, "%-14s %s\n", "Payment", orDash(sub.PaymentMethod))
	now := m.cfg.Clock.Now()
	fmt.Fprintf(&b, "%-14s %s\n", "Next renewal", orDash(sub.NextRenewal(now).String()))
	if !sub.TrialEnds.IsZero() {
		trial := sub.TrialEnds.String()
		if sub.InTrial(now) {
			trial += "  " + m.rndr.Warn("cancel before this to pay nothing")
		}
		fmt.Fprintf(&b, "%-14s %s\n", "Trial ends", trial)
	}
	fmt.Fprintf(&b, "%-14s %s\n", "Last confirmed", orDash(sub.LastConfirmed.String()))
	fmt.Fprintf(&b, "%-14s %s\n", "URL", orDash(sub.URL))
	fmt.Fprintf(&b, "%-14s %s\n", "Notes", orDash(sub.Notes))

	if len(sub.BillingHistory) > 0 {
		b.WriteString("\n" + m.rndr.Heading("Billing History") + "\n\n")
		for _, r := range sub.BillingHistory {
			est := ""
			if r.Estimated {
				est = m.rndr.Muted(" (est)")
			}
			fmt.Fprintf(&b, "%s  %s  @ %.4f %s%s\n",
				r.BilledOn.String(), money.Format(r.Amount, r.Currency), r.FXRate, r.FXSource, est)
		}
	}
	b.WriteString("\n" + m.rndr.Muted("enter/esc to close") + "\n")
	return b.String()
}

// helpBody renders the keyboard-shortcut modal.
func (m Model) helpBody() string {
	rows := [][2]string{
		{"1–5 / tab / shift+tab", "switch tab"},
		{"j / k / ↑ / ↓", "move selection · scroll Insights"},
		{"enter", "open details for selected row"},
		{"/", "filter subscriptions"},
		{"s", "cycle sort — renewal / name / amount"},
		{"a", "add subscription"},
		{"e", "edit selected"},
		{"m", "mark confirmed"},
		{"c", "cancel"},
		{"r", "reactivate"},
		{"d", "delete"},
		{"S", "sync with the configured remote"},
		{"?", "toggle this help"},
		{"q / ctrl+c", "quit"},
	}
	var b strings.Builder
	b.WriteString(m.rndr.Heading("Keyboard Shortcuts") + "\n\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-22s %s\n", r[0], m.rndr.Muted(r[1]))
	}
	b.WriteString("\n" + m.rndr.Muted("esc to close"))
	if !m.cfg.Color {
		return b.String()
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("42")).
		Padding(0, 2)
	return box.Render(b.String())
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// body dispatches to the active tab's content function.
func (m Model) body() string {
	switch m.active {
	case tabOverview:
		return m.overviewBody()
	case tabSubs:
		return m.subsBody()
	case tabDue:
		return m.dueBody()
	case tabInsights:
		return m.insightsBody()
	case tabZombies:
		return m.zombiesBody()
	}
	return ""
}

// header is the boxed LEAK_SYS banner plus the tab bar.
func (m Model) header() string {
	return m.banner() + "\n" + m.tabBar() + "\n"
}

// banner renders the pixel-logo box: a small faucet-drip glyph with the LEAK
// label, joined to the LEAK_SYS title and tagline, wrapped in a rounded border.
func (m Model) banner() string {
	green := lipgloss.Color("42")

	logoStyle := lipgloss.NewStyle()
	titleStyle := lipgloss.NewStyle()
	tagStyle := lipgloss.NewStyle()
	borderStyle := lipgloss.NewStyle()
	if m.cfg.Color {
		logoStyle = logoStyle.Foreground(green).Bold(true)
		titleStyle = titleStyle.Foreground(green).Bold(true)
		tagStyle = tagStyle.Foreground(lipgloss.Color("245"))
		borderStyle = borderStyle.Foreground(green)
	}

	logo := logoStyle.Render("▛▀▜\n▙▄▟\nLEAK")
	text := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("LEAK_SYS"),
		tagStyle.Render("Mark. :: Sweep. :: Save."),
		"",
	)
	inner := lipgloss.JoinHorizontal(lipgloss.Top, logo, "  ", text)

	return borderStyle.Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(inner)
}

// tabBar renders the tab labels, highlighting the active one.
func (m Model) tabBar() string {
	parts := make([]string, len(tabNames))
	for i, n := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, n)
		if tab(i) == m.active {
			parts[i] = m.rndr.Accent("[" + label + "]")
		} else {
			parts[i] = m.rndr.Muted(" " + label + " ")
		}
	}
	return strings.Join(parts, " ")
}

// footer shows the transient status (or nav help) below a blank spacer.
func (m Model) footer() string {
	line := m.status
	if line == "" {
		line = m.rndr.Muted(navHelp)
	}
	return "\n" + line
}

// sel prefixes a row with a selection marker (color-independent so it reads in
// golden output and under NO_COLOR).
func (m Model) sel(active bool, txt string) string {
	if active {
		if m.cfg.Color {
			return m.rndr.Accent("▸ ") + txt
		}
		return "▸ " + txt
	}
	return "  " + txt
}

// --- tab bodies ---

// emptyStateBody is shown when the registry has no subscriptions at all.
func (m Model) emptyStateBody() string {
	var b strings.Builder
	b.WriteString(m.rndr.Heading("Welcome to leak") + "\n\n")
	b.WriteString("No subscriptions tracked yet.\n\n")
	b.WriteString("Press " + m.rndr.Accent("a") + " to add your first subscription")
	if m.active != tabSubs {
		b.WriteString(" (on the " + m.rndr.Accent("2 Subscriptions") + " tab)")
	}
	b.WriteString(".\n")
	return b.String()
}

func (m Model) overviewBody() string {
	if len(m.subs) == 0 {
		return m.emptyStateBody()
	}
	r := m.report
	var b strings.Builder
	b.WriteString(m.rndr.Heading("Spending Overview") + "\n\n")
	fmt.Fprintf(&b, "%-16s%s\n", "Monthly spend", m.rndr.Accent(money.Format(r.MonthlyTotal, r.Currency)))
	fmt.Fprintf(&b, "%-16s%s\n", "Yearly spend", money.Format(r.YearlyTotal, r.Currency))
	fmt.Fprintf(&b, "%-16s%d active\n", "Subscriptions", len(r.Top))
	if len(r.Top) > 0 {
		fmt.Fprintf(&b, "%-16s%s\n", "Avg / sub", money.Format(money.Round2(r.MonthlyTotal/float64(len(r.Top))), r.Currency))
	}
	b.WriteString("\n")

	b.WriteString(m.rndr.Heading("Top Subscriptions") + "\n\n")
	b.WriteString(m.spendBars(r.TopN(5)))
	b.WriteString("\n")

	if len(m.zombies) > 0 {
		b.WriteString(m.rndr.Warn(fmt.Sprintf("%d zombie subscription(s) — potential leak %s/mo",
			len(m.zombies), money.Format(m.savings.Monthly, r.Currency))) + "\n")
	} else {
		b.WriteString(m.rndr.Accent("✓ No zombies. Clean house.") + "\n")
	}
	return b.String()
}

func (m Model) subsBody() string {
	if len(m.subs) == 0 {
		return m.emptyStateBody()
	}
	rows, _ := m.rowsForTab()
	var b strings.Builder
	if m.filtering || m.filter.Value() != "" {
		b.WriteString(m.filter.View() + "\n")
	}
	b.WriteString(m.rndr.Muted("sorted by "+m.sort.label()+" — press s to change") + "\n\n")
	if len(rows) == 0 {
		b.WriteString(m.rndr.Muted("No matching subscriptions.") + "\n")
		return b.String()
	}
	now := m.cfg.Clock.Now()
	nameW, amtW := 4, 0
	amts := make([]string, len(rows))
	for i, s := range rows {
		nameW = max(nameW, len([]rune(s.Name)))
		amts[i] = money.Format(s.Amount, s.Currency) + cycleSuffix(s.BillingCycle)
		amtW = max(amtW, len([]rune(amts[i])))
	}
	for i, s := range rows {
		line := fmt.Sprintf("%-*s  %-*s  %s", nameW, s.Name, amtW, amts[i], render.NextChargeLabel(s, now))
		if !s.Active() {
			line += m.rndr.Muted(" (" + s.Status + ")")
		}
		b.WriteString(m.sel(i == m.cursor, line) + "\n")
	}
	return b.String()
}

func (m Model) dueBody() string {
	items := insights.UpcomingWithin(m.subs, m.cfg.Clock.Now(), dueDays)
	var b strings.Builder
	b.WriteString(m.rndr.Heading(fmt.Sprintf("Next %d Days", dueDays)) + "\n\n")
	if len(items) == 0 {
		b.WriteString(m.rndr.Muted("Nothing due. Enjoy the quiet.") + "\n")
		return b.String()
	}
	for i, u := range items {
		b.WriteString(m.sel(i == m.cursor, m.rndr.UpcomingLine(u)) + "\n")
	}
	return b.String()
}

func (m Model) insightsBody() string {
	if len(m.subs) == 0 {
		return m.emptyStateBody()
	}
	r := m.report
	var b strings.Builder

	fmt.Fprintf(&b, "%s  monthly %s · yearly %s\n\n",
		m.rndr.Heading("Summary"),
		m.rndr.Accent(money.Format(r.MonthlyTotal, r.Currency)),
		money.Format(r.YearlyTotal, r.Currency))

	b.WriteString(m.rndr.Heading("Top Subscriptions") + "\n\n")
	b.WriteString(m.spendBars(r.TopN(5)))
	b.WriteString("\n")

	b.WriteString(m.rndr.Heading("Category Breakdown") + "\n\n")
	var maxCat float64
	for _, c := range r.ByCategory {
		if c.Monthly > maxCat {
			maxCat = c.Monthly
		}
	}
	for _, c := range r.ByCategory {
		fmt.Fprintf(&b, "%-14s %-20s %s (%.0f%%)\n",
			c.Category, bar(c.Monthly, maxCat, 20), money.Format(c.Monthly, r.Currency), c.Percent)
	}
	b.WriteString("\n")

	if len(r.ByPaymentMethod) > 0 {
		b.WriteString(m.rndr.Heading("Payment Methods") + "\n\n")
		var maxPm float64
		for _, pm := range r.ByPaymentMethod {
			if pm.Monthly > maxPm {
				maxPm = pm.Monthly
			}
		}
		for _, pm := range r.ByPaymentMethod {
			fmt.Fprintf(&b, "%-18s %-16s %s (%d)\n",
				pm.Method, bar(pm.Monthly, maxPm, 16), money.Format(pm.Monthly, r.Currency), pm.Count)
		}
		b.WriteString("\n")
	}

	b.WriteString(m.rndr.Heading("Coming Up (60 days)") + "\n\n")
	if len(r.Upcoming) == 0 {
		b.WriteString(m.rndr.Muted("Nothing due.") + "\n")
	}
	for _, u := range r.Upcoming {
		b.WriteString(m.rndr.UpcomingLine(u) + "\n")
	}
	return b.String()
}

// spendBars renders a name + bar + amount row per subscription, scaled to the
// largest monthly spend in the set.
func (m Model) spendBars(items []insights.NamedSpend) string {
	if len(items) == 0 {
		return m.rndr.Muted("No active subscriptions yet.") + "\n"
	}
	var max float64
	for _, it := range items {
		if it.Monthly > max {
			max = it.Monthly
		}
	}
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "%-16s %-20s %s\n",
			truncate(it.Name, 16), bar(it.Monthly, max, 20), money.Format(it.Monthly, m.report.Currency))
	}
	return b.String()
}

// truncate shortens s to n runes, adding an ellipsis when clipped.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func (m Model) zombiesBody() string {
	var b strings.Builder
	b.WriteString(m.rndr.Heading("Zombie Subscriptions") + "\n\n")
	if len(m.zombies) == 0 {
		b.WriteString(m.rndr.Accent("✓ No zombies. Clean house.") + "\n")
		return b.String()
	}
	for i, z := range m.zombies {
		head := fmt.Sprintf("%s  (%d days stale, %s/mo)",
			z.Sub.Name, z.DaysSince,
			money.Format(money.NormalizedMonthly(z.Sub.Amount, z.Sub.BillingCycle), z.Sub.Currency))
		b.WriteString(m.sel(i == m.cursor, m.rndr.Warn(head)) + "\n")
	}
	fmt.Fprintf(&b, "\nPotential leak: %s/mo\n",
		m.rndr.Accent(money.Format(m.savings.Monthly, m.report.Currency)))
	return b.String()
}

// cycleSuffix renders a compact cadence suffix like "/mo" (mirrors internal/render).
func cycleSuffix(cycle string) string {
	switch strings.ToLower(cycle) {
	case "monthly":
		return "/mo"
	case "yearly":
		return "/yr"
	case "weekly":
		return "/wk"
	case "quarterly":
		return "/qtr"
	default:
		return ""
	}
}
