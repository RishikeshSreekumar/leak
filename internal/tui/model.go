// Package tui is Leak's full-screen interactive dashboard: a Bubble Tea program
// that unifies the overview, subscription list, upcoming renewals, insights, and
// zombie audit into keyboard-driven tabs with in-place quick actions.
//
// The heavy lifting stays in the pure packages (insights, audit, money); this
// package only adds a view/controller layer and the store mutations behind the
// mark/cancel/delete keys.
package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/clock"
	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/render"
	"github.com/RishikeshSreekumar/leak/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// dueDays is the look-ahead window for the Due tab.
const dueDays = 30

// tab identifies a dashboard pane.
type tab int

const (
	tabOverview tab = iota
	tabSubs
	tabDue
	tabInsights
	tabZombies
	tabCount
)

var tabNames = []string{"Overview", "Subscriptions", "Due", "Insights", "Zombies"}

// sortMode orders the Subscriptions list.
type sortMode int

const (
	sortRenewal sortMode = iota
	sortName
	sortAmount
	sortModeCount
)

var sortLabels = []string{"renewal", "name", "amount"}

func (s sortMode) label() string { return sortLabels[int(s)%len(sortLabels)] }

// Config bundles everything the dashboard needs. It mirrors the fields of
// cmd.Deps that the TUI reads, without importing cmd (which would cycle).
type Config struct {
	Store   store.Store
	FX      fx.Provider
	Clock   clock.Clock
	Profile model.Profile
	Color   bool
}

// Model is the Bubble Tea model for the dashboard.
type Model struct {
	cfg  Config
	rndr *render.Styler

	active tab
	cursor int

	subs    []model.Subscription
	report  insights.Report
	zombies []audit.Zombie
	savings audit.Savings

	filter    textinput.Model
	filtering bool
	vp        viewport.Model

	sort   sortMode
	detail bool     // detail overlay for the selected row
	help   bool     // help modal overlay
	form   *subForm // add/edit overlay (nil when inactive)

	width, height int
	status        string
	loading       bool
	ready         bool
	err           error
}

// New builds an initial model. Call tea.NewProgram(New(cfg)).Run() to launch.
func New(cfg Config) Model {
	ti := textinput.New()
	ti.Placeholder = "filter by name or category"
	ti.Prompt = "/"
	return Model{
		cfg:     cfg,
		rndr:    render.New(cfg.Color),
		filter:  ti,
		loading: true,
	}
}

// Init kicks off the first async load.
func (m Model) Init() tea.Cmd { return m.loadCmd() }

// Update is the Bubble Tea event loop.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp = viewport.New(msg.Width, m.bodyHeight())
		m.vp.SetContent(m.insightsBody())
		m.ready = true
		if m.form != nil {
			return m.updateForm(msg)
		}
		return m, nil

	case loadedMsg:
		m.subs, m.report, m.zombies, m.savings = msg.subs, msg.report, msg.zombies, msg.savings
		m.loading = false
		m.clampCursor()
		m.vp.SetContent(m.insightsBody())
		return m, nil

	case actionDoneMsg:
		m.status = msg.status
		return m, m.loadCmd() // refresh aggregates after a mutation

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		if m.form != nil {
			return m.updateForm(msg)
		}
		return m.handleKey(msg)
	}
	// Forward any other message (huh ticks, etc.) to an active form.
	if m.form != nil {
		return m.updateForm(msg)
	}
	return m, nil
}

// updateForm drives the add/edit overlay and saves on completion.
func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	fm, cmd := m.form.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		m.form.form = f
	}
	switch m.form.form.State {
	case huh.StateCompleted:
		save := m.saveFormCmd(m.form)
		m.form = nil
		return m, save
	case huh.StateAborted:
		m.form = nil
		m.status = "Cancelled — no changes"
		return m, nil
	}
	return m, cmd
}

