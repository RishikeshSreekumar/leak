package sync

import (
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	t0 = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(24 * time.Hour)
	t2 = t0.Add(48 * time.Hour)
)

func sub(id string, amount float64, updated time.Time, rev int) model.Subscription {
	return model.Subscription{
		ID: id, Name: id, Amount: amount, Currency: "INR",
		BillingCycle: model.CycleMonthly, Status: model.StatusActive,
		UpdatedAt: updated, Rev: rev,
	}
}

func snap(subs []model.Subscription, tombs []model.Tombstone, at time.Time) Snapshot {
	s := Snapshot{Format: FormatVersion, Schema: model.SchemaVersion, UpdatedAt: at, Subs: subs, Deleted: tombs}
	s.Checksum = s.contentHash()
	return s
}

func ids(subs []model.Subscription) []string {
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, s.ID)
	}
	return out
}

func TestMergeUnionsBothSides(t *testing.T) {
	local := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)
	remote := snap([]model.Subscription{sub("spotify", 119, t0, 1)}, nil, t0)

	res := Merge(local, remote, State{}, model.StrategyLWW, t2)
	assert.Equal(t, []string{"netflix", "spotify"}, ids(res.Merged.Subs))
	assert.Equal(t, 1, res.Added, "spotify is new to local")
	assert.Equal(t, 1, res.Pushed, "netflix is new to the remote")
	assert.True(t, res.Changed())
}

func TestMergeLastWriterWins(t *testing.T) {
	local := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)
	remote := snap([]model.Subscription{sub("netflix", 799, t1, 2)}, nil, t1)

	res := Merge(local, remote, State{}, model.StrategyLWW, t2)
	require.Len(t, res.Merged.Subs, 1)
	assert.InDelta(t, 799.0, res.Merged.Subs[0].Amount, 1e-9, "newer remote write should win")
	assert.Equal(t, 1, res.Updated)
	require.Len(t, res.Conflicts, 1)
	assert.Equal(t, "remote", res.Conflicts[0].Resolved)
}

func TestMergeLocalWinsWhenNewer(t *testing.T) {
	local := snap([]model.Subscription{sub("netflix", 999, t2, 3)}, nil, t2)
	remote := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)

	res := Merge(local, remote, State{}, model.StrategyLWW, t2)
	assert.InDelta(t, 999.0, res.Merged.Subs[0].Amount, 1e-9)
	assert.Equal(t, 0, res.Updated)
	assert.Equal(t, 1, res.Pushed)
	assert.False(t, res.Changed(), "a local win does not modify local")
}

func TestMergeIdenticalRecordsAreNotConflicts(t *testing.T) {
	s := sub("netflix", 649, t0, 1)
	res := Merge(snap([]model.Subscription{s}, nil, t0), snap([]model.Subscription{s}, nil, t0), State{}, model.StrategyLWW, t2)
	assert.Empty(t, res.Conflicts)
	assert.False(t, res.Changed())
}

func TestMergeManualStrategyKeepsLocalAndFlags(t *testing.T) {
	local := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)
	remote := snap([]model.Subscription{sub("netflix", 799, t1, 2)}, nil, t1)

	res := Merge(local, remote, State{}, model.StrategyManual, t2)
	assert.InDelta(t, 649.0, res.Merged.Subs[0].Amount, 1e-9, "manual must not overwrite local")
	require.Len(t, res.Conflicts, 1)
	assert.Equal(t, "unresolved", res.Conflicts[0].Resolved)
	assert.Equal(t, 1, unresolved(res.Conflicts))
}

func TestMergeTombstoneDeletesRemotely(t *testing.T) {
	local := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)
	remote := snap(nil, []model.Tombstone{{ID: "netflix", DeletedAt: t1}}, t1)

	res := Merge(local, remote, State{}, model.StrategyLWW, t2)
	assert.Empty(t, res.Merged.Subs, "remote delete should propagate")
	assert.Equal(t, 1, res.Removed)
	assert.Len(t, res.Merged.Deleted, 1, "tombstone is retained so other devices converge too")
}

