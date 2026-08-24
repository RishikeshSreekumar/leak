package sync

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// device is one machine in a sync test: its own config dir and store, sharing a
// transport with the other devices.
type device struct {
	name  string
	store *store.YAMLStore
	eng   *Engine
	now   time.Time
}

func newDevice(t *testing.T, name string, tr Transport, strategy string) *device {
	t.Helper()
	st, err := store.NewAt(t.TempDir())
	require.NoError(t, err)
	d := &device{name: name, store: st, now: t0}
	d.eng = &Engine{
		Store:     st,
		Transport: tr,
		Config:    model.SyncConfig{Kind: model.SyncKindDir, Target: "shared", Strategy: strategy, Device: name},
		Now:       func() time.Time { return d.now },
		StatePath: filepath.Join(st.Dir(), StateFile),
	}
	return d
}

// add writes a subscription with sync bookkeeping stamped at the device clock.
func (d *device) add(t *testing.T, s model.Subscription) model.Subscription {
	t.Helper()
	s.Touch(d.now)
	saved, err := d.store.AddSub(s)
	require.NoError(t, err)
	return saved
}

func (d *device) sync(t *testing.T) Report {
	t.Helper()
	rep, err := d.eng.Sync(context.Background(), false)
	require.NoError(t, err)
	return rep
}

func (d *device) subs(t *testing.T) []model.Subscription {
	t.Helper()
	data, err := d.store.Load()
	require.NoError(t, err)
	return data.Subscriptions
}

func TestDirTransportRoundTrip(t *testing.T) {
	tr := &DirTransport{Path: filepath.Join(t.TempDir(), "shared")}

	_, err := tr.Fetch(context.Background())
	assert.ErrorIs(t, err, ErrNoRemote, "an empty target is a first sync, not a failure")

	s := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)
	require.NoError(t, tr.Publish(context.Background(), s, ""))

	got, err := tr.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Subs, 1)
	assert.Equal(t, "netflix", got.Subs[0].ID)
}

func TestDirTransportRefusesToClobberAConcurrentWrite(t *testing.T) {
	tr := &DirTransport{Path: filepath.Join(t.TempDir(), "shared")}
	first := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)
	require.NoError(t, tr.Publish(context.Background(), first, ""))

	// Another device published since we fetched: our `expect` is stale.
	second := snap([]model.Subscription{sub("spotify", 119, t1, 1)}, nil, t1)
	err := tr.Publish(context.Background(), second, "stale-checksum")
	assert.ErrorIs(t, err, ErrRemoteMoved)

	got, _ := tr.Fetch(context.Background())
	assert.Equal(t, []string{"netflix"}, ids(got.Subs), "the other device's snapshot survives")

	// The matching checksum publishes, and --force skips the check entirely.
	require.NoError(t, tr.Publish(context.Background(), second, first.Checksum))
	third := snap([]model.Subscription{sub("figma", 12, t2, 1)}, nil, t2)
	require.NoError(t, tr.Publish(context.Background(), third, OverwriteAny))
	got, _ = tr.Fetch(context.Background())
	assert.Equal(t, []string{"figma"}, ids(got.Subs))
}

func TestTwoDevicesConvergeThroughDirectory(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyLWW)

	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	rep := a.sync(t)
	assert.False(t, rep.RemoteFound, "laptop seeds the remote")
	assert.True(t, rep.Published)

	b.add(t, model.Subscription{Name: "Spotify", Amount: 119, Currency: "INR", Status: model.StatusActive})
	rep = b.sync(t)
	assert.True(t, rep.RemoteFound)
	assert.Equal(t, 1, rep.Added, "desktop pulls Netflix")

	// Laptop picks up Spotify on its next cycle: both devices now match.
	a.now = t1
	rep = a.sync(t)
	assert.Equal(t, 1, rep.Added)
	assert.Equal(t, []string{"netflix", "spotify"}, ids(a.subs(t)))
	assert.Equal(t, []string{"netflix", "spotify"}, ids(b.subs(t)))
}

func TestEditOnOneDevicePropagates(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyLWW)

	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	a.sync(t)
	b.sync(t)

	// Desktop raises the price later; laptop must adopt it.
	b.now = t1
	got, err := b.store.GetSub("netflix")
	require.NoError(t, err)
	got.Amount = 799
	got.Touch(b.now)
	require.NoError(t, b.store.UpdateSub(got))
	b.sync(t)

	a.now = t2
	rep := a.sync(t)
	assert.Equal(t, 1, rep.Updated)
	subs := a.subs(t)
	require.Len(t, subs, 1)
	assert.InDelta(t, 799.0, subs[0].Amount, 1e-9)
}

func TestDeletePropagatesAndDoesNotResurrect(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyLWW)

	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	a.sync(t)
	b.sync(t)
	require.Len(t, b.subs(t), 1)

	// Desktop hard-deletes; the tombstone travels with the next sync.
	b.now = t1
	require.NoError(t, b.store.RemoveSub("netflix", b.now))
	b.sync(t)

	a.now = t2
	rep := a.sync(t)
	assert.Equal(t, 1, rep.Removed)
	assert.Empty(t, a.subs(t))

	// A second round must not bring it back from either side.
	b.now = t2.Add(time.Hour)
	b.sync(t)
	a.now = t2.Add(2 * time.Hour)
	a.sync(t)
	assert.Empty(t, a.subs(t))
	assert.Empty(t, b.subs(t))
}

