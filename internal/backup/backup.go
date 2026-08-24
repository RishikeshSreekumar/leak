// Package backup provides timestamped snapshots of Leak's config directory and
// an optional git-repo transport. It is the Phase 1 spine for "trivially backed
// up and restored": snapshots are plain file copies under <configdir>/backups,
// so a user can inspect or restore them without any Leak-specific tooling.
//
// Snapshots capture the human-editable registry files at the top level of the
// config dir (subscriptions.yaml, config.yaml). Volatile caches (fx_cache.yaml),
// the backups tree itself, atomic-write temp files, and the .git dir are
// excluded so a snapshot is a clean point-in-time copy of user data.
package backup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// dirName is the subdirectory of the config dir that holds snapshots.
const dirName = "backups"

// snapshotLayout names snapshot dirs. It is colon-free so it is a valid path
// segment on every OS, and lexically sortable == chronologically sortable.
const snapshotLayout = "2006-01-02T15-04-05Z"

// ErrNothingToBackUp reports that the config dir held no user data to snapshot
// (a first run). Callers that back up defensively treat it as a no-op.
var ErrNothingToBackUp = errors.New("nothing to back up")

// IsNothingToBackUp reports whether err means "there was nothing to snapshot".
func IsNothingToBackUp(err error) bool { return errors.Is(err, ErrNothingToBackUp) }

// excluded lists top-level entries a snapshot never copies.
var excluded = map[string]bool{
	dirName:         true, // don't recurse backups into backups
	".git":          true, // git metadata, handled by the git transport
	"sync-repo":     true, // the sync transport's scratch clone
	"fx_cache.yaml": true, // volatile FX cache, not user data
}

// Snapshot describes one saved backup.
type Snapshot struct {
	Name string    // directory name, e.g. 2026-07-24T09-30-00Z
	Path string    // absolute path to the snapshot dir
	When time.Time // parsed from Name (UTC); zero if unparseable
}

// Dir returns the backups directory for a given config dir.
func Dir(configDir string) string { return filepath.Join(configDir, dirName) }

// Create snapshots the config dir's registry files into a new timestamped
// directory and returns it. now is used for the snapshot name (pass a clock).
func Create(configDir string, now time.Time) (Snapshot, error) {
	base := now.UTC().Format(snapshotLayout)
	name := base
	// Two snapshots in the same second (e.g. a safety snapshot during restore)
	// would collide; suffix -2, -3, … until the name is free.
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(Dir(configDir), name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	dest := filepath.Join(Dir(configDir), name)
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return Snapshot{}, err
	}
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return Snapshot{}, err
	}
	copied := 0
	for _, e := range entries {
		if e.IsDir() || !snapshotable(e.Name()) {
			continue
		}
		if err := copyFile(filepath.Join(configDir, e.Name()), filepath.Join(dest, e.Name())); err != nil {
			return Snapshot{}, err
		}
		copied++
	}
	if copied == 0 {
		// Nothing to back up — don't leave an empty dir lying around.
		os.Remove(dest)
		return Snapshot{}, fmt.Errorf("%w in %s", ErrNothingToBackUp, configDir)
	}
	return Snapshot{Name: name, Path: dest, When: now.UTC()}, nil
}

// Prune deletes all but the newest `keep` snapshots and returns those removed.
// keep <= 0 is a no-op, so "retain everything" needs no special-casing at the
// call site.
func Prune(configDir string, keep int) ([]Snapshot, error) {
	if keep <= 0 {
		return nil, nil
	}
	snaps, err := List(configDir)
	if err != nil || len(snaps) <= keep {
		return nil, err
	}
	var removed []Snapshot
	for _, s := range snaps[keep:] {
		if err := os.RemoveAll(s.Path); err != nil {
			return removed, err
		}
		removed = append(removed, s)
	}
	return removed, nil
}

// List returns saved snapshots, newest first.
func List(configDir string) ([]Snapshot, error) {
	entries, err := os.ReadDir(Dir(configDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		when, _ := time.Parse(snapshotLayout, e.Name())
		snaps = append(snaps, Snapshot{
			Name: e.Name(),
			Path: filepath.Join(Dir(configDir), e.Name()),
			When: when,
		})
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Name > snaps[j].Name })
	return snaps, nil
}

// Restore copies the files from snapshot `name` back into the config dir,
// overwriting current registry files. If name is empty the latest snapshot is
// used. It snapshots the current state first (a safety net) unless skipSafety.
func Restore(configDir, name string, now time.Time, skipSafety bool) (Snapshot, error) {
	snaps, err := List(configDir)
	if err != nil {
		return Snapshot{}, err
	}
	if len(snaps) == 0 {
		return Snapshot{}, fmt.Errorf("no backups found in %s", Dir(configDir))
	}
	var target Snapshot
	if name == "" {
		target = snaps[0]
	} else {
		found := false
		for _, s := range snaps {
			if s.Name == name {
				target, found = s, true
				break
			}
		}
		if !found {
			return Snapshot{}, fmt.Errorf("backup %q not found", name)
		}
	}
	// Safety snapshot of the current state so a restore is itself reversible.
	if !skipSafety {
		if _, err := Create(configDir, now); err != nil && !IsNothingToBackUp(err) {
			return Snapshot{}, fmt.Errorf("safety snapshot before restore: %w", err)
		}
	}
	entries, err := os.ReadDir(target.Path)
	if err != nil {
		return Snapshot{}, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyFile(filepath.Join(target.Path, e.Name()), filepath.Join(configDir, e.Name())); err != nil {
			return Snapshot{}, err
		}
	}
	return target, nil
}

// snapshotable reports whether a top-level file belongs in a snapshot.
func snapshotable(name string) bool {
	if excluded[name] {
		return false
	}
	if strings.HasPrefix(name, ".leak-") { // atomic-write temp files
		return false
	}
	return true
}

// copyFile copies src to dst atomically (temp file + rename), owner-only.
func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".leak-bak-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

// --- optional git-repo transport (Phase 1: backup + history via git) ---

// GitAvailable reports whether a git binary is on PATH.
func GitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// GitInitAndCommit initialises the config dir as a git repo (idempotent),
// writes a .gitignore excluding volatile/backup paths, stages everything, and
// commits when there is something to commit. This is the cheapest mechanism
// that gives both backup and history using existing tooling, per spec §3.1.
func GitInitAndCommit(configDir, message string) error {
	if !GitAvailable() {
		return fmt.Errorf("git not found on PATH")
	}
	if _, err := os.Stat(filepath.Join(configDir, ".git")); os.IsNotExist(err) {
		if err := git(configDir, "init", "-q"); err != nil {
			return err
		}
	}
	if err := ensureGitignore(configDir); err != nil {
		return err
	}
	if err := git(configDir, "add", "-A"); err != nil {
		return err
	}
	// Nothing staged → treat as a no-op success rather than a git error.
	if git(configDir, "diff", "--cached", "--quiet") == nil {
		return nil
	}
	return git(configDir, "commit", "-q", "-m", message)
}

func ensureGitignore(configDir string) error {
	path := filepath.Join(configDir, ".gitignore")
	const body = "# Managed by leak — volatile and local-only paths\n" +
		dirName + "/\nfx_cache.yaml\n.leak-*.tmp\n"
	if b, err := os.ReadFile(path); err == nil && string(b) == body {
		return nil
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func git(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
