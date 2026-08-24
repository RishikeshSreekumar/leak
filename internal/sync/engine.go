package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/store"
)

// pushAttempts bounds the fetch → merge → publish retry loop. A retry happens
// when another device published between our fetch and our push; three rounds is
// plenty for the single-user, few-devices case Leak targets.
const pushAttempts = 3

// ErrRemoteAhead is returned by Push when the remote holds changes the local
// registry does not, and --force was not given.
var ErrRemoteAhead = errors.New("remote has changes not in the local registry")

// Engine runs the sync cycle over a Store and a Transport. Local files stay the
// source of truth: nothing is written remotely until the local merge succeeds,
// and nothing is written locally until BeforeApply (typically a backup) runs.
type Engine struct {
	Store     store.Store
	Transport Transport
	Config    model.SyncConfig
	Now       func() time.Time
	// StatePath is where the merge base lives (see State). Empty disables it,
	// which makes every divergence look like a conflict.
	StatePath string
	// BeforeApply runs once, immediately before the first local write of a
	// cycle. Use it to snapshot the registry. Optional.
	BeforeApply func() error
}

// base loads the merge base for this device, tolerating a missing file.
func (e *Engine) base() State {
	if e.StatePath == "" {
		return State{Records: map[string]string{}}
	}
	st, err := LoadState(e.StatePath)
	if err != nil {
		return State{Records: map[string]string{}}
	}
	return st
}

// rebase records what the remote is now known to hold. Errors are surfaced:
// a lost base is not fatal, but silently dropping it would turn the next sync's
// clean fast-forwards into conflicts.
func (e *Engine) rebase(known Snapshot) error {
	if e.StatePath == "" {
		return nil
	}
	return SaveState(e.StatePath, StateFrom(known, e.now()))
}

// Report describes what one sync cycle did.
type Report struct {
	Transport   string `json:"transport"`
	Strategy    string `json:"strategy"`
	RemoteFound bool   `json:"remote_found"`
	Applied     bool   `json:"applied"`
	Published   bool   `json:"published"`
	UpToDate    bool   `json:"up_to_date"`
	DryRun      bool   `json:"dry_run"`
	Result
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) strategy() string {
	if e.Config.Strategy == model.StrategyManual {
		return model.StrategyManual
	}
	return model.StrategyLWW
}

func (e *Engine) device() string {
	if e.Config.Device != "" {
		return e.Config.Device
	}
	return DefaultDevice()
}

// localSnapshot reads the current registry + profile as a snapshot.
func (e *Engine) localSnapshot() (Snapshot, model.Profile, error) {
	data, err := e.Store.Load()
	if err != nil {
		return Snapshot{}, model.Profile{}, err
	}
	prof, err := e.Store.LoadProfile()
	if err != nil {
		return Snapshot{}, model.Profile{}, err
	}
	return FromData(data, prof, e.now(), e.device()), prof, nil
}

// fetch reads the remote snapshot, translating "nothing there yet" into a
// present-but-empty result rather than an error.
func (e *Engine) fetch(ctx context.Context) (Snapshot, bool, error) {
	remote, err := e.Transport.Fetch(ctx)
	if errors.Is(err, ErrNoRemote) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	return remote, true, nil
}

// apply writes a merged snapshot to local disk, taking the safety backup first.
func (e *Engine) apply(merged Snapshot, localProf model.Profile) error {
	if e.BeforeApply != nil {
		if err := e.BeforeApply(); err != nil {
			return fmt.Errorf("pre-sync backup: %w", err)
		}
	}
	if err := e.Store.Save(merged.Data()); err != nil {
		return err
	}
	prof := merged.Profile
	prof.Sync = localProf.Sync // transport config never travels
	if !sameProfile(prof, localProf) {
		if err := e.Store.SaveProfile(prof); err != nil {
			return err
		}
	}
	return nil
}

// stampSynced records the successful sync time on the local profile.
func (e *Engine) stampSynced(at time.Time) error {
	prof, err := e.Store.LoadProfile()
	if err != nil {
		return err
	}
	prof.Sync = e.Config
	prof.Sync.LastSynced = at.UTC()
	e.Config = prof.Sync
	return e.Store.SaveProfile(prof)
}