// handleKey routes a keypress. Filtering and the detail overlay capture keys
// before the normal bindings.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case keyEsc:
			m.filtering = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.clampCursor()
			return m, nil
		case keyEnter:
			m.filtering = false
			m.filter.Blur()
			m.clampCursor()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.cursor = 0
		return m, cmd
	}

	if m.help {
		switch msg.String() {
		case keyCtrlC:
			return m, tea.Quit
		case keyEsc, keyHelp, keyQuit:
			m.help = false
		}
		return m, nil
	}

	if m.detail {
		switch msg.String() {
		case keyQuit, keyCtrlC:
			return m, tea.Quit
		case keyEsc, keyEnter:
			m.detail = false
		}
		return m, nil
	}

	switch msg.String() {
	case keyQuit, keyCtrlC:
		return m, tea.Quit
	case keyHelp:
		m.help = true
		return m, nil
	case keyTab:
		m.active = (m.active + 1) % tabCount
		m.cursor, m.status = 0, ""
		return m, nil
	case keyShiftTab:
		m.active = (m.active + tabCount - 1) % tabCount
		m.cursor, m.status = 0, ""
		return m, nil
	case "1", "2", "3", "4", "5":
		m.active = tab(msg.String()[0] - '1')
		m.cursor, m.status = 0, ""
		return m, nil
	case keyFilter:
		if m.active == tabSubs {
			m.filtering = true
			m.filter.Focus()
			return m, nil
		}
	case keySort:
		if m.active == tabSubs {
			m.sort = (m.sort + 1) % sortModeCount
			m.cursor = 0
			m.status = "Sorted by " + m.sort.label()
			return m, nil
		}
	case keyDetail:
		if _, ok := m.selectedID(); ok {
			m.detail = true
		}
		return m, nil
	case keyAdd:
		if m.active == tabSubs {
			m.form = newSubForm(formAdd, model.Subscription{}, m.cfg.Profile)
			return m, m.form.form.Init()
		}
	case keyEdit:
		return m.startEdit()
	case keyUp, keyK:
		return m.move(-1)
	case keyDown, keyJ:
		return m.move(1)
	case keyMark:
		return m.act(m.markCmd)
	case keyCancel:
		return m.act(m.cancelCmd)
	case keyReactivate:
		return m.act(m.reactivateCmd)
	case keyDelete:
		return m.act(m.deleteCmd)
	case keySync:
		if !m.cfg.Profile.Sync.Enabled() {
			m.status = "Sync not configured — run `leak sync init --dir <path>`"
			return m, nil
		}
		m.status = "Syncing…"
		return m, m.syncCmd()
	}

	// Forward remaining keys to the viewport for Insights scrolling.
	if m.active == tabInsights {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// startEdit opens the edit overlay for the selected subscription.
func (m Model) startEdit() (tea.Model, tea.Cmd) {
	id, ok := m.selectedID()
	if !ok {
		return m, nil
	}
	sub, err := m.cfg.Store.GetSub(id)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.form = newSubForm(formEdit, sub, m.cfg.Profile)
	return m, m.form.form.Init()
}

// move shifts the cursor within the active tab's row list, if any.
func (m Model) move(delta int) (tea.Model, tea.Cmd) {
	if m.active == tabInsights {
		var cmd tea.Cmd
		m.vp.SetContent(m.insightsBody())
		m.vp, cmd = m.vp.Update(scrollKey(delta))
		return m, cmd
	}
	n := m.rowCount()
	if n == 0 {
		return m, nil
	}
	m.cursor = (m.cursor + delta + n) % n
	return m, nil
}

// act dispatches a mutation command against the selected row, if the active tab
// carries a selection.
func (m Model) act(cmd func(id string) tea.Cmd) (tea.Model, tea.Cmd) {
	id, ok := m.selectedID()
	if !ok {
		return m, nil
	}
	return m, cmd(id)
}

// --- selection & row helpers (pure, terminal-free) ---

// visibleSubs filters subscriptions by a case-insensitive substring over name
// and category.
func visibleSubs(subs []model.Subscription, filter string) []model.Subscription {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return subs
	}
	var out []model.Subscription
	for _, s := range subs {
		if strings.Contains(strings.ToLower(s.Name), filter) ||
			strings.Contains(strings.ToLower(s.Category), filter) {
			out = append(out, s)
		}
	}
	return out
}

// sortSubs returns a copy of subs ordered by the given mode. Amount uses the
// normalized monthly cost in the default currency so multi-currency rows compare
// fairly.
func sortSubs(cfg Config, subs []model.Subscription, mode sortMode) []model.Subscription {
	out := make([]model.Subscription, len(subs))
	copy(out, subs)
	switch mode {
	case sortName:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
	case sortAmount:
		sort.SliceStable(out, func(i, j int) bool {
			return monthlyInDefault(cfg, out[i]) > monthlyInDefault(cfg, out[j])
		})
	default: // sortRenewal — zero dates sink to the bottom
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i].RenewalDate, out[j].RenewalDate
			if a.IsZero() != b.IsZero() {
				return !a.IsZero()
			}
			return a.Before(b.Time)
		})
	}
	return out
}

// dueRows returns active subscriptions renewing within the window, soonest first.
func dueRows(subs []model.Subscription, now time.Time, days int) []model.Subscription {
	var out []model.Subscription
	for _, s := range subs {
		if !s.Active() || s.RenewalDate.IsZero() {
			continue
		}
		d := int(s.RenewalDate.Sub(now).Hours() / 24)
		if d < 0 || d > days {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].RenewalDate.Before(out[j].RenewalDate.Time)
	})
	return out
}

// rowsForTab returns the selectable subscription rows for the active tab, and
// whether the tab is a selectable list at all.
func (m Model) rowsForTab() ([]model.Subscription, bool) {
	switch m.active {
	case tabSubs:
		return sortSubs(m.cfg, visibleSubs(m.subs, m.filter.Value()), m.sort), true
	case tabDue:
		return dueRows(m.subs, m.cfg.Clock.Now(), dueDays), true
	case tabZombies:
		out := make([]model.Subscription, len(m.zombies))
		for i, z := range m.zombies {
			out[i] = z.Sub
		}
		return out, true
	default:
		return nil, false
	}
}

// rowCount is the number of selectable rows on the active tab.
func (m Model) rowCount() int {
	rows, ok := m.rowsForTab()
	if !ok {
		return 0
	}
	return len(rows)
}

// selectedID returns the id of the currently selected row, if any.
func (m Model) selectedID() (string, bool) {
	rows, ok := m.rowsForTab()
	if !ok || m.cursor < 0 || m.cursor >= len(rows) {
		return "", false
	}
	return rows[m.cursor].ID, true
}

// clampCursor keeps the cursor inside the active row list after data changes.
func (m *Model) clampCursor() {
	n := m.rowCount()
	if n == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// scrollKey maps a +/-1 delta to the key the viewport understands.
func scrollKey(delta int) tea.KeyMsg {
	if delta < 0 {
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyDown}
}
