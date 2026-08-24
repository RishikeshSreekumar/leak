package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

func TestCreateCopiesRegistryFilesAndSkipsVolatile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "subscriptions.yaml", "subscriptions: []\n")
	writeFile(t, dir, "config.yaml", "default_currency: INR\n")
	writeFile(t, dir, "fx_cache.yaml", "cached: true\n")

	now := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	snap, err := Create(dir, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-24T09-30-00Z", snap.Name)

	_, err = os.Stat(filepath.Join(snap.Path, "subscriptions.yaml"))
	assert.NoError(t, err, "subscriptions.yaml should be snapshotted")
	_, err = os.Stat(filepath.Join(snap.Path, "config.yaml"))
	assert.NoError(t, err, "config.yaml should be snapshotted")
	_, err = os.Stat(filepath.Join(snap.Path, "fx_cache.yaml"))
	assert.True(t, os.IsNotExist(err), "volatile fx_cache.yaml must be excluded")
}

func TestCreateNothingToBackUp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fx_cache.yaml", "cached: true\n") // excluded → nothing snapshotable
	_, err := Create(dir, time.Now())
	assert.ErrorContains(t, err, "nothing to back up")
}

func TestListNewestFirst(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "subscriptions.yaml", "subscriptions: []\n")
	_, err := Create(dir, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	_, err = Create(dir, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	snaps, err := List(dir)
	require.NoError(t, err)
	require.Len(t, snaps, 2)
	assert.Equal(t, "2026-06-01T00-00-00Z", snaps[0].Name, "newest first")
	assert.Equal(t, "2026-01-01T00-00-00Z", snaps[1].Name)
}

func TestListEmptyWhenNoBackups(t *testing.T) {
	snaps, err := List(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

func TestRestoreOverwritesAndTakesSafetySnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "subscriptions.yaml", "version: 1\nold\n")

	snap, err := Create(dir, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	// Mutate current state, then restore the earlier snapshot.
	writeFile(t, dir, "subscriptions.yaml", "version: 1\nnew\n")
	target, err := Restore(dir, snap.Name, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), false)
	require.NoError(t, err)
	assert.Equal(t, snap.Name, target.Name)

	got, err := os.ReadFile(filepath.Join(dir, "subscriptions.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "old", "restore should bring back snapshot content")

	// Safety snapshot of the "new" state must have been taken.
	snaps, err := List(dir)
	require.NoError(t, err)
	assert.Len(t, snaps, 2, "restore should add a safety snapshot")
}

func TestRestoreLatestWhenNameEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "subscriptions.yaml", "v1\n")
	_, err := Create(dir, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	writeFile(t, dir, "subscriptions.yaml", "v2\n")
	latest, err := Create(dir, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	target, err := Restore(dir, "", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), true)
	require.NoError(t, err)
	assert.Equal(t, latest.Name, target.Name)
}

func TestRestoreUnknownName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "subscriptions.yaml", "v1\n")
	_, err := Create(dir, time.Now())
	require.NoError(t, err)
	_, err = Restore(dir, "nope", time.Now(), true)
	assert.ErrorContains(t, err, "not found")
}

func TestRestoreNoBackups(t *testing.T) {
	_, err := Restore(t.TempDir(), "", time.Now(), true)
	assert.ErrorContains(t, err, "no backups found")
}

func TestGitInitAndCommit(t *testing.T) {
	if !GitAvailable() {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	writeFile(t, dir, "subscriptions.yaml", "version: 1\n")

	require.NoError(t, GitInitAndCommit(dir, "first"))
	_, err := os.Stat(filepath.Join(dir, ".git"))
	assert.NoError(t, err, "repo should be initialised")

	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(gi), "backups/")
	assert.Contains(t, string(gi), "fx_cache.yaml")

	// Second call with no changes is a no-op success.
	assert.NoError(t, GitInitAndCommit(dir, "noop"))
}
