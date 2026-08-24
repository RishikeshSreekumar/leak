package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
)

// SnapshotFile is the file name a transport reads and writes.
const SnapshotFile = "leak-snapshot.json"

// ErrNoRemote reports that the transport target holds no snapshot yet — a
// first sync, not a failure.
var ErrNoRemote = errors.New("sync: no snapshot at the remote yet")

// ErrRemoteMoved reports that the remote changed between the fetch a merge was
// based on and the attempt to publish it. The engine answers by merging again.
var ErrRemoteMoved = errors.New("sync: the remote changed while this sync ran")

// OverwriteAny is the `expect` value that skips the concurrent-write check —
// used by `leak sync push --force`, where overwriting is the point.
const OverwriteAny = "*"

// Transport moves snapshots between this machine and somewhere else. Both
// methods must be safe to call repeatedly; Fetch returning ErrNoRemote means
// "nothing there yet".
type Transport interface {
	// Describe returns a short human-readable target, e.g. "git git@host:me/subs".
	Describe() string
	// Fetch reads the remote snapshot, or ErrNoRemote if none exists.
	Fetch(ctx context.Context) (Snapshot, error)
	// Publish writes a snapshot to the remote. expect is the checksum of the
	// snapshot this one was merged from ("" when the remote was empty); a
	// transport that can detect a concurrent write returns ErrRemoteMoved
	// instead of overwriting it.
	Publish(ctx context.Context, s Snapshot, expect string) error
}

// NewTransport builds the transport described by a profile's sync config.
// workDir is a scratch directory the transport may use (the config dir).
func NewTransport(cfg model.SyncConfig, workDir string) (Transport, error) {
	switch cfg.Kind {
	case model.SyncKindDir:
		return &DirTransport{Path: cfg.Target}, nil
	case model.SyncKindGit:
		return &GitTransport{Remote: cfg.Target, WorkDir: filepath.Join(workDir, "sync-repo")}, nil
	case model.SyncKindNone:
		return nil, fmt.Errorf("sync is not configured — run `leak sync init --dir <path>` or `--git <remote>`")
	default:
		return nil, fmt.Errorf("unknown sync transport %q (dir|git)", cfg.Kind)
	}
}

// --- directory transport ---------------------------------------------------

// DirTransport syncs through a directory: a cloud-drive folder (Dropbox,
// iCloud, Drive), a Syncthing share, a NAS mount, or a USB key. It is the
// zero-infrastructure option — the folder's own sync engine moves the bytes.
type DirTransport struct{ Path string }

// Describe implements Transport.
func (d *DirTransport) Describe() string { return "dir " + d.Path }

func (d *DirTransport) file() string { return filepath.Join(d.Path, SnapshotFile) }

// Fetch implements Transport.
func (d *DirTransport) Fetch(context.Context) (Snapshot, error) {
	b, err := os.ReadFile(d.file())
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrNoRemote
	}
	if err != nil {
		return Snapshot{}, err
	}
	return Unmarshal(b)
}

// Publish implements Transport, writing atomically so a half-written file is
// never visible to the folder's own sync daemon. A shared folder offers no
// locking, so the current snapshot is checked against `expect` first: if another
// device published in the meantime, its work is reported rather than overwritten.
func (d *DirTransport) Publish(ctx context.Context, s Snapshot, expect string) error {
	if expect != OverwriteAny {
		switch current, err := d.Fetch(ctx); {
		case err == nil && current.Checksum != expect:
			return ErrRemoteMoved
		case errors.Is(err, ErrNoRemote) && expect != "":
			return ErrRemoteMoved // the snapshot we merged from was deleted
		case err != nil && !errors.Is(err, ErrNoRemote):
			return err
		}
	}
	if err := os.MkdirAll(d.Path, 0o700); err != nil {
		return err
	}
	b, err := s.Marshal()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(d.Path, ".leak-sync-*.tmp")
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
	return os.Rename(tmpName, d.file())
}

// --- git transport ---------------------------------------------------------

