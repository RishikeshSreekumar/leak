package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/audit"
	"github.com/RishikeshSreekumar/leak/internal/insights"
	"github.com/RishikeshSreekumar/leak/internal/model"
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
			if jsonRequested(cmd) {
				return writeJSON(d, dueWithin(subs, d.Clock.Now(), days))
			}
			fmt.Fprint(d.Out, d.Render.Due(subs, d.Clock.Now(), days))
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "look-ahead window in days")
	jsonFlag(cmd)
	return cmd
}

// dueWithin filters active subscriptions renewing inside the window.
func dueWithin(subs []model.Subscription, now time.Time, days int) []model.Subscription {
	out := make([]model.Subscription, 0, len(subs))
	for _, s := range subs {
		if !s.Active() || s.RenewalDate.IsZero() {
			continue
		}
		if d := int(s.RenewalDate.Sub(now).Hours() / 24); d >= 0 && d <= days {
			out = append(out, s)
		}
	}
	return out
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
		Use:   "categories",
		Short: "List categories with counts and spend.",
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
		Use:   "payment-methods",
		Short: "List payment methods with counts and spend.",
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
