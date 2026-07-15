package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/clock"
	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/render"
	"github.com/RishikeshSreekumar/leak/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// harness wires injected deps over a temp store for deterministic command runs.
type harness struct {
	deps *Deps
	buf  *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.NewAt(t.TempDir())
	require.NoError(t, err)
	prof, _ := st.LoadProfile()
	buf := &bytes.Buffer{}
	return &harness{
		buf: buf,
		deps: &Deps{
			Store:   st,
			FX:      fx.Static{Rates: map[string]float64{"USD:INR": 86.0, "EUR:INR": 90.0}},
			Clock:   clock.At(2026, time.July, 15),
			Profile: prof,
			Out:     buf,
			In:      strings.NewReader(""),
			Render:  render.New(false),
		},
	}
}

// run executes leak with args and returns captured stdout.
func (h *harness) run(t *testing.T, args ...string) string {
	t.Helper()
	h.buf.Reset()
	root := NewRoot(h.deps)
	root.SetOut(h.buf)
	root.SetErr(h.buf)
	root.SetArgs(args)
	require.NoError(t, root.Execute(), "args=%v out=%s", args, h.buf.String())
	return h.buf.String()
}

func TestE2EAddListStatsFlow(t *testing.T) {
	h := newHarness(t)

	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR",
		"--category", "Entertainment", "--cycle", "monthly", "--renewal", "2026-07-21")
	h.run(t, "add", "--name", "GitHub Copilot", "--amount", "10", "--currency", "USD",
		"--category", "Development", "--cycle", "monthly", "--renewal", "2026-07-24")

	list := h.run(t, "list")
	assert.Contains(t, list, "Netflix")
	assert.Contains(t, list, "GitHub Copilot")
	assert.Contains(t, list, "$10/mo")

	stats := h.run(t, "stats")
	// 649 INR + 10 USD*86 = 649 + 860 = 1509
	assert.Contains(t, stats, "₹1,509")
	assert.Contains(t, stats, "Entertainment")
}

func TestE2EFXRecordStored(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "OpenAI", "--amount", "20", "--currency", "USD",
		"--category", "AI", "--cycle", "monthly", "--renewal", "2026-07-28")

	sub, err := h.deps.Store.GetSub("openai")
	require.NoError(t, err)
	require.Len(t, sub.BillingHistory, 1)
	assert.InDelta(t, 86.0, sub.BillingHistory[0].FXRate, 1e-9)
	assert.Equal(t, "static", sub.BillingHistory[0].FXSource)
	assert.Equal(t, 1, sub.Rev, "sync bookkeeping should be bumped on add")
}

func TestE2EReviewSweepGC(t *testing.T) {
	h := newHarness(t)
	// Add a stale sub (last_confirmed 200 days ago) directly via store.
	_, err := h.deps.Store.AddSub(model.Subscription{
		Name: "Adobe CC", Amount: 1675, Currency: "INR", BillingCycle: model.CycleMonthly,
		Category: "Development", Status: model.StatusActive,
		LastConfirmed: model.NewDate(2025, 12, 1), // ~226 days before 2026-07-15
	})
	require.NoError(t, err)

	sweep := h.run(t, "sweep")
	assert.Contains(t, sweep, "Adobe CC")
	assert.Contains(t, sweep, "zombie subscription(s) detected")

	// gc dry run does not mutate.
	gc := h.run(t, "gc")
	assert.Contains(t, gc, "would cancel Adobe CC")
	sub, _ := h.deps.Store.GetSub("adobe-cc")
	assert.Equal(t, model.StatusActive, sub.Status)

	// gc --apply cancels it.
	h.run(t, "gc", "--apply")
	sub, _ = h.deps.Store.GetSub("adobe-cc")
	assert.Equal(t, model.StatusCancelled, sub.Status)

	// After cancel, sweep is clean.
	assert.Contains(t, h.run(t, "sweep"), "No zombies")
}

func TestE2EMarkResetsStaleness(t *testing.T) {
	h := newHarness(t)
	_, _ = h.deps.Store.AddSub(model.Subscription{
		Name: "Spotify", Amount: 119, Currency: "INR", BillingCycle: model.CycleMonthly,
		Status: model.StatusActive, LastConfirmed: model.NewDate(2025, 1, 1),
	})
	assert.Contains(t, h.run(t, "sweep"), "Spotify")
	h.run(t, "mark", "spotify")
	assert.Contains(t, h.run(t, "sweep"), "No zombies")
}

func TestE2EImportExportRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR", "--renewal", "2026-07-21")

	jsonOut := h.run(t, "export", "--format", "json")
	assert.Contains(t, jsonOut, "\"name\": \"Netflix\"")

	csvOut := h.run(t, "export", "--format", "csv")
	assert.Contains(t, csvOut, "name,amount,currency")
	assert.Contains(t, csvOut, "Netflix,649,INR")
}

func TestE2EProfileCategoryPayment(t *testing.T) {
	h := newHarness(t)
	h.run(t, "category", "add", "Gaming")
	prof, _ := h.deps.Store.LoadProfile()
	assert.Contains(t, prof.Categories, "Gaming")

	h.run(t, "payment", "remove", "PayPal")
	prof, _ = h.deps.Store.LoadProfile()
	assert.NotContains(t, prof.PaymentMethods, "PayPal")
}

func TestE2ERemoveSoftAndHard(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Hulu", "--amount", "500", "--currency", "INR")
	h.run(t, "remove", "hulu")
	sub, _ := h.deps.Store.GetSub("hulu")
	assert.Equal(t, model.StatusCancelled, sub.Status)

	h.run(t, "remove", "hulu", "--hard")
	_, err := h.deps.Store.GetSub("hulu")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestE2EDueWindow(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Soon", "--amount", "100", "--currency", "INR", "--renewal", "2026-07-20")
	h.run(t, "add", "--name", "Later", "--amount", "100", "--currency", "INR", "--renewal", "2026-09-01")
	due := h.run(t, "due")
	assert.Contains(t, due, "Soon")
	assert.NotContains(t, due, "Later")
}
