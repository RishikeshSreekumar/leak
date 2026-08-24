package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RishikeshSreekumar/leak/internal/backup"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTemp drops a fixture file next to the test and returns its path.
func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// statementCSV is a small bank export: one clean monthly subscription, one
// irregular merchant, and a salary credit.
const statementCSV = `Txn Date,Narration,Withdrawal Amt,Deposit Amt
12/04/2026,UPI/NETFLIX COM/1111,649.00,
15/04/2026,SWIGGY ORDER 8891,432.00,
12/05/2026,UPI/NETFLIX COM/2222,649.00,
28/05/2026,SWIGGY ORDER 9902,1180.00,
12/06/2026,POS 4321 NETFLIX.COM MUMBAI,649.00,
01/07/2026,SALARY JULY,,50000.00
12/07/2026,UPI/NETFLIX COM/4444,649.00,
`

func TestE2EScanDetectsAndApplies(t *testing.T) {
	h := newHarness(t)
	path := writeTemp(t, "statement.csv", statementCSV)

	out := h.run(t, "scan", path)
	assert.Contains(t, out, "Netflix")
	assert.NotContains(t, out, "Swiggy", "irregular spend is not a subscription")
	assert.Contains(t, out, "Dry run")

	data, err := h.deps.Store.Load()
	require.NoError(t, err)
	assert.Empty(t, data.Subscriptions, "a dry run must not write")

	out = h.run(t, "scan", path, "--apply")
	assert.Contains(t, out, "Added 1 subscription(s)")

	sub, err := h.deps.Store.GetSub("netflix")
	require.NoError(t, err)
	assert.InDelta(t, 649.0, sub.Amount, 1e-9)
	assert.Equal(t, model.CycleMonthly, sub.BillingCycle)
	assert.Equal(t, "Entertainment", sub.Category)
	assert.Contains(t, sub.Notes, "detected from statement")

	// Re-scanning must not duplicate what is already tracked.
	out = h.run(t, "scan", path, "--apply")
	assert.Contains(t, out, "skipped 1 already tracked")
}

func TestE2EScanJSON(t *testing.T) {
	h := newHarness(t)
	path := writeTemp(t, "statement.csv", statementCSV)

	var got struct {
		Transactions int `json:"transactions"`
		Candidates   []struct {
			Name        string  `json:"name"`
			Cycle       string  `json:"billing_cycle"`
			Confidence  float64 `json:"confidence"`
			AlreadyHave bool    `json:"already_tracked"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal([]byte(h.run(t, "scan", path, "--json")), &got))
	assert.Equal(t, 6, got.Transactions, "credits are excluded")
	require.Len(t, got.Candidates, 1)
	assert.Equal(t, "Netflix", got.Candidates[0].Name)
	assert.False(t, got.Candidates[0].AlreadyHave)
}

func TestE2EDoctorFindsAndFixesProblems(t *testing.T) {
	h := newHarness(t)
	// A record straight from a hand-edited YAML file: bad cycle, no currency.
	_, err := h.deps.Store.AddSub(model.Subscription{
		Name: "Broken", Amount: 100, BillingCycle: "fortnightly", Status: model.StatusActive,
	})
	require.NoError(t, err)

	out := h.run(t, "doctor")
	assert.Contains(t, out, "billing cycle")
	assert.Contains(t, out, "currency")
	assert.Contains(t, out, "error(s)")

	h.run(t, "doctor", "--fix")
	sub, err := h.deps.Store.GetSub("broken")
	require.NoError(t, err)
	assert.Equal(t, model.CycleMonthly, sub.BillingCycle)
	assert.Equal(t, "INR", sub.Currency)

	// The fix is snapshotted, so it is reversible.
	snaps, err := backup.List(h.deps.Store.Dir())
	require.NoError(t, err)
	assert.NotEmpty(t, snaps)
}

func TestE2EDoctorJSONIsClean(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR",
		"--category", "Entertainment", "--renewal", "2026-08-01")

	var got struct {
		Findings []struct {
			Severity string `json:"severity"`
			Subject  string `json:"subject"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal([]byte(h.run(t, "doctor", "--json")), &got))
	for _, f := range got.Findings {
		assert.NotEqual(t, "error", f.Severity, "a freshly added subscription must be clean: %s", f.Subject)
	}
}

func TestE2EBackupRestorePrune(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR")
	h.run(t, "backup")

	// Delete everything, then restore from the snapshot.
	h.run(t, "remove", "netflix", "--hard")
	_, err := h.deps.Store.GetSub("netflix")
	require.Error(t, err)

	out := h.run(t, "restore")
	assert.Contains(t, out, "Restored from")
	_, err = h.deps.Store.GetSub("netflix")
	assert.NoError(t, err, "restore should bring the subscription back")

	out = h.run(t, "backup", "prune", "--keep", "1")
	assert.Contains(t, out, "Pruned")
	snaps, err := backup.List(h.deps.Store.Dir())
	require.NoError(t, err)
	assert.Len(t, snaps, 1)
}

func TestE2EHardDeleteLeavesTombstone(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR")
	h.run(t, "remove", "netflix", "--hard")

	data, err := h.deps.Store.Load()
	require.NoError(t, err)
	require.Len(t, data.Deleted, 1)
	assert.Equal(t, "netflix", data.Deleted[0].ID)
}

func TestE2EImportDryRunAndUpdate(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR")
	path := writeTemp(t, "subs.csv",
		"name,amount,currency,billing_cycle,category\n"+
			"Netflix,799,INR,monthly,Entertainment\n"+
			"Spotify,119,INR,monthly,Entertainment\n")

	out := h.run(t, "import", path, "--dry-run")
	assert.Contains(t, out, "duplicate")
	assert.Contains(t, out, "Spotify")
	assert.Contains(t, out, "Dry run")
	sub, _ := h.deps.Store.GetSub("netflix")
	assert.InDelta(t, 649.0, sub.Amount, 1e-9, "dry run must not write")

	out = h.run(t, "import", path)
	assert.Contains(t, out, "Imported 1, updated 0, skipped 1")

	out = h.run(t, "import", path, "--update")
	assert.Contains(t, out, "updated 2")
	sub, _ = h.deps.Store.GetSub("netflix")
	assert.InDelta(t, 799.0, sub.Amount, 1e-9, "--update should overwrite the existing record")
	assert.NotEmpty(t, sub.BillingHistory, "an import must not wipe Leak's own FX history")
}

func TestE2EJSONOutputs(t *testing.T) {
	h := newHarness(t)
	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR",
		"--category", "Entertainment", "--renewal", "2026-07-20")

	var subs []model.Subscription
	require.NoError(t, json.Unmarshal([]byte(h.run(t, "list", "--json")), &subs))
	require.Len(t, subs, 1)
	assert.Equal(t, "Netflix", subs[0].Name)

	var due []model.Subscription
	require.NoError(t, json.Unmarshal([]byte(h.run(t, "due", "--json")), &due))
	assert.Len(t, due, 1)

	var stats map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.run(t, "stats", "--json")), &stats))
	assert.Contains(t, stats, "monthly_total")

	var one model.Subscription
	require.NoError(t, json.Unmarshal([]byte(h.run(t, "show", "netflix", "--json")), &one))
	assert.Equal(t, "netflix", one.ID)
}

