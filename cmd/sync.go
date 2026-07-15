package cmd

import (
	"github.com/RishikeshSreekumar/leak/internal/sync"
	"github.com/spf13/cobra"
)

// newSyncCmd exposes the cloud-sync seam. It is hidden and wired to the Noop
// backend: every subcommand reports that sync is not implemented yet. See
// internal/sync for the full design.
func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "sync",
		Short:  "Cloud sync (not implemented — see internal/sync design).",
		Hidden: true,
	}
	backend := sync.Noop{}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "push",
			Short: "Push local registry to a remote.",
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, err := backend.Push(cmd.Context(), sync.Snapshot{})
				return err
			},
		},
		&cobra.Command{
			Use:   "pull",
			Short: "Pull registry from a remote.",
			RunE: func(cmd *cobra.Command, _ []string) error {
				_, err := backend.Pull(cmd.Context())
				return err
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show sync status.",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return sync.ErrNotImplemented
			},
		},
	)
	return cmd
}
