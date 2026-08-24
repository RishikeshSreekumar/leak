package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *YAMLStore {
	t.Helper()
	s, err := NewAt(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestBootstrapWritesDefaultConfig(t *testing.T) {
	s := newTestStore(t)
	_, err := os.Stat(filepath.Join(s.Dir(), configFile))
	assert.NoError(t, err, "config.yaml should exist after bootstrap")

	p, err := s.LoadProfile()
	require.NoError(t, err)
	assert.Equal(t, "INR", p.DefaultCurrency)
}

func TestAddGetUpdateRemove(t *testing.T) {
	s := newTestStore(t)

	sub, err := s.AddSub(model.Subscription{Name: "Netflix", Amount: 649, Currency: "INR", Status: model.StatusActive})
	require.NoError(t, err)
	assert.Equal(t, "netflix", sub.ID)

	got, err := s.GetSub("netflix")
	require.NoError(t, err)
	assert.EqualValues(t, 649, got.Amount)

	got.Amount = 699
	require.NoError(t, s.UpdateSub(got))
	got2, _ := s.GetSub("netflix")
	assert.EqualValues(t, 699, got2.Amount)

	require.NoError(t, s.RemoveSub("netflix", time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)))
	_, err = s.GetSub("netflix")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRemoveRecordsTombstoneAndReAddClearsIt(t *testing.T) {
	s := newTestStore(t)
	deletedAt := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	_, err := s.AddSub(model.Subscription{Name: "Netflix", Status: model.StatusActive})
	require.NoError(t, err)
	require.NoError(t, s.RemoveSub("netflix", deletedAt))

	d, err := s.Load()
	require.NoError(t, err)
	require.Len(t, d.Deleted, 1, "a hard delete must leave a tombstone for sync")
	assert.Equal(t, "netflix", d.Deleted[0].ID)
	assert.True(t, deletedAt.Equal(d.Deleted[0].DeletedAt))

	// Re-adding the same id is an intentional resurrection.
	_, err = s.AddSub(model.Subscription{Name: "Netflix"})
	require.NoError(t, err)
	d, err = s.Load()
	require.NoError(t, err)
	assert.Empty(t, d.Deleted, "re-created id should drop its tombstone")
}

func TestLoadProfileNormalizesMissingKeys(t *testing.T) {
	dir := t.TempDir()
	// A hand-edited config with only one key set — everything else must fall
	// back to defaults instead of zeros.
	require.NoError(t, os.WriteFile(filepath.Join(dir, configFile), []byte("default_currency: USD\n"), 0o644))
	s, err := NewAt(dir)
	require.NoError(t, err)

	p, err := s.LoadProfile()
	require.NoError(t, err)
	assert.Equal(t, "USD", p.DefaultCurrency)
	assert.Equal(t, 180, p.StaleAfterDays)
	assert.Equal(t, 90, p.ReviewAfterDays)
	assert.True(t, p.AutoBackupEnabled(), "auto-backup defaults on for pre-existing configs")
	assert.Equal(t, model.DefaultBackupKeep, p.BackupKeep)
}

func TestIDDedupe(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.AddSub(model.Subscription{Name: "Google One"})
	b, _ := s.AddSub(model.Subscription{Name: "Google One"})
	assert.Equal(t, "google-one", a.ID)
	assert.Equal(t, "google-one-2", b.ID)
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	d, err := s.Load()
	require.NoError(t, err)
	assert.Empty(t, d.Subscriptions)
}

func TestAtomicSavePersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewAt(dir)
	_, err := s.AddSub(model.Subscription{Name: "Spotify", Amount: 119, Currency: "INR"})
	require.NoError(t, err)

	// Fresh store over same dir must see it.
	s2, _ := NewAt(dir)
	d, _ := s2.Load()
	require.Len(t, d.Subscriptions, 1)
	assert.Equal(t, "Spotify", d.Subscriptions[0].Name)

	// No stray temp files left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".leak-")
	}
}

func TestUpdateMissing(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateSub(model.Subscription{ID: "ghost"})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSlugify(t *testing.T) {
	assert.Equal(t, "github-copilot", Slugify("GitHub Copilot"))
	assert.Equal(t, "adobe-creative-cloud", Slugify("Adobe Creative Cloud!"))
	assert.Equal(t, "sub", Slugify("!!!"))
}

func TestConfigDirEnvOverride(t *testing.T) {
	t.Setenv("LEAK_CONFIG_DIR", "/tmp/leak-test-xyz")
	d, err := ConfigDir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/leak-test-xyz", d)
}
