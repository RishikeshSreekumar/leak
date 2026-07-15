package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

func newEditCmd() *cobra.Command {
	var f subFlags
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a subscription (wizard, or use flags).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			sub, err := d.Store.GetSub(args[0])
			if err != nil {
				return err
			}
			if anyFlagSet(cmd) {
				f.applyTo(&sub, d.Profile)
			} else {
				if err := runSubForm(d, &sub, "Edit Subscription"); err != nil {
					if errors.Is(err, errCancelled) {
						fmt.Fprintln(d.Out, "Cancelled — no changes")
						return nil
					}
					return err
				}
			}
			sub.Touch(d.Clock.Now())
			if err := d.Store.UpdateSub(sub); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Updated %s\n", d.Render.Accent("✓"), sub.ID)
			return nil
		},
	}
	f.register(cmd)
	return cmd
}

// anyFlagSet reports whether the user passed at least one editable flag.
func anyFlagSet(cmd *cobra.Command) bool {
	set := false
	for _, name := range []string{"name", "amount", "currency", "cycle", "category", "payment", "renewal", "notes", "status"} {
		if cmd.Flags().Changed(name) {
			set = true
		}
	}
	return set
}