// Sync runs the full cycle: fetch, merge, write locally, publish. With dryRun
// it reports what would change and writes nothing anywhere.
func (e *Engine) Sync(ctx context.Context, dryRun bool) (Report, error) {
	rep := Report{Transport: e.Transport.Describe(), Strategy: e.strategy(), DryRun: dryRun}

	var lastErr error
	for attempt := 0; attempt < pushAttempts; attempt++ {
		local, localProf, err := e.localSnapshot()
		if err != nil {
			return rep, err
		}
		remote, found, err := e.fetch(ctx)
		if err != nil {
			return rep, err
		}
		rep.RemoteFound = found

		res := Merge(local, remote, e.base(), e.strategy(), e.now())
		rep.Result = res

		if unresolved(res.Conflicts) > 0 {
			return rep, conflictErr(res.Conflicts)
		}
		if dryRun {
			return rep, nil
		}

		if res.Changed() {
			if err := e.apply(res.Merged, localProf); err != nil {
				return rep, err
			}
			rep.Applied = true
		}
		// Skip the write entirely when the remote already holds this exact
		// content — otherwise every no-op sync would touch the file and make a
		// cloud-drive daemon re-upload it.
		if found && remote.Checksum != "" && remote.Checksum == res.Merged.Checksum {
			rep.UpToDate = true
		} else {
			if err := e.Transport.Publish(ctx, res.Merged, remote.Checksum); err != nil {
				lastErr = err
				continue // remote moved under us; refetch, merge again, retry
			}
			rep.Published = true
		}
		// Both sides now hold the merged snapshot: that is the new merge base.
		if err := e.rebase(res.Merged); err != nil {
			return rep, err
		}
		if err := e.stampSynced(e.now()); err != nil {
			return rep, err
		}
		return rep, nil
	}
	return rep, fmt.Errorf("sync: giving up after %d attempts: %w", pushAttempts, lastErr)
}

// Pull fetches and merges into the local registry without publishing.
func (e *Engine) Pull(ctx context.Context, dryRun bool) (Report, error) {
	rep := Report{Transport: e.Transport.Describe(), Strategy: e.strategy(), DryRun: dryRun}
	local, localProf, err := e.localSnapshot()
	if err != nil {
		return rep, err
	}
	remote, found, err := e.fetch(ctx)
	if err != nil {
		return rep, err
	}
	rep.RemoteFound = found
	if !found {
		return rep, nil
	}
	res := Merge(local, remote, e.base(), e.strategy(), e.now())
	rep.Result = res
	if unresolved(res.Conflicts) > 0 {
		return rep, conflictErr(res.Conflicts)
	}
	if dryRun {
		return rep, nil
	}
	if res.Changed() {
		if err := e.apply(res.Merged, localProf); err != nil {
			return rep, err
		}
		rep.Applied = true
	}
	// The remote was not written, so the base is what the remote still holds.
	if err := e.rebase(remote); err != nil {
		return rep, err
	}
	return rep, e.stampSynced(e.now())
}

// Push publishes the local registry. It refuses when the remote holds records
// the local side lacks, unless force is set — that check is what makes push
// safe to run from a device that has been offline.
func (e *Engine) Push(ctx context.Context, force, dryRun bool) (Report, error) {
	rep := Report{Transport: e.Transport.Describe(), Strategy: e.strategy(), DryRun: dryRun}
	local, _, err := e.localSnapshot()
	if err != nil {
		return rep, err
	}
	remote, found, err := e.fetch(ctx)
	if err != nil {
		return rep, err
	}
	rep.RemoteFound = found
	if found {
		res := Merge(local, remote, e.base(), e.strategy(), e.now())
		rep.Result = res
		if res.Changed() && !force {
			return rep, fmt.Errorf("%w — run `leak sync pull` first, or `leak sync push --force` to overwrite it", ErrRemoteAhead)
		}
	}
	if dryRun {
		return rep, nil
	}
	// Force means "this device wins": publish over whatever is there.
	expect := remote.Checksum
	if force {
		expect = OverwriteAny
	}
	if err := e.Transport.Publish(ctx, local, expect); err != nil {
		return rep, err
	}
	rep.Published = true
	// The remote now holds exactly this device's registry.
	if err := e.rebase(local); err != nil {
		return rep, err
	}
	return rep, e.stampSynced(e.now())
}

// Status reports how local and remote differ without writing anything.
func (e *Engine) Status(ctx context.Context) (Report, error) {
	return e.Sync(ctx, true)
}

// conflictErr explains what to do about records that need a human.
func conflictErr(cs []Conflict) error {
	ids := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.Resolved == "unresolved" {
			ids = append(ids, c.ID)
		}
	}
	return fmt.Errorf("%d record(s) changed on both devices: %s — inspect with "+
		"`leak sync status --json`, edit the version you want to keep (`leak edit <id>`), then re-run",
		len(ids), strings.Join(ids, ", "))
}

func unresolved(cs []Conflict) int {
	n := 0
	for _, c := range cs {
		if c.Resolved == "unresolved" {
			n++
		}
	}
	return n
}

func sameProfile(a, b model.Profile) bool { return hashOf(a) == hashOf(b) }
