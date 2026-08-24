// Package sync keeps a Leak registry converged across devices without a hosted
// backend. Local files stay the source of truth; sync is opt-in and no core
// command depends on it.
//
// # Model
//
// A Snapshot is the portable unit of synchronization: the subscription list,
// deletion tombstones, and the profile, serialized as JSON. It is the same
// shape `leak export --format json` emits, so any external tool — or an LLM —
// can read or produce one.
//
// # Merge
//
// Merge is per-record and field-agnostic. Every model.Subscription carries
// UpdatedAt + Rev (bumped by Subscription.Touch on every mutation), so the
// newer write wins (last-writer-wins), with Rev then id as deterministic
// tie-breakers. Deletions propagate through tombstones: a tombstone removes a
// record unless the record was updated *after* the delete, in which case the
// re-creation wins. Divergences are always reported as []Conflict, so
// `--strategy manual` can refuse to auto-resolve and let the user decide.
//
// # Transports
//
// A Transport is just "read a snapshot / write a snapshot":
//
//   - DirTransport — a directory (Dropbox, iCloud Drive, Syncthing, a USB key,
//     or an NFS mount). Zero infrastructure.
//   - GitTransport — any git remote. Backup, sync, and full history in one,
//     and the remote can be a private repo the user already owns.
//
// # Security
//
// Leak stores payment-method *labels* (e.g. "ICICI Amazon Pay"), never card
// numbers, so a snapshot carries no secrets. Notes are user-controlled free
// text and travel as-is — a redaction hook can strip them before Push if a
// user wants.
package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
)

// FormatVersion is the snapshot wire-format version. It is independent of the
// on-disk schema version so the two can evolve separately.
const FormatVersion = 1

// Snapshot is the portable, LLM-readable unit of synchronization.
type Snapshot struct {
	Format    int                  `json:"format"`
	Schema    int                  `json:"schema"`
	UpdatedAt time.Time            `json:"updated_at"`
	Device    string               `json:"device,omitempty"`
	Subs      []model.Subscription `json:"subscriptions"`
	Deleted   []model.Tombstone    `json:"deleted,omitempty"`
	Profile   model.Profile        `json:"profile"`
	Checksum  string               `json:"checksum"` // content hash for cheap diffing
}

// FromData builds a snapshot from a loaded registry and profile.
func FromData(d *model.Data, p model.Profile, now time.Time, device string) Snapshot {
	s := Snapshot{
		Format:    FormatVersion,
		Schema:    model.SchemaVersion,
		UpdatedAt: now.UTC(),
		Device:    device,
		Subs:      append([]model.Subscription(nil), d.Subscriptions...),
		Deleted:   append([]model.Tombstone(nil), d.Deleted...),
		Profile:   p,
	}
	// The transport config is device-local (one machine may sync via git, the
	// other via a shared folder) and never travels in a snapshot.
	s.Profile.Sync = model.SyncConfig{}
	s.Checksum = s.contentHash()
	return s
}

// Data converts a snapshot back into a registry.
func (s Snapshot) Data() *model.Data {
	return &model.Data{
		Version:       model.SchemaVersion,
		Subscriptions: append([]model.Subscription(nil), s.Subs...),
		Deleted:       append([]model.Tombstone(nil), s.Deleted...),
	}
}

