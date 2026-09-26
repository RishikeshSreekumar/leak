package cmd

import (
	"errors"
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/spf13/cobra"
)

// ErrSomethingDue is returned by `leak due --quiet` when the window is not
// empty, so a shell hook can branch on the exit status. Execute prints nothing
// for it: the one-line summary already went to stdout.
var ErrSomethingDue = errors.New("something is due")

func newDueCmd() *cobra.Command {
	var (
		days  int
		quiet bool
	)
	cmd := &cobra.Command{
		Use:   "due",
		Short: "Show upcoming renewals and trial conversions.",
		Long: "List what will charge you inside the look-ahead window. Renewal dates roll " +
			"forward by billing cycle automatically, and a subscription still in its free " +
			"trial shows the trial end instead — that is the day to cancel by.\n\n" +
			"--quiet prints a single line (nothing at all when the window is empty) and " +
			"exits 1 when something is due, so it drops into a shell prompt or rc file.",
		Example: "  leak due --days 7\n" +
			"  leak due --days 7 --quiet     # add to ~/.zshrc for a login nag",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			items := insights.UpcomingWithin(data.Subscriptions, d.Clock.Now(), days)
			switch {
			case jsonRequested(cmd):
				return writeJSON(d, items)
			case quiet:
				fmt.Fprint(d.Out, d.Render.DueOneLine(items, days))
				if len(items) > 0 {
					return ErrSomethingDue
				}
				return nil
			}
			fmt.Fprint(d.Out, d.Render.Due(items, days))
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "look-ahead window in days")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "one line or nothing; exit 1 when something is due")
	jsonFlag(cmd)
	return cmd
}

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Monthly spend and category breakdown.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			r := insights.Build(data.Subscriptions, d.Profile, d.FX, d.Clock.Now())
			if jsonRequested(cmd) {
				return writeJSON(d, r)
			}
			fmt.Fprint(d.Out, d.Render.Stats(r))
			return nil
		},
	}
	jsonFlag(cmd)
	return cmd
}

func newInsightsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Actionable spending insights.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			now := d.Clock.Now()
			r := insights.Build(data.Subscriptions, d.Profile, d.FX, now)
			zs := audit.Zombies(data.Subscriptions, now, d.Profile.StaleAfterDays)
			sav := audit.SavingsFor(zs, func(z audit.Zombie) float64 { return monthlyInDefault(d, z.Sub) })
			if jsonRequested(cmd) {
				return writeJSON(d, map[string]any{
					"report":  r,
					"zombies": len(zs),
					"savings": sav,
				})
			}
			fmt.Fprint(d.Out, d.Render.Insights(r, len(zs), sav))
			return nil
		},
	}
	jsonFlag(cmd)
	return cmd
}

func newCategoriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "categories",
		Short:  "List categories with counts and spend.",
		Hidden: true, // same numbers as `leak stats`; kept for scripts
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			r := insights.Build(data.Subscriptions, d.Profile, d.FX, d.Clock.Now())
			if jsonRequested(cmd) {
				return writeJSON(d, r.ByCategory)
			}
			for _, c := range r.ByCategory {
				fmt.Fprintf(d.Out, "%-16s%s  (%.0f%%)\n", c.Category, money.Format(c.Monthly, r.Currency), c.Percent)
			}
			return nil
		},
	}
	jsonFlag(cmd)
	return cmd
}

func newPaymentMethodsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "payment-methods",
		Short:  "List payment methods with counts and spend.",
		Hidden: true, // same numbers as `leak insights`; kept for scripts
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			r := insights.Build(data.Subscriptions, d.Profile, d.FX, d.Clock.Now())
			if jsonRequested(cmd) {
				return writeJSON(d, r.ByPaymentMethod)
			}
			for _, m := range r.ByPaymentMethod {
				fmt.Fprintf(d.Out, "%-18s %d subs  %s\n", m.Method, m.Count, money.Format(m.Monthly, r.Currency))
			}
			return nil
		},
	}
	jsonFlag(cmd)
	return cmd
}
