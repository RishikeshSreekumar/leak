package cmd

import (
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var hard bool
	cmd := &cobra.Command{
		Use:     "remove <id>",
		Aliases: []string{"rm"},
		Short:   "Cancel a subscription (or delete with --hard).",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			sub, err := resolveSub(d, args[0])
			if err != nil {
				return err
			}
			if hard {
				// A hard delete is the one irreversible command; snapshot first.
				if err := autoBackup(d, "remove --hard"); err != nil {
					return err
				}
				if err := d.Store.RemoveSub(sub.ID, d.Clock.Now()); err != nil {
					return err
				}
				fmt.Fprintf(d.Out, "%s Deleted %s\n", d.Render.Warn("✗"), sub.ID)
				autoSync(cmd)
				return nil
			}
			sub.Status = model.StatusCancelled
			sub.Touch(d.Clock.Now())
			if err := d.Store.UpdateSub(sub); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Cancelled %s\n", d.Render.Accent("✓"), sub.ID)
			autoSync(cmd)
			return nil
		},
	}
	cmd.Flags().BoolVar(&hard, "hard", false, "permanently delete instead of marking cancelled")
	return cmd
}