// contentHash hashes the records only (not UpdatedAt/Device), so two devices
// holding identical data agree on the checksum and can skip a push.
func (s Snapshot) contentHash() string {
	payload := struct {
		Subs    []model.Subscription `json:"subscriptions"`
		Deleted []model.Tombstone    `json:"deleted"`
		Profile model.Profile        `json:"profile"`
	}{sortedSubs(s.Subs), sortedTombstones(s.Deleted), s.Profile}
	b, err := json.Marshal(payload)
	if err != nil { // marshaling plain structs cannot fail; be explicit anyway
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Marshal renders the snapshot as indented JSON (human-diffable in git).
func (s Snapshot) Marshal() ([]byte, error) {
	s.Checksum = s.contentHash()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Unmarshal parses a snapshot and verifies its checksum when one is present.
func Unmarshal(b []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return Snapshot{}, fmt.Errorf("parsing snapshot: %w", err)
	}
	if s.Format > FormatVersion {
		return Snapshot{}, fmt.Errorf("snapshot format v%d is newer than this leak understands (v%d) — upgrade leak",
			s.Format, FormatVersion)
	}
	if s.Checksum != "" && s.Checksum != s.contentHash() {
		return Snapshot{}, fmt.Errorf("snapshot checksum mismatch — remote file looks corrupted")
	}
	return s, nil
}

// Conflict describes a per-record divergence between two snapshots.
type Conflict struct {
	ID       string             `json:"id"`
	Local    model.Subscription `json:"local"`
	Remote   model.Subscription `json:"remote"`
	Resolved string             `json:"resolved"` // "local" | "remote" | "unresolved"
}

// Result is the outcome of a merge.
type Result struct {
	Merged    Snapshot   `json:"-"`
	Conflicts []Conflict `json:"conflicts,omitempty"`
	// Counts describe how the merged result differs from the LOCAL side.
	Added   int `json:"added"`   // records local did not have
	Updated int `json:"updated"` // records the remote's version won
	Removed int `json:"removed"` // records a remote tombstone deleted locally
	Pushed  int `json:"pushed"`  // records local wins that the remote lacks or trails
	// ProfileUpdated reports that the remote's profile (currencies, categories,
	// thresholds) replaced the local one.
	ProfileUpdated bool `json:"profile_updated,omitempty"`
}

// Changed reports whether the merge alters the local side.
func (r Result) Changed() bool { return r.Added+r.Updated+r.Removed > 0 || r.ProfileUpdated }

// Merge converges local and remote into one snapshot.
//
// base is the merge base from the last successful sync (see State). It is what
// separates a fast-forward from a real conflict: if only one side's copy of a
// record differs from the base, that side simply wins — no conflict, nothing to
// ask the user. Only when *both* sides moved away from the base (or there is no
// base at all) is the record genuinely diverged, and then strategy decides:
// model.StrategyLWW takes the newer write, model.StrategyManual keeps the local
// version and reports it unresolved.
func Merge(local, remote Snapshot, base State, strategy string, now time.Time) Result {
	res := Result{}

	// Tombstones from both sides, keyed by id, keeping the latest delete.
	tombs := map[string]time.Time{}
	for _, t := range append(append([]model.Tombstone(nil), local.Deleted...), remote.Deleted...) {
		if cur, ok := tombs[t.ID]; !ok || t.DeletedAt.After(cur) {
			tombs[t.ID] = t.DeletedAt.UTC()
		}
	}

	localByID := indexByID(local.Subs)
	remoteByID := indexByID(remote.Subs)

	ids := make([]string, 0, len(localByID)+len(remoteByID))
	for id := range localByID {
		ids = append(ids, id)
	}
	for id := range remoteByID {
		if _, ok := localByID[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	merged := make([]model.Subscription, 0, len(ids))
	for _, id := range ids {
		l, hasLocal := localByID[id]
		r, hasRemote := remoteByID[id]

		// A tombstone wins unless the surviving record was written after it.
		if del, ok := tombs[id]; ok {
			newest := latestWrite(hasLocal, l, hasRemote, r)
			if !newest.After(del) {
				if hasLocal {
					res.Removed++
				}
				continue
			}
			// Re-created after the delete: keep the record and drop the tombstone.
			delete(tombs, id)
		}

		switch {
		case hasLocal && !hasRemote:
			merged = append(merged, l)
			res.Pushed++
		case !hasLocal && hasRemote:
			merged = append(merged, r)
			res.Added++
		default:
			winner, side := resolve(l, r, base, strategy)
			if side == "conflict" {
				// Manual strategy: keep local untouched, hand the divergence back.
				res.Conflicts = append(res.Conflicts, Conflict{ID: id, Local: l, Remote: r, Resolved: "unresolved"})
				merged = append(merged, l)
				continue
			}
			if !sameContent(l, r) && diverged(l, r, base) {
				res.Conflicts = append(res.Conflicts, Conflict{ID: id, Local: l, Remote: r, Resolved: side})
			}
			merged = append(merged, winner)
			switch {
			case side == "remote":
				res.Updated++
			case !sameContent(l, r):
				res.Pushed++
			}
		}
	}

	out := Snapshot{
		Format:    FormatVersion,
		Schema:    model.SchemaVersion,
		UpdatedAt: now.UTC(),
		Device:    local.Device,
		Subs:      merged,
		Deleted:   tombstoneList(tombs),
		Profile:   mergeProfile(local, remote, base),
	}
	out.Checksum = out.contentHash()
	res.ProfileUpdated = profileKey(out.Profile) != profileKey(local.Profile)
	res.Merged = out
	return res
}

// mergeProfile resolves the profile whole-file: it is small, rarely edited on
// two devices at once, and record-level merging would add complexity for no
// practical gain. Snapshot timestamps cannot decide it — the local snapshot is
// generated fresh on every run and would always look newer — so the merge base
// does: the remote wins only when this device has not touched its profile since
// the last sync. Transport config stays device-local and never travels.
func mergeProfile(local, remote Snapshot, base State) model.Profile {
	p := local.Profile
	localUnchanged := base.Profile != "" && profileKey(local.Profile) == base.Profile
	remoteChanged := profileKey(remote.Profile) != base.Profile
	if localUnchanged && remoteChanged && remote.Profile.DefaultCurrency != "" {
		p = remote.Profile
	}
	p.Sync = local.Profile.Sync
	return p
}

// profileKey hashes a profile ignoring the device-local transport config, which
// is deliberately different on every machine.
func profileKey(p model.Profile) string {
	p.Sync = model.SyncConfig{}
	return hashOf(p)
}

// resolve picks the surviving version of a record present on both sides.
// It returns the winner and one of "local", "remote", or "conflict" (manual
// strategy on a genuine divergence).
func resolve(l, r model.Subscription, base State, strategy string) (model.Subscription, string) {
	if sameContent(l, r) {
		// Same data, possibly different bookkeeping: keep the higher rev so both
		// sides settle on one representation.
		return pick(l, r)
	}
	if !diverged(l, r, base) {
		// Exactly one side moved since the last sync — a fast-forward.
		if h, ok := base.Known(l.ID); ok && contentKey(l) == h {
			return r, "remote"
		}
		return l, "local"
	}
	if strategy == model.StrategyManual {
		return l, "conflict"
	}
	return pick(l, r)
}

// diverged reports whether both sides changed a record since the merge base.
// With no base entry (never synced, or a fresh device) any difference counts as
// divergence — Leak has no history to prove otherwise.
func diverged(l, r model.Subscription, base State) bool {
	h, ok := base.Known(l.ID)
	if !ok {
		return !sameContent(l, r)
	}
	return contentKey(l) != h && contentKey(r) != h
}

// pick resolves two versions of one record: newer UpdatedAt wins, then higher
// Rev, then the lexically greater checksum so both devices choose identically.
func pick(l, r model.Subscription) (model.Subscription, string) {
	switch {
	case r.UpdatedAt.After(l.UpdatedAt):
		return r, "remote"
	case l.UpdatedAt.After(r.UpdatedAt):
		return l, "local"
	case r.Rev > l.Rev:
		return r, "remote"
	case l.Rev > r.Rev:
		return l, "local"
	}
	if sameContent(l, r) {
		return l, "local"
	}
	// Fully tied but different: deterministic, not "whoever synced last".
	if hashOf(r) > hashOf(l) {
		return r, "remote"
	}
	return l, "local"
}

// latestWrite returns the newest UpdatedAt across the sides that hold a record.
func latestWrite(hasLocal bool, l model.Subscription, hasRemote bool, r model.Subscription) time.Time {
	var newest time.Time
	if hasLocal && l.UpdatedAt.After(newest) {
		newest = l.UpdatedAt
	}
	if hasRemote && r.UpdatedAt.After(newest) {
		newest = r.UpdatedAt
	}
	return newest
}

// sameContent compares two records ignoring sync bookkeeping.
func sameContent(a, b model.Subscription) bool {
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	a.Rev, b.Rev = 0, 0
	return hashOf(a) == hashOf(b)
}

func hashOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func indexByID(subs []model.Subscription) map[string]model.Subscription {
	m := make(map[string]model.Subscription, len(subs))
	for _, s := range subs {
		// Later entries win over earlier duplicates; ids are unique in practice.
		m[s.ID] = s
	}
	return m
}

func tombstoneList(tombs map[string]time.Time) []model.Tombstone {
	if len(tombs) == 0 {
		return nil
	}
	out := make([]model.Tombstone, 0, len(tombs))
	for id, at := range tombs {
		out = append(out, model.Tombstone{ID: id, DeletedAt: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedSubs(subs []model.Subscription) []model.Subscription {
	out := append([]model.Subscription(nil), subs...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedTombstones(ts []model.Tombstone) []model.Tombstone {
	out := append([]model.Tombstone(nil), ts...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DefaultDevice returns a best-effort label for this machine, recorded on
// pushed snapshots so `leak sync status` can say where a change came from.
func DefaultDevice() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "unknown"
}
