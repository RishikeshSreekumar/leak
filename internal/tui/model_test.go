package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/clock"
	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLoadedModel builds a model over a seeded temp store and applies the first
// async load so aggregates are populated — the same fakes cmd/e2e_test.go uses.
func newLoadedModel(t *testing.T) Model {
	t.Helper()
	st, err := store.NewAt(t.TempDir())
	require.NoError(t, err)
	prof, _ := st.LoadProfile()

	// Two active subs due soon, plus one long-stale zombie.
	_, err = st.AddSub(model.Subscription{
		Name: "Netflix", Amount: 649, Currency: "INR", BillingCycle: model.CycleMonthly,
		Category: "Entertainment", Status: model.StatusActive, RenewalDate: model.NewDate(2026, 7, 21),
	})
	require.NoError(t, err)
	_, err = st.AddSub(model.Subscription{
		Name: "Copilot", Amount: 10, Currency: "USD", BillingCycle: model.CycleMonthly,
		Category: "Development", Status: model.StatusActive, RenewalDate: model.NewDate(2026, 7, 24),
	})
	require.NoError(t, err)
	_, err = st.AddSub(model.Subscription{
		Name: "Adobe", Amount: 1675, Currency: "INR", BillingCycle: model.CycleMonthly,
		Category: "Development", Status: model.StatusActive, LastConfirmed: model.NewDate(2025, 1, 1),
	})
	require.NoError(t, err)

	m := New(Config{
		Store:   st,
		FX:      fx.Static{Rates: map[string]float64{"USD:INR": 86.0}},
		Clock:   clock.At(2026, time.July, 15),
		Profile: prof,
		Color:   false,
	})
	return apply(t, m, m.loadCmd()())
}

// apply feeds a message through Update and returns the concrete model.
func apply(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

// press sends a key by its string form and returns the model and any command.
func press(t *testing.T, m Model, s string) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(runeKey(s))
	return updated.(Model), cmd
}

