package cmd

import (
	"fmt"
	"sort"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/spf13/cobra"
)

func newDueCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "due",
		Short: "Show upcoming renewals.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			subs := data.Subscriptions
			sort.SliceStable(subs, func(i, j int) bool {
				return subs[i].RenewalDate.Before(subs[j].RenewalDate.Time)
			})
			fmt.Fprint(d.Out, d.Render.Due(subs, d.Clock.Now(), days))
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "look-ahead window in days")
	return cmd
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Monthly spend and category breakdown.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			r := insights.Build(data.Subscriptions, d.Profile, d.FX, d.Clock.Now())
			fmt.Fprint(d.Out, d.Render.Stats(r))
			return nil
		},
	}
}

func newInsightsCmd() *cobra.Command {
	return &cobra.Command{
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
			fmt.Fprint(d.Out, d.Render.Insights(r, len(zs), sav))
			return nil
		},
	}
}

func newCategoriesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "categories",
		Short: "List categories with counts and spend.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			r := insights.Build(data.Subscriptions, d.Profile, d.FX, d.Clock.Now())
			for _, c := range r.ByCategory {
				fmt.Fprintf(d.Out, "%-16s%s  (%.0f%%)\n", c.Category, money.Format(c.Monthly, r.Currency), c.Percent)
			}
			return nil
		},
	}
}

func newPaymentMethodsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "payment-methods",
		Short: "List payment methods with counts and spend.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			r := insights.Build(data.Subscriptions, d.Profile, d.FX, d.Clock.Now())
			for _, m := range r.ByPaymentMethod {
				fmt.Fprintf(d.Out, "%-18s %d subs  %s\n", m.Method, m.Count, money.Format(m.Monthly, r.Currency))
			}
			return nil
		},
	}
}
