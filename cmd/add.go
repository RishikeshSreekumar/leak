package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var f subFlags
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a subscription (interactive wizard, or use flags).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			sub := model.Subscription{Status: model.StatusActive}

			if f.name == "" {
				// Interactive wizard.
				if err := runSubForm(d, &sub, "Add Subscription"); err != nil {
					if errors.Is(err, errCancelled) {
						fmt.Fprintln(d.Out, "Cancelled — nothing added")
						return nil
					}
					return err
				}
			} else {
				f.applyTo(&sub, d.Profile)
			}
			if sub.Name == "" {
				return fmt.Errorf("name is required")
			}
			if sub.LastConfirmed.IsZero() {
				sub.LastConfirmed = model.Date{Time: d.Clock.Now()}
			}
			attachFXRecord(d, &sub)
			sub.Touch(d.Clock.Now())

			saved, err := d.Store.AddSub(sub)
			if err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Subscription added: %s (%s)\n", d.Render.Accent("✓"), saved.Name, saved.ID)
			return nil
		},
	}
	f.register(cmd)
	return cmd
}

// runSubForm drives the step-by-step huh wizard, one question per screen,
// prefilling from *sub for edits. Option fields (currency, category, payment)
// offer an "add new" entry that reveals a follow-up input page. A final review
// screen confirms before the caller saves; declining returns errCancelled.
func runSubForm(d *Deps, sub *model.Subscription, title string) error {
	amountStr := ""
	if sub.Amount != 0 {
		amountStr = trimFloat(sub.Amount)
	}
	if sub.Currency == "" {
		sub.Currency = d.Profile.DefaultCurrency
	}
	if sub.BillingCycle == "" {
		sub.BillingCycle = model.CycleMonthly
	}
	renewal := sub.RenewalDate.String()

	// Selects bind to temp picks; the sentinel routes to the *Custom inputs.
	currencyPick, currencyCustom := sub.Currency, ""
	categoryPick, categoryCustom := sub.Category, ""
	paymentPick, paymentCustom := sub.PaymentMethod, ""
	confirmed := false

	// isAddNew reports whether a pick selected the "add new" sentinel.
	isAddNew := func(pick *string) func() bool {
		return func() bool { return *pick != addNewSentinel }
	}

	form := huh.NewForm(
		huh.NewGroup(huh.NewInput().Title("Subscription name?").Value(&sub.Name)),
		huh.NewGroup(huh.NewInput().Title("Amount?").Value(&amountStr).Validate(validatePositiveFloat)),

		huh.NewGroup(huh.NewSelect[string]().Title("Currency?").
			Options(stringOptions(d.Profile.Currencies, sub.Currency)...).Value(&currencyPick)),
		huh.NewGroup(huh.NewInput().Title("New currency code?").
			Value(&currencyCustom).Validate(validateNonEmpty)).
			WithHideFunc(isAddNew(&currencyPick)),

		huh.NewGroup(huh.NewSelect[string]().Title("Billing cycle?").
			Options(cycleOptions()...).Value(&sub.BillingCycle)),
		huh.NewGroup(huh.NewInput().Title("Renewal date? (YYYY-MM-DD)").
			Value(&renewal).Validate(validateDateOptional)),

		huh.NewGroup(huh.NewSelect[string]().Title("Category?").
			Options(stringOptions(d.Profile.Categories, sub.Category)...).Value(&categoryPick)),
		huh.NewGroup(huh.NewInput().Title("New category?").
			Value(&categoryCustom).Validate(validateNonEmpty)).
			WithHideFunc(isAddNew(&categoryPick)),

		huh.NewGroup(huh.NewSelect[string]().Title("Payment method?").
			Options(stringOptions(d.Profile.PaymentMethods, sub.PaymentMethod)...).Value(&paymentPick)),
		huh.NewGroup(huh.NewInput().Title("New payment method?").
			Value(&paymentCustom).Validate(validateNonEmpty)).
			WithHideFunc(isAddNew(&paymentPick)),

		huh.NewGroup(huh.NewInput().Title("Notes?").Value(&sub.Notes)),

		huh.NewGroup(huh.NewConfirm().Title("Save this subscription?").
			DescriptionFunc(func() string {
				return subSummary(sub, amountStr, renewal,
					resolvePick(currencyPick, currencyCustom),
					resolvePick(categoryPick, categoryCustom),
					resolvePick(paymentPick, paymentCustom))
			}, []*string{&currencyPick, &currencyCustom, &categoryPick, &categoryCustom, &paymentPick, &paymentCustom, &amountStr, &renewal}).
			Value(&confirmed)),
	)
	fmt.Fprintln(d.Out, d.Render.Heading(title))

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return errCancelled
		}
		return err
	}
	if !confirmed {
		return errCancelled
	}

	sub.Currency = strings.ToUpper(resolvePick(currencyPick, currencyCustom))
	sub.Category = resolvePick(categoryPick, categoryCustom)
	sub.PaymentMethod = resolvePick(paymentPick, paymentCustom)

	amt, err := parseFloat(amountStr)
	if err != nil {
		return err
	}
	sub.Amount = amt
	if renewal != "" {
		dt, err := model.ParseDate(renewal)
		if err != nil {
			return err
		}
		sub.RenewalDate = dt
	}
	return nil
}

// subSummary renders the review-screen description from the resolved fields.
func subSummary(sub *model.Subscription, amount, renewal, currency, category, payment string) string {
	if renewal == "" {
		renewal = "—"
	}
	notes := sub.Notes
	if notes == "" {
		notes = "—"
	}
	return fmt.Sprintf(
		"Name:     %s\nAmount:   %s %s\nCycle:    %s\nRenewal:  %s\nCategory: %s\nPayment:  %s\nNotes:    %s",
		sub.Name, amount, strings.ToUpper(currency), sub.BillingCycle, renewal, category, payment, notes,
	)
}
