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
			sub, err := d.Store.GetSub(args[0])
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
			fmt.Fprintf(out, "  Category:      %s\n", sub.Category)
			fmt.Fprintf(out, "  Payment:       %s\n", sub.PaymentMethod)
			fmt.Fprintf(out, "  Renewal:       %s\n", sub.RenewalDate)
			fmt.Fprintf(out, "  Status:        %s\n", sub.Status)
			fmt.Fprintf(out, "  Last confirmed:%s\n", " "+sub.LastConfirmed.String())
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