func TestMergeRecreationBeatsOlderTombstone(t *testing.T) {
	// Deleted on device A, then re-added on device B afterwards: the re-add wins
	// and the tombstone is dropped so it cannot delete the record again later.
	local := snap([]model.Subscription{sub("netflix", 649, t2, 1)}, nil, t2)
	remote := snap(nil, []model.Tombstone{{ID: "netflix", DeletedAt: t1}}, t1)

	res := Merge(local, remote, State{}, model.StrategyLWW, t2)
	require.Len(t, res.Merged.Subs, 1)
	assert.Empty(t, res.Merged.Deleted)
	assert.Equal(t, 0, res.Removed)
}

func TestMergeIsDeterministicOnFullTie(t *testing.T) {
	a := sub("netflix", 649, t0, 1)
	b := sub("netflix", 799, t0, 1)
	fwd := Merge(snap([]model.Subscription{a}, nil, t0), snap([]model.Subscription{b}, nil, t0), State{}, model.StrategyLWW, t2)
	rev := Merge(snap([]model.Subscription{b}, nil, t0), snap([]model.Subscription{a}, nil, t0), State{}, model.StrategyLWW, t2)
	assert.InDelta(t, fwd.Merged.Subs[0].Amount, rev.Merged.Subs[0].Amount, 1e-9,
		"both devices must pick the same winner")
}

func TestMergeFastForwardIsNotAConflict(t *testing.T) {
	// Base == local: only the remote moved, so the remote simply wins — even
	// under the manual strategy, which must not nag about one-sided edits.
	original := sub("netflix", 649, t0, 1)
	base := StateFrom(snap([]model.Subscription{original}, nil, t0), t0)

	updated := sub("netflix", 799, t1, 2)
	local := snap([]model.Subscription{original}, nil, t0)
	remote := snap([]model.Subscription{updated}, nil, t1)

	for _, strategy := range []string{model.StrategyLWW, model.StrategyManual} {
		res := Merge(local, remote, base, strategy, t2)
		assert.InDelta(t, 799.0, res.Merged.Subs[0].Amount, 1e-9, strategy)
		assert.Empty(t, res.Conflicts, "one-sided change is a fast-forward under %s", strategy)
		assert.Equal(t, 1, res.Updated, strategy)
	}
}

func TestMergeLocalOnlyChangeStaysLocal(t *testing.T) {
	// Base == remote: only this device moved, so nothing is applied locally and
	// the change is queued for the remote.
	original := sub("netflix", 649, t0, 1)
	base := StateFrom(snap([]model.Subscription{original}, nil, t0), t0)

	local := snap([]model.Subscription{sub("netflix", 999, t1, 2)}, nil, t1)
	remote := snap([]model.Subscription{original}, nil, t0)

	res := Merge(local, remote, base, model.StrategyManual, t2)
	assert.InDelta(t, 999.0, res.Merged.Subs[0].Amount, 1e-9)
	assert.Empty(t, res.Conflicts)
	assert.False(t, res.Changed())
	assert.Equal(t, 1, res.Pushed)
}

func TestMergeBothSidesChangedIsAConflict(t *testing.T) {
	base := StateFrom(snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0), t0)
	local := snap([]model.Subscription{sub("netflix", 999, t2, 2)}, nil, t2)
	remote := snap([]model.Subscription{sub("netflix", 799, t1, 2)}, nil, t1)

	lww := Merge(local, remote, base, model.StrategyLWW, t2)
	require.Len(t, lww.Conflicts, 1)
	assert.Equal(t, "local", lww.Conflicts[0].Resolved, "newer local write wins under lww")

	manual := Merge(local, remote, base, model.StrategyManual, t2)
	require.Len(t, manual.Conflicts, 1)
	assert.Equal(t, "unresolved", manual.Conflicts[0].Resolved)
}

