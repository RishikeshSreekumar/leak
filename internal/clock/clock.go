// Package clock provides an injectable time source so command and audit logic
// stays deterministic under test.
package clock

import "time"

// Clock returns the current time. Production code uses Real; tests use Fake.
type Clock interface {
	Now() time.Time
}

// Real is the wall-clock implementation.
type Real struct{}

// Now returns the current wall-clock time.
func (Real) Now() time.Time { return time.Now() }

// Fake is a fixed-time Clock for deterministic tests.
type Fake struct{ T time.Time }

// Now returns the fixed time.
func (f Fake) Now() time.Time { return f.T }

// At builds a Fake pinned to the given date (UTC, midnight).
func At(year int, month time.Month, day int) Fake {
	return Fake{T: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}
