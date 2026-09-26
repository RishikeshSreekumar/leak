package cmd

import (
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show full detail for a subscription.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			sub, err := resolveSub(d, args[0])
			if err != nil {
				return err
			}
			if jsonRequested(cmd) {
				return writeJSON(d, sub)
			}
			out := d.Out
			fmt.Fprintf(out, "%s (%s)\n", d.Render.Accent(sub.Name), sub.ID)
			fmt.Fprintf(out, "  Amount:        %s / %s\n", money.Format(sub.Amount, sub.Currency), sub.BillingCycle)
			if sub.Currency != d.Profile.DefaultCurrency {
				conv := monthlyInDefault(d, sub)
				fmt.Fprintf(out, "  ≈ %s/mo\n", money.Format(money.Round2(conv), d.Profile.DefaultCurrency))
			}
			fmt.Fprintf(out, "  Category:      %s\n", orDash(sub.Category))
			fmt.Fprintf(out, "  Payment:       %s\n", orDash(sub.PaymentMethod))
			now := d.Clock.Now()
			if next := sub.NextRenewal(now); !next.IsZero() && !next.Equal(sub.RenewalDate.Time) {
				fmt.Fprintf(out, "  Next renewal:  %s  %s\n", next, d.Render.Muted("(anchored on "+sub.RenewalDate.String()+")"))
			} else {
				fmt.Fprintf(out, "  Next renewal:  %s\n", next)
			}
			if !sub.TrialEnds.IsZero() {
				label := "  Trial ends:    %s\n"
				if sub.InTrial(now) {
					label = "  Trial ends:    %s  " + d.Render.Warn("cancel before this to pay nothing") + "\n"
				}
				fmt.Fprintf(out, label, sub.TrialEnds)
			}
			fmt.Fprintf(out, "  Status:        %s\n", sub.Status)
			fmt.Fprintf(out, "  Last confirmed:%s\n", " "+sub.LastConfirmed.String())
			if sub.URL != "" {
				fmt.Fprintf(out, "  URL:           %s  %s\n", sub.URL, d.Render.Muted("(`leak open "+sub.ID+"`)"))
			}
			if sub.Notes != "" {
				fmt.Fprintf(out, "  Notes:         %s\n", sub.Notes)
			}
			if len(sub.BillingHistory) > 0 {
				fmt.Fprintln(out, "  Billing history:")
				for _, r := range sub.BillingHistory {
					est := ""
					if r.Estimated {
						est = " (estimated rate)"
					}
					fmt.Fprintf(out, "    %s  %s  fx=%.4f%s\n",
						r.BilledOn, money.Format(r.Amount, r.Currency), r.FXRate, est)
				}
			}
			return nil
		},
	}
	jsonFlag(cmd)
	return cmd
}
