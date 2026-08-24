// Package store persists Leak's registry and profile as human-editable YAML
// under the user's config directory (or LEAK_CONFIG_DIR when set).
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"gopkg.in/yaml.v3"
)

// File names within the config directory.
const (
	subsFile    = "subscriptions.yaml"
	configFile  = "config.yaml"
	fxCacheFile = "fx_cache.yaml"
)

// ErrNotFound is returned when a subscription id does not exist.
var ErrNotFound = errors.New("subscription not found")

// Store is the persistence contract. Keeping it an interface lets commands and
// tests swap in fakes and lets the sync layer wrap it.
type Store interface {
	Load() (*model.Data, error)
	Save(*model.Data) error
	GetSub(id string) (model.Subscription, error)
	AddSub(model.Subscription) (model.Subscription, error)
	UpdateSub(model.Subscription) error
	// RemoveSub hard-deletes a subscription and records a tombstone stamped at
	// `at`, so the deletion survives a sync instead of being resurrected.
	RemoveSub(id string, at time.Time) error
	LoadProfile() (model.Profile, error)
	SaveProfile(model.Profile) error
	Dir() string
}

// YAMLStore is the on-disk implementation.
type YAMLStore struct{ dir string }

// ConfigDir resolves the base directory: LEAK_CONFIG_DIR if set, else
// <os user config dir>/leak.
func ConfigDir() (string, error) {
	if d := os.Getenv("LEAK_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "leak"), nil
}

// New builds a YAMLStore rooted at the resolved config dir, creating it and a
// default config on first run.
func New() (*YAMLStore, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return NewAt(dir)
}

// NewAt builds a YAMLStore rooted at dir (used by tests).
func NewAt(dir string) (*YAMLStore, error) {
	s := &YAMLStore{dir: dir}
	if err := s.bootstrap(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the store's root directory.
func (s *YAMLStore) Dir() string { return s.dir }

func (s *YAMLStore) path(name string) string { return filepath.Join(s.dir, name) }

// bootstrap creates the config dir and a default config.yaml if missing. The
// directory is owner-only: it holds what someone pays for and how, which is
// nobody else's business on a shared machine.
func (s *YAMLStore) bootstrap() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(s.path(configFile)); errors.Is(err, os.ErrNotExist) {
		if err := s.SaveProfile(model.DefaultProfile()); err != nil {
			return err
		}
	}
	return nil
}

// Load reads the subscription registry (empty registry if the file is absent).
func (s *YAMLStore) Load() (*model.Data, error) {
	b, err := os.ReadFile(s.path(subsFile))
	if errors.Is(err, os.ErrNotExist) {
		return &model.Data{}, nil
	}
	if err != nil {
		return nil, err
	}
	var d model.Data
	if err := yaml.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", subsFile, err)
	}
	d.Migrate()
	return &d, nil
}

// Save writes the registry atomically, stamping the current schema version.
func (s *YAMLStore) Save(d *model.Data) error {
	d.Version = model.SchemaVersion
	b, err := yaml.Marshal(d)
	if err != nil {
		return err
	}
	return writeAtomic(s.path(subsFile), b)
}

// GetSub returns a subscription by id.
func (s *YAMLStore) GetSub(id string) (model.Subscription, error) {
	d, err := s.Load()
	if err != nil {
		return model.Subscription{}, err
	}
	for _, sub := range d.Subscriptions {
		if sub.ID == id {
			return sub, nil
		}
	}
	return model.Subscription{}, ErrNotFound
}

// AddSub assigns a unique id (if empty) and appends the subscription.
func (s *YAMLStore) AddSub(sub model.Subscription) (model.Subscription, error) {
	d, err := s.Load()
	if err != nil {
		return sub, err
	}
	existing := make(map[string]bool, len(d.Subscriptions))
	for _, x := range d.Subscriptions {
		existing[x.ID] = true
	}
	if sub.ID == "" {
		sub.ID = Slugify(sub.Name)
	}
	sub.ID = uniqueID(sub.ID, existing)
	// A re-created id is an intentional resurrection; clear any old tombstone
	// so sync does not delete the new record.
	d.DropTombstone(sub.ID)
	d.Subscriptions = append(d.Subscriptions, sub)
	return sub, s.Save(d)
}

// UpdateSub replaces an existing subscription by id.
func (s *YAMLStore) UpdateSub(sub model.Subscription) error {
	d, err := s.Load()
	if err != nil {
		return err
	}
	for i, x := range d.Subscriptions {
		if x.ID == sub.ID {
			d.Subscriptions[i] = sub
			return s.Save(d)
		}
	}
	return ErrNotFound
}

// RemoveSub deletes a subscription by id and records a tombstone.
func (s *YAMLStore) RemoveSub(id string, at time.Time) error {
	d, err := s.Load()
	if err != nil {
		return err
	}
	out := d.Subscriptions[:0]
	found := false
	for _, x := range d.Subscriptions {
		if x.ID == id {
			found = true
			continue
		}
		out = append(out, x)
	}
	if !found {
		return ErrNotFound
	}
	d.Subscriptions = out
	d.Tombstone(id, at)
	return s.Save(d)
}

// LoadProfile reads config.yaml, falling back to defaults if absent.
func (s *YAMLStore) LoadProfile() (model.Profile, error) {
	b, err := os.ReadFile(s.path(configFile))
	if errors.Is(err, os.ErrNotExist) {
		return model.DefaultProfile(), nil
	}
	if err != nil {
		return model.Profile{}, err
	}
	var p model.Profile
	if err := yaml.Unmarshal(b, &p); err != nil {
		return model.Profile{}, fmt.Errorf("parsing %s: %w", configFile, err)
	}
	p.Normalize()
	return p, nil
}

// SubsPath exposes the registry file location (used by `leak doctor`).
func (s *YAMLStore) SubsPath() string { return s.path(subsFile) }

// ConfigPath exposes the profile file location (used by `leak doctor`).
func (s *YAMLStore) ConfigPath() string { return s.path(configFile) }

// SaveProfile writes config.yaml atomically.
func (s *YAMLStore) SaveProfile(p model.Profile) error {
	b, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return writeAtomic(s.path(configFile), b)
}

// FXCachePath exposes the fx cache location for the fx package.
func (s *YAMLStore) FXCachePath() string { return s.path(fxCacheFile) }

// writeAtomic writes via a temp file + rename so readers never see a partial file.
func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".leak-*.tmp")
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
	return os.Rename(tmpName, path)
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a name into a filesystem/id-friendly slug.
func Slugify(name string) string {
	s := slugRE.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "sub"
	}
	return s
}

// uniqueID appends -2, -3, ... until the id is free.
func uniqueID(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !taken[cand] {
			return cand
		}
	}
}
