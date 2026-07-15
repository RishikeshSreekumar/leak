// Package sync is a DESIGN-ONLY seam for future cloud/remote synchronization.
// Nothing here talks to a network yet — the interface and types exist so a real
// backend (git, object store, REST, or an MCP server exposing subscriptions to
// an LLM) can drop in without touching the rest of Leak.
//
// # Design
//
// Local-first is the source of truth. Sync is opt-in and never required for any
// core command. The wire format is JSON so any external tool or LLM can both
// read and produce a Snapshot — in fact `leak export --format json` already
// emits the same shape, so today an LLM can consume the registry directly and
// sync merely automates the round trip.
//
// # Change tracking
//
// Every model.Subscription already carries UpdatedAt + Rev (written on every
// mutation via Subscription.Touch). Those fields exist now, during the MVP, so
// a future merge can be field-agnostic and last-writer-wins without a data
// migration later.
//
// # Conflict strategy
//
// Default resolution is last-writer-wins per record keyed on UpdatedAt. A
// `--strategy manual` escape hatch would surface []Conflict for the user to
// resolve instead of auto-merging.
//
// # Transport candidates (any can satisfy Syncer)
//
//   - git repo — commit the YAML files and push (fits Leak's "git of
//     subscriptions" positioning).
//   - object store (S3/GCS) — a single JSON blob per user.
//   - generic REST endpoint — Push/Pull over HTTP.
//   - MCP server — expose subscriptions to an LLM as a live resource.
//
// # Security
//
// Leak stores payment-method *labels* (e.g. "ICICI Amazon Pay"), never card
// numbers, so snapshots are safe to sync. A redaction hook can strip Notes or
// other fields before Push if a user wants.
package sync

import (
	"context"
	"errors"

	"github.com/RishikeshSreekumar/leak/internal/model"
)

// ErrNotImplemented is returned by every stub method until a real backend lands.
var ErrNotImplemented = errors.New("sync: not implemented yet")

// Snapshot is the portable, LLM-readable unit of synchronization.
type Snapshot struct {
	Version   string               `json:"version"`
	UpdatedAt string               `json:"updated_at"`
	Subs      []model.Subscription `json:"subscriptions"`
	Profile   model.Profile        `json:"profile"`
	Checksum  string               `json:"checksum"` // content hash for cheap diffing
}

// Conflict describes a per-record divergence for manual resolution.
type Conflict struct {
	ID     string
	Local  model.Subscription
	Remote model.Subscription
}

// Syncer is the pluggable backend contract. Implementations are future work.
type Syncer interface {
	Push(ctx context.Context, snap Snapshot) (remoteVersion string, err error)
	Pull(ctx context.Context) (Snapshot, error)
	Resolve(local, remote Snapshot) (merged Snapshot, conflicts []Conflict)
}

// Noop is the default Syncer: every operation reports ErrNotImplemented.
type Noop struct{}

// Push always returns ErrNotImplemented.
func (Noop) Push(context.Context, Snapshot) (string, error) { return "", ErrNotImplemented }

// Pull always returns ErrNotImplemented.
func (Noop) Pull(context.Context) (Snapshot, error) { return Snapshot{}, ErrNotImplemented }

// Resolve returns the local snapshot unchanged with no detected conflicts.
func (Noop) Resolve(local, _ Snapshot) (Snapshot, []Conflict) { return local, nil }