func runeKey(s string) tea.KeyMsg {
	switch s {
	case keyTab:
		return tea.KeyMsg{Type: tea.KeyTab}
	case keyEsc:
		return tea.KeyMsg{Type: tea.KeyEsc}
	case keyEnter:
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestLoadPopulatesAggregates(t *testing.T) {
	m := newLoadedModel(t)
	assert.False(t, m.loading)
	assert.Len(t, m.subs, 3)
	// 649 INR + 10 USD*86 + 1675 INR = 3184 (Adobe is active, counts in totals).
	assert.InDelta(t, 3184.0, m.report.MonthlyTotal, 0.01)
	assert.Len(t, m.zombies, 1) // only Adobe is stale
	assert.Equal(t, "Adobe", m.zombies[0].Sub.Name)
}

func TestTabSwitching(t *testing.T) {
	m := newLoadedModel(t)
	assert.Equal(t, tabOverview, m.active)

	m, _ = press(t, m, keyTab)
	assert.Equal(t, tabSubs, m.active)

	m, _ = press(t, m, "3")
	assert.Equal(t, tabDue, m.active)

	m, _ = press(t, m, "5")
	assert.Equal(t, tabZombies, m.active)
}

func TestCursorWrapsAndClamps(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2") // Subscriptions: 3 rows
	assert.Equal(t, 0, m.cursor)

	m, _ = press(t, m, keyK) // up from 0 wraps to last
	assert.Equal(t, 2, m.cursor)

	m, _ = press(t, m, keyJ) // down wraps back to 0
	assert.Equal(t, 0, m.cursor)
}

func TestFilterNarrowsRows(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2")       // Subscriptions
	m, _ = press(t, m, keyFilter) // enter filter mode
	assert.True(t, m.filtering)

	m, _ = press(t, m, "net") // matches Netflix only
	rows := visibleSubs(m.subs, m.filter.Value())
	require.Len(t, rows, 1)
	assert.Equal(t, "Netflix", rows[0].Name)

	m, _ = press(t, m, keyEsc) // clears filter
	assert.False(t, m.filtering)
	assert.Len(t, visibleSubs(m.subs, m.filter.Value()), 3)
}

func TestMarkConfirmsSelected(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "5") // Zombies tab: Adobe selected at cursor 0
	_, cmd := press(t, m, keyMark)
	require.NotNil(t, cmd)

	msg := cmd()
	done, ok := msg.(actionDoneMsg)
	require.True(t, ok)
	assert.Contains(t, done.status, "Adobe")

	sub, err := m.cfg.Store.GetSub("adobe")
	require.NoError(t, err)
	assert.False(t, sub.LastConfirmed.IsZero())
	assert.Equal(t, "2026-07-15", sub.LastConfirmed.String())
}

func TestCancelAndDelete(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2") // Subscriptions, cursor 0 = Netflix

	_, cmd := press(t, m, keyCancel)
	require.NotNil(t, cmd)
	cmd()
	sub, err := m.cfg.Store.GetSub("netflix")
	require.NoError(t, err)
	assert.Equal(t, model.StatusCancelled, sub.Status)

	_, cmd = press(t, m, keyDelete)
	require.NotNil(t, cmd)
	cmd()
	_, err = m.cfg.Store.GetSub("netflix")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRefreshAfterActionRecomputes(t *testing.T) {
	m := newLoadedModel(t)
	// Mark Adobe fresh, then apply the follow-up loadedMsg the refresh produces.
	m, _ = press(t, m, "5")
	_, cmd := press(t, m, keyMark)
	cmd() // performs the store mutation

	m2, refresh := m.Update(actionDoneMsg{status: "Confirmed Adobe"})
	m = m2.(Model)
	require.NotNil(t, refresh)
	m = apply(t, m, refresh())
	assert.Empty(t, m.zombies, "Adobe should no longer be stale after mark")
}

func TestBodiesRenderExpectedContent(t *testing.T) {
	m := newLoadedModel(t)
	assert.Contains(t, m.overviewBody(), "Monthly spend")
	assert.Contains(t, m.overviewBody(), "zombie")

	m.active = tabDue
	due := m.dueBody()
	assert.Contains(t, due, "Netflix")
	assert.Contains(t, due, "Copilot")

	insights := m.insightsBody()
	assert.Contains(t, insights, "Category Breakdown")
	assert.Contains(t, insights, "█") // bar chart rendered

	assert.Contains(t, m.zombiesBody(), "Adobe")
	assert.Contains(t, m.zombiesBody(), "days stale")
}

func TestSelectionMarkerColorless(t *testing.T) {
	m := newLoadedModel(t)
	m.active = tabSubs
	body := m.subsBody()
	assert.True(t, strings.Contains(body, "▸ Netflix"), "selected row should carry the marker: %q", body)
}

func TestSortCycleReorders(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2") // Subscriptions, default sort = renewal
	rows, _ := m.rowsForTab()
	assert.Equal(t, "Netflix", rows[0].Name, "earliest renewal first")

	m, _ = press(t, m, keySort) // -> name
	assert.Equal(t, sortName, m.sort)
	rows, _ = m.rowsForTab()
	assert.Equal(t, "Adobe", rows[0].Name, "alphabetical")

	m, _ = press(t, m, keySort) // -> amount (Adobe 1675 highest in base)
	assert.Equal(t, sortAmount, m.sort)
	rows, _ = m.rowsForTab()
	assert.Equal(t, "Adobe", rows[0].Name, "largest monthly first")
}

func TestReactivate(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2")
	_, cmd := press(t, m, keyCancel)
	cmd()
	sub, _ := m.cfg.Store.GetSub("netflix")
	require.Equal(t, model.StatusCancelled, sub.Status)

	_, cmd = press(t, m, keyReactivate)
	require.NotNil(t, cmd)
	cmd()
	sub, _ = m.cfg.Store.GetSub("netflix")
	assert.Equal(t, model.StatusActive, sub.Status)
	assert.False(t, sub.LastConfirmed.IsZero())
}

func TestDetailToggle(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2")
	m, _ = press(t, m, keyEnter) // open detail
	assert.True(t, m.detail)
	assert.Contains(t, m.detailBody(), "Netflix")
	assert.Contains(t, m.detailBody(), "Renewal")

	m, _ = press(t, m, keyEnter) // close
	assert.False(t, m.detail)
}

func TestAddFormOverlayOpens(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2")
	m, cmd := press(t, m, keyAdd)
	require.NotNil(t, m.form)
	assert.Equal(t, formAdd, m.form.mode)
	assert.NotNil(t, cmd) // form.Init
}

func TestEditFormOverlayOpensWithOriginal(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2") // cursor 0 = Netflix (earliest renewal)
	m, _ = press(t, m, keyEdit)
	require.NotNil(t, m.form)
	assert.Equal(t, formEdit, m.form.mode)
	assert.Equal(t, "netflix", m.form.orig.ID)
	assert.Equal(t, "649", m.form.amount)
}

func TestSaveAddPersistsWithFXRecord(t *testing.T) {
	m := newLoadedModel(t)
	sf := newSubForm(formAdd, model.Subscription{}, m.cfg.Profile)
	sf.name = "Figma"
	sf.amount = "12"
	sf.currencyPick = "USD"
	sf.cycle = model.CycleMonthly
	sf.renewal = "2026-08-10"

	msg := m.saveFormCmd(sf)()
	done, ok := msg.(actionDoneMsg)
	require.True(t, ok)
	assert.Contains(t, done.status, "Figma")

	sub, err := m.cfg.Store.GetSub("figma")
	require.NoError(t, err)
	assert.Equal(t, 12.0, sub.Amount)
	assert.Equal(t, model.StatusActive, sub.Status)
	assert.False(t, sub.LastConfirmed.IsZero())
	require.Len(t, sub.BillingHistory, 1)
	assert.InDelta(t, 86.0, sub.BillingHistory[0].FXRate, 1e-9)
	assert.Equal(t, 1, sub.Rev)
}

func TestSaveEditPreservesUntouchedFields(t *testing.T) {
	m := newLoadedModel(t)
	orig, err := m.cfg.Store.GetSub("adobe")
	require.NoError(t, err)

	sf := newSubForm(formEdit, orig, m.cfg.Profile)
	sf.amount = "1999" // bump price only

	msg := m.saveFormCmd(sf)()
	_, ok := msg.(actionDoneMsg)
	require.True(t, ok)

	sub, err := m.cfg.Store.GetSub("adobe")
	require.NoError(t, err)
	assert.Equal(t, 1999.0, sub.Amount)
	assert.Equal(t, orig.LastConfirmed.String(), sub.LastConfirmed.String(), "edit must preserve last_confirmed")
	assert.Greater(t, sub.Rev, orig.Rev, "edit bumps the revision")
}

func TestFormAbortClears(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, "2")
	m, _ = press(t, m, keyAdd)
	require.NotNil(t, m.form)

	// Ctrl+C aborts the huh form.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)
	assert.Nil(t, m.form)
	assert.Contains(t, m.status, "Cancelled")
}

func TestEmptyStateOnboarding(t *testing.T) {
	st, err := store.NewAt(t.TempDir())
	require.NoError(t, err)
	prof, _ := st.LoadProfile()
	m := New(Config{Store: st, FX: fx.Static{}, Clock: clock.At(2026, time.July, 15), Profile: prof})
	m = apply(t, m, m.loadCmd()())

	assert.Empty(t, m.subs)
	assert.Contains(t, m.overviewBody(), "add your first subscription")
	m.active = tabSubs
	assert.Contains(t, m.subsBody(), "add your first subscription")
}

func TestHelpModalToggle(t *testing.T) {
	m := newLoadedModel(t)
	m, _ = press(t, m, keyHelp)
	assert.True(t, m.help)
	assert.Contains(t, m.helpBody(), "Keyboard Shortcuts")
	assert.Contains(t, m.helpBody(), "esc to close")

	m, _ = press(t, m, keyEsc)
	assert.False(t, m.help)
}

func TestInsightsHasBarsAndSummary(t *testing.T) {
	m := newLoadedModel(t)
	body := m.insightsBody()
	assert.Contains(t, body, "Summary")
	assert.Contains(t, body, "Top Subscriptions")
	assert.Contains(t, body, "Payment Methods")
	assert.Contains(t, body, "█")
}