func TestE2ESyncRoundTripThroughDirectory(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")

	// Device A: configure sync and publish.
	a := newHarness(t)
	out := a.run(t, "sync", "init", "--dir", shared)
	assert.Contains(t, out, "Sync configured")
	a.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR")

	out = a.run(t, "sync")
	assert.Contains(t, out, "Published to")

	// Device B: a separate config dir pointed at the same folder.
	b := newHarness(t)
	b.run(t, "sync", "init", "--dir", shared)
	out = b.run(t, "sync")
	assert.Contains(t, out, "1 added")

	sub, err := b.deps.Store.GetSub("netflix")
	require.NoError(t, err)
	assert.InDelta(t, 649.0, sub.Amount, 1e-9)

	// Status on a converged pair reports no pending changes.
	out = b.run(t, "sync", "status")
	assert.Contains(t, out, "0 added, 0 updated, 0 removed")
}

func TestE2ESyncStatusWithoutConfig(t *testing.T) {
	h := newHarness(t)
	out := h.run(t, "sync", "status")
	assert.Contains(t, out, "Sync is not configured")

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.run(t, "sync", "status", "--json")), &got))
	assert.Equal(t, false, got["enabled"])
}

func TestE2ESyncInitValidatesFlags(t *testing.T) {
	h := newHarness(t)
	root := NewRoot(h.deps)
	root.SetOut(h.buf)
	root.SetErr(h.buf)
	root.SetArgs([]string{"sync", "init"})
	err := root.Execute()
	assert.ErrorContains(t, err, "exactly one of --dir")
}

func TestE2ESyncDisableKeepsData(t *testing.T) {
	h := newHarness(t)
	shared := filepath.Join(t.TempDir(), "shared")
	h.run(t, "sync", "init", "--dir", shared)
	h.run(t, "add", "--name", "Netflix", "--amount", "649", "--currency", "INR")
	h.run(t, "sync")

	h.run(t, "sync", "disable")
	prof, err := h.deps.Store.LoadProfile()
	require.NoError(t, err)
	assert.False(t, prof.Sync.Enabled())
	_, err = h.deps.Store.GetSub("netflix")
	assert.NoError(t, err)
}

func TestE2ESyncInitResolvesRelativeDir(t *testing.T) {
	h := newHarness(t)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	h.run(t, "sync", "init", "--dir", "./relative-sync-target")

	prof, err := h.deps.Store.LoadProfile()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cwd, "relative-sync-target"), prof.Sync.Target,
		"a relative target must be resolved now, not when a later command runs elsewhere")
}

func TestE2ESyncInitKeepsGitURLIntact(t *testing.T) {
	h := newHarness(t)
	const remote = "git@github.com:me/subs.git"
	h.run(t, "sync", "init", "--git", remote)

	prof, err := h.deps.Store.LoadProfile()
	require.NoError(t, err)
	assert.Equal(t, remote, prof.Sync.Target)
}
