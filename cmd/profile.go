package cmd

import (
	"fmt"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/spf13/cobra"
)

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Show the profile; manage currencies, categories, payment methods.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			p := d.Profile
			out := d.Out
			fmt.Fprintf(out, "Default currency:  %s\n", p.DefaultCurrency)
			fmt.Fprintf(out, "FX provider:       %s\n", p.ExchangeRateProvider)
			fmt.Fprintf(out, "Review after:      %d days\n", p.ReviewAfterDays)
			fmt.Fprintf(out, "Stale after:       %d days\n", p.StaleAfterDays)
			fmt.Fprintf(out, "Currencies:        %s\n", strings.Join(p.Currencies, ", "))
			fmt.Fprintf(out, "Categories:        %s\n", strings.Join(p.Categories, ", "))
			fmt.Fprintf(out, "Payment methods:   %s\n", strings.Join(p.PaymentMethods, ", "))
			return nil
		},
	}
	cmd.AddCommand(newCurrencyCmd(), newCategoryCmd(), newPaymentCmd())
	return cmd
}

func newCurrencyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "currency", Short: "Manage currencies."}
	cmd.AddCommand(
		listMutCmd("add", "Add a currency.", func(p *model.Profile, v string) {
			p.Currencies = addUnique(p.Currencies, strings.ToUpper(v))
		}),
		listMutCmd("remove", "Remove a currency.", func(p *model.Profile, v string) {
			p.Currencies = removeVal(p.Currencies, strings.ToUpper(v))
		}),
		&cobra.Command{
			Use:   "set-default <code>",
			Short: "Set the default reporting currency.",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				d := depsFrom(cmd)
				code := strings.ToUpper(args[0])
				d.Profile.DefaultCurrency = code
				// Ensure the default is in the currencies list.
				d.Profile.Currencies = addUnique(d.Profile.Currencies, code)
				if err := saveProfile(cmd, d); err != nil {
					return err
				}
				fmt.Fprintf(d.Out, "%s Default currency set to %s\n", d.Render.Accent("✓"), code)
				return nil
			},
		},
	)
	return cmd
}

func newCategoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "category", Short: "Manage categories."}
	cmd.AddCommand(
		listMutCmd("add", "Add a category.", func(p *model.Profile, v string) {
			p.Categories = addUnique(p.Categories, v)
		}),
		listMutCmd("remove", "Remove a category.", func(p *model.Profile, v string) {
			p.Categories = removeVal(p.Categories, v)
		}),
	)
	return cmd
}

func newPaymentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "payment", Short: "Manage payment methods."}
	cmd.AddCommand(
		listMutCmd("add", "Add a payment method.", func(p *model.Profile, v string) {
			p.PaymentMethods = addUnique(p.PaymentMethods, v)
		}),
		listMutCmd("remove", "Remove a payment method.", func(p *model.Profile, v string) {
			p.PaymentMethods = removeVal(p.PaymentMethods, v)
		}),
	)
	return cmd
}

// listMutCmd builds an add/remove subcommand that mutates a profile list.
func listMutCmd(use, short string, mut func(*model.Profile, string)) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <value>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			mut(&d.Profile, args[0])
			if err := saveProfile(cmd, d); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s %s %q\n", d.Render.Accent("✓"), use, args[0])
			return nil
		},
	}
}

func saveProfile(cmd *cobra.Command, d *Deps) error {
	return d.Store.SaveProfile(d.Profile)
}

func addUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func removeVal(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
