package sync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
)

// StateFile is the device-local record of the last agreed-upon sync point. It
// never travels to a remote and is excluded from backups: it describes *this*
// machine's relationship to the remote, nothing about the user's data.
const StateFile = "sync_state.json"

// State is the merge base — the content of each record as of the last
// successful sync. It is what lets Leak tell "the other device changed this"
// apart from "we both changed this", which timestamps alone cannot do.
type State struct {
	UpdatedAt time.Time         `json:"updated_at"`
	Records   map[string]string `json:"records"`           // subscription id -> content hash
	Profile   string            `json:"profile,omitempty"` // hash of the profile at that point
}

// Known reports whether a base entry exists for id.
func (s State) Known(id string) (string, bool) {
	h, ok := s.Records[id]
	return h, ok
}

// StateFrom captures a snapshot as the new merge base.
func StateFrom(snap Snapshot, now time.Time) State {
	st := State{
		UpdatedAt: now.UTC(),
		Records:   make(map[string]string, len(snap.Subs)),
		Profile:   profileKey(snap.Profile),
	}
	for _, sub := range snap.Subs {
		st.Records[sub.ID] = contentKey(sub)
	}
	return st
}

// LoadState reads the merge base. A missing file yields an empty base, which
// makes the first sync conservative: every divergence is treated as a conflict
// because there is no shared history to reason from.
func LoadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Records: map[string]string{}}, nil
	}
	if err != nil {
		return State{Records: map[string]string{}}, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		// A corrupt base is recoverable: fall back to "no shared history".
		return State{Records: map[string]string{}}, nil
	}
	if s.Records == nil {
		s.Records = map[string]string{}
	}
	return s, nil
}

// SaveState writes the merge base atomically.
func SaveState(path string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".leak-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// contentKey hashes a record's user-visible content, ignoring the sync
// bookkeeping fields that change on every write.
func contentKey(s model.Subscription) string {
	s.UpdatedAt = time.Time{}
	s.Rev = 0
	return hashOf(s)
}