func TestMergeProfileAdoptsRemoteWhenLocalUntouched(t *testing.T) {
	shared := model.Profile{DefaultCurrency: "INR"}

	local := snap(nil, nil, t0)
	local.Profile = shared
	local.Profile.Sync = model.SyncConfig{Kind: model.SyncKindDir, Target: "/local/only"}
	remote := snap(nil, nil, t1)
	remote.Profile = model.Profile{DefaultCurrency: "USD"}

	// Base == the shared profile: only the remote edited it, so the remote wins.
	base := StateFrom(Snapshot{Profile: shared}, t0)
	res := Merge(local, remote, base, model.StrategyLWW, t2)
	assert.Equal(t, "USD", res.Merged.Profile.DefaultCurrency)
	assert.True(t, res.ProfileUpdated)
	assert.True(t, res.Changed(), "a profile-only change still has to be written locally")
	assert.Equal(t, "/local/only", res.Merged.Profile.Sync.Target, "transport config stays device-local")
}

func TestMergeProfileKeepsLocalEdits(t *testing.T) {
	base := StateFrom(Snapshot{Profile: model.Profile{DefaultCurrency: "INR"}}, t0)

	local := snap(nil, nil, t0)
	local.Profile = model.Profile{DefaultCurrency: "GBP"} // edited here
	remote := snap(nil, nil, t1)
	remote.Profile = model.Profile{DefaultCurrency: "USD"} // and there

	res := Merge(local, remote, base, model.StrategyLWW, t2)
	assert.Equal(t, "GBP", res.Merged.Profile.DefaultCurrency, "a local profile edit is never clobbered")
	assert.False(t, res.ProfileUpdated)
}

func TestMergeProfileIgnoredWithoutABase(t *testing.T) {
	// First sync ever: there is no shared history, so the local profile stands.
	local := snap(nil, nil, t0)
	local.Profile = model.Profile{DefaultCurrency: "INR"}
	remote := snap(nil, nil, t1)
	remote.Profile = model.Profile{DefaultCurrency: "USD"}

	res := Merge(local, remote, State{}, model.StrategyLWW, t2)
	assert.Equal(t, "INR", res.Merged.Profile.DefaultCurrency)
	assert.False(t, res.ProfileUpdated)
}

func TestSnapshotRoundTripAndChecksum(t *testing.T) {
	d := &model.Data{Subscriptions: []model.Subscription{sub("netflix", 649, t0, 1)}}
	d.Tombstone("old", t0)
	s := FromData(d, model.DefaultProfile(), t1, "laptop")

	b, err := s.Marshal()
	require.NoError(t, err)
	got, err := Unmarshal(b)
	require.NoError(t, err)
	assert.Equal(t, s.Checksum, got.Checksum)
	assert.Equal(t, "laptop", got.Device)
	require.Len(t, got.Data().Subscriptions, 1)
	assert.Len(t, got.Data().Deleted, 1)
}

func TestUnmarshalRejectsCorruptedSnapshot(t *testing.T) {
	s := snap([]model.Subscription{sub("netflix", 649, t0, 1)}, nil, t0)
	b, err := s.Marshal()
	require.NoError(t, err)
	// Tamper with the payload; the stored checksum no longer matches.
	tampered := []byte(string(b))
	for i := range tampered {
		if tampered[i] == '6' && i+2 < len(tampered) && tampered[i+1] == '4' && tampered[i+2] == '9' {
			tampered[i] = '9'
			break
		}
	}
	_, err = Unmarshal(tampered)
	assert.ErrorContains(t, err, "checksum mismatch")
}

func TestUnmarshalRejectsNewerFormat(t *testing.T) {
	_, err := Unmarshal([]byte(`{"format": 99, "subscriptions": []}`))
	assert.ErrorContains(t, err, "newer than this leak understands")
}

func TestSnapshotOmitsTransportConfig(t *testing.T) {
	p := model.DefaultProfile()
	p.Sync = model.SyncConfig{Kind: model.SyncKindGit, Target: "git@example.com:me/private.git"}
	s := FromData(&model.Data{}, p, t0, "laptop")
	b, err := s.Marshal()
	require.NoError(t, err)
	assert.NotContains(t, string(b), "private.git", "sync target must not leak into the snapshot")
}
