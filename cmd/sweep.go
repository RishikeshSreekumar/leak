package cmd

import (
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/spf13/cobra"
)

func newSweepCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sweep",
		Short: "Detect zombie subscriptions and potential savings.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			zs, sav := zombiesWithSavings(d, data.Subscriptions)
			fmt.Fprint(d.Out, d.Render.Sweep(zs, d.Profile.DefaultCurrency, sav))
			return nil
		},
	}
}

func newGCCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect zombies: cancel stale subscriptions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			zs, sav := zombiesWithSavings(d, data.Subscriptions)
			if len(zs) == 0 {
				fmt.Fprintln(d.Out, d.Render.Accent("✓ Nothing to collect."))
				return nil
			}
			// A bulk cancel is the most regrettable action in Leak; snapshot first.
			if apply {
				if err := autoBackup(d, "gc --apply"); err != nil {
					return err
				}
			}
			for _, z := range zs {
				if apply {
					sub := z.Sub
					sub.Status = model.StatusCancelled
					sub.Touch(d.Clock.Now())
					if err := d.Store.UpdateSub(sub); err != nil {
						return err
					}
					fmt.Fprintf(d.Out, "%s Cancelled %s\n", d.Render.Warn("✗"), z.Sub.Name)
				} else {
					fmt.Fprintf(d.Out, "would cancel %s (%d days stale)\n", z.Sub.Name, z.DaysSince)
				}
			}
			if !apply {
				fmt.Fprintf(d.Out, "\nDry run. Re-run with --apply to cancel %d subscription(s), saving %s/mo.\n",
					len(zs), money.Format(sav.Monthly, d.Profile.DefaultCurrency))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "actually cancel the zombies (default is a dry run)")
	return cmd
}

// zombiesWithSavings computes zombies plus savings rolled up into the default currency.
func zombiesWithSavings(d *Deps, subs []model.Subscription) ([]audit.Zombie, audit.Savings) {
	now := d.Clock.Now()
	zs := audit.Zombies(subs, now, d.Profile.StaleAfterDays)
	sav := audit.SavingsFor(zs, func(z audit.Zombie) float64 { return monthlyInDefault(d, z.Sub) })
	return zs, sav
}