func TestManualStrategyStopsOnConflict(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyManual)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyManual)

	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	a.sync(t)
	b.sync(t)

	// Both devices edit the same record while apart.
	b.now = t1
	sb, _ := b.store.GetSub("netflix")
	sb.Amount = 799
	sb.Touch(b.now)
	require.NoError(t, b.store.UpdateSub(sb))
	b.sync(t)

	a.now = t2
	sa, _ := a.store.GetSub("netflix")
	sa.Amount = 999
	sa.Touch(a.now)
	require.NoError(t, a.store.UpdateSub(sa))

	_, err := a.eng.Sync(context.Background(), false)
	require.Error(t, err, "manual strategy must not silently overwrite")
	assert.ErrorContains(t, err, "changed on both devices: netflix")

	subs := a.subs(t)
	assert.InDelta(t, 999.0, subs[0].Amount, 1e-9, "local edit is preserved untouched")
}

func TestDryRunWritesNothing(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})

	rep, err := a.eng.Sync(context.Background(), true)
	require.NoError(t, err)
	assert.True(t, rep.DryRun)
	assert.False(t, rep.Published)

	_, err = (&DirTransport{Path: shared}).Fetch(context.Background())
	assert.ErrorIs(t, err, ErrNoRemote, "dry run must not publish")
}

func TestPushRefusesWhenRemoteIsAhead(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyLWW)

	b.add(t, model.Subscription{Name: "Spotify", Amount: 119, Currency: "INR", Status: model.StatusActive})
	b.sync(t)

	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	_, err := a.eng.Push(context.Background(), false, false)
	assert.ErrorIs(t, err, ErrRemoteAhead)

	// --force overwrites the remote with this device's registry.
	_, err = a.eng.Push(context.Background(), true, false)
	require.NoError(t, err)
	remote, err := (&DirTransport{Path: shared}).Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"netflix"}, ids(remote.Subs))
}

func TestBeforeApplyRunsBeforeLocalWrite(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyLWW)
	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	a.sync(t)

	called := 0
	b.eng.BeforeApply = func() error { called++; return nil }
	b.sync(t)
	assert.Equal(t, 1, called, "a merge that changes local must snapshot first")

	// A no-op sync should not churn backups.
	b.now = t1
	b.sync(t)
	assert.Equal(t, 1, called)
}

func TestSyncStampsLastSynced(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	a.sync(t)

	prof, err := a.store.LoadProfile()
	require.NoError(t, err)
	assert.True(t, t0.Equal(prof.Sync.LastSynced))
	assert.Equal(t, "laptop", prof.Sync.Device)
}

func TestGitTransportRoundTrip(t *testing.T) {
	if !GitAvailable() {
		t.Skip("git not on PATH")
	}
	remote := filepath.Join(t.TempDir(), "origin.git")
	require.NoError(t, exec.Command("git", "init", "-q", "--bare", "-b", "main", remote).Run())

	a := newDevice(t, "laptop", &GitTransport{Remote: remote, WorkDir: filepath.Join(t.TempDir(), "clone-a")}, model.StrategyLWW)
	b := newDevice(t, "desktop", &GitTransport{Remote: remote, WorkDir: filepath.Join(t.TempDir(), "clone-b")}, model.StrategyLWW)

	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	rep := a.sync(t)
	assert.False(t, rep.RemoteFound, "empty git remote is a first sync")
	assert.True(t, rep.Published)

	rep = b.sync(t)
	assert.True(t, rep.RemoteFound)
	assert.Equal(t, []string{"netflix"}, ids(b.subs(t)))

	// A change on the second device travels back through the same remote.
	b.now = t1
	b.add(t, model.Subscription{Name: "Spotify", Amount: 119, Currency: "INR", Status: model.StatusActive})
	b.sync(t)

	a.now = t2
	a.sync(t)
	assert.Equal(t, []string{"netflix", "spotify"}, ids(a.subs(t)))
}

func TestProfileChangePropagates(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyLWW)

	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	a.sync(t)
	b.sync(t)

	// Laptop adds a category; desktop has not touched its profile.
	a.now = t1
	prof, err := a.store.LoadProfile()
	require.NoError(t, err)
	prof.Categories = append(prof.Categories, "Gaming")
	require.NoError(t, a.store.SaveProfile(prof))
	a.sync(t)

	b.now = t2
	rep := b.sync(t)
	assert.True(t, rep.ProfileUpdated, "a remote profile edit must reach this device")

	got, err := b.store.LoadProfile()
	require.NoError(t, err)
	assert.Contains(t, got.Categories, "Gaming")
	assert.Equal(t, "desktop", got.Sync.Device, "the transport config stays device-local")
}

func TestLocalProfileEditIsNotClobbered(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	a := newDevice(t, "laptop", &DirTransport{Path: shared}, model.StrategyLWW)
	b := newDevice(t, "desktop", &DirTransport{Path: shared}, model.StrategyLWW)
	a.add(t, model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	a.sync(t)
	b.sync(t)

	// Both devices change the default currency while apart.
	a.now, b.now = t1, t1
	for dev, cur := range map[*device]string{a: "USD", b: "GBP"} {
		prof, err := dev.store.LoadProfile()
		require.NoError(t, err)
		prof.DefaultCurrency = cur
		require.NoError(t, dev.store.SaveProfile(prof))
	}
	a.sync(t)

	b.now = t2
	b.sync(t)
	got, err := b.store.LoadProfile()
	require.NoError(t, err)
	assert.Equal(t, "GBP", got.DefaultCurrency, "this device's own profile edit wins")
}