// GitTransport syncs through any git remote. Leak keeps its own clone under
// <config>/sync-repo and stores a single snapshot file there, so merging stays
// in Leak's record-level engine and git never has to resolve a text conflict.
type GitTransport struct {
	Remote  string
	WorkDir string
	Branch  string // defaults to "main"
}

// Describe implements Transport.
func (g *GitTransport) Describe() string { return "git " + g.Remote }

func (g *GitTransport) branch() string {
	if g.Branch != "" {
		return g.Branch
	}
	return "main"
}

// GitAvailable reports whether a git binary is on PATH.
func GitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Fetch implements Transport: sync the clone to the remote tip and read the
// snapshot file out of it.
func (g *GitTransport) Fetch(ctx context.Context) (Snapshot, error) {
	if err := g.ensureClone(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := g.pull(ctx); err != nil {
		return Snapshot{}, err
	}
	b, err := os.ReadFile(filepath.Join(g.WorkDir, SnapshotFile))
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, ErrNoRemote
	}
	if err != nil {
		return Snapshot{}, err
	}
	return Unmarshal(b)
}

// Publish implements Transport: write, commit, push. Git needs no `expect`
// check of its own — a non-fast-forward push is rejected by the remote, and the
// caller re-runs the whole fetch → merge → publish cycle rather than forcing.
func (g *GitTransport) Publish(ctx context.Context, s Snapshot, _ string) error {
	if err := g.ensureClone(ctx); err != nil {
		return err
	}
	b, err := s.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(g.WorkDir, SnapshotFile), b, 0o600); err != nil {
		return err
	}
	if err := g.run(ctx, "add", SnapshotFile); err != nil {
		return err
	}
	// Nothing staged → remote already matches; skip the commit and the push.
	if g.run(ctx, "diff", "--cached", "--quiet") == nil {
		return nil
	}
	msg := fmt.Sprintf("leak sync %s (%d subs)", s.UpdatedAt.Format(time.RFC3339), len(s.Subs))
	if err := g.run(ctx, "commit", "-q", "-m", msg); err != nil {
		return err
	}
	return g.run(ctx, "push", "-q", "origin", "HEAD:"+g.branch())
}

// ensureClone makes WorkDir a git repo wired to Remote. It tolerates an empty
// remote (a freshly created, branchless repo) by initialising locally instead.
func (g *GitTransport) ensureClone(ctx context.Context) error {
	if !GitAvailable() {
		return errors.New("git not found on PATH — install git or use `leak sync init --dir <path>`")
	}
	if _, err := os.Stat(filepath.Join(g.WorkDir, ".git")); err == nil {
		return g.run(ctx, "remote", "set-url", "origin", g.Remote)
	}
	if err := os.MkdirAll(g.WorkDir, 0o700); err != nil {
		return err
	}
	if err := g.run(ctx, "init", "-q", "-b", g.branch()); err != nil {
		return err
	}
	if err := g.run(ctx, "remote", "add", "origin", g.Remote); err != nil {
		return err
	}
	return nil
}

// pull fast-forwards the clone to the remote branch. A remote with no commits
// yet is not an error — it is simply the first sync.
func (g *GitTransport) pull(ctx context.Context) error {
	if err := g.run(ctx, "fetch", "-q", "origin", g.branch()); err != nil {
		if isMissingRemoteBranch(err) {
			return nil // empty remote: nothing to fast-forward to
		}
		return err
	}
	// Hard-reset is safe: the clone is Leak-owned scratch space holding exactly
	// one generated file, never user edits.
	return g.run(ctx, "reset", "-q", "--hard", "FETCH_HEAD")
}

func isMissingRemoteBranch(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "couldn't find remote ref") ||
		strings.Contains(msg, "no such ref") ||
		strings.Contains(msg, "not our ref")
}

// run executes git in WorkDir with prompts disabled, so a bad credential fails
// fast instead of hanging a non-interactive command forever.
func (g *GitTransport) run(ctx context.Context, args ...string) error {
	full := append([]string{
		// Commit even when the user has no global git identity configured.
		"-c", "user.name=leak", "-c", "user.email=leak@localhost",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = g.WorkDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
