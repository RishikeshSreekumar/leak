package sync

import (
	"context"
	"testing"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestNoopReportsNotImplemented(t *testing.T) {
	var s Noop
	_, err := s.Push(context.Background(), Snapshot{})
	assert.ErrorIs(t, err, ErrNotImplemented)
	_, err = s.Pull(context.Background())
	assert.ErrorIs(t, err, ErrNotImplemented)
}

func TestNoopResolveReturnsLocal(t *testing.T) {
	local := Snapshot{Version: "L", Subs: []model.Subscription{{ID: "a"}}}
	merged, conflicts := Noop{}.Resolve(local, Snapshot{Version: "R"})
	assert.Equal(t, "L", merged.Version)
	assert.Nil(t, conflicts)
}
