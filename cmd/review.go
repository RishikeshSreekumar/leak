package cmd

import (
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

func newReviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review",
		Short: "Interactively confirm or flag subscriptions (mark phase).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			now := d.Clock.Now()
			queue := audit.NeedsReview(data.Subscriptions, now, d.Profile.ReviewAfterDays)
			if len(queue) == 0 {
				fmt.Fprintln(d.Out, d.Render.Accent("✓ Nothing to review. All fresh."))
				return nil
			}
			for i := range queue {
				sub := queue[i]
				choice := "skip"
				err := huh.NewSelect[string]().
					Title(sub.Name+"?").
					Options(
						huh.NewOption("Keep", "keep"),
						huh.NewOption("Cancel candidate", "cancel"),
						huh.NewOption("Skip", "skip"),
					).Value(&choice).Run()
				if err != nil {
					return err
				}
				if err := applyReviewChoice(d, sub.ID, choice); err != nil {
					return err
				}
			}
			fmt.Fprintln(d.Out, d.Render.Accent("✓ Review complete"))
			autoSync(cmd)
			return nil
		},
	}
}

func newMarkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mark <id>",
		Short: "Confirm a subscription as still in use (non-interactive).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			if err := applyReviewChoice(d, args[0], "keep"); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Confirmed %s\n", d.Render.Accent("✓"), args[0])
			autoSync(cmd)
			return nil
		},
	}
}

// applyReviewChoice updates a subscription per a keep/cancel/skip decision.
func applyReviewChoice(d *Deps, id, choice string) error {
	sub, err := d.Store.GetSub(id)
	if err != nil {
		return err
	}
	switch choice {
	case "keep":
		sub.LastConfirmed = model.Date{Time: d.Clock.Now()}
	case "cancel":
		sub.Status = model.StatusCancelled
	default:
		return nil // skip
	}
	sub.Touch(d.Clock.Now())
	return d.Store.UpdateSub(sub)
}
