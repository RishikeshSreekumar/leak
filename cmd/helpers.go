package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// errCancelled signals the wizard was declined or aborted; callers skip saving.
var errCancelled = errors.New("cancelled")

// subFlags holds the non-interactive flags shared by add and edit.
type subFlags struct {
	name, currency, cycle, category, payment, renewal, notes, status string
	url, trialEnds                                                   string
	amount                                                           float64
	amountSet                                                        bool
}

// subFlagNames lists every flag register adds, for "did the user pass any".
var subFlagNames = []string{"name", "amount", "currency", "cycle", "category", "payment",
	"renewal", "notes", "status", "url", "trial-ends"}

func (f *subFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.name, "name", "", "subscription name")
	cmd.Flags().Float64Var(&f.amount, "amount", 0, "amount per billing cycle")
	cmd.Flags().StringVar(&f.currency, "currency", "", "currency code (default: profile default)")
	cmd.Flags().StringVar(&f.cycle, "cycle", "", "billing cycle: weekly|monthly|quarterly|yearly")
	cmd.Flags().StringVar(&f.category, "category", "", "category")
	cmd.Flags().StringVar(&f.payment, "payment", "", "payment method")
	cmd.Flags().StringVar(&f.renewal, "renewal", "", "next renewal date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&f.notes, "notes", "", "free-form notes")
	cmd.Flags().StringVar(&f.status, "status", "", "status: active|paused|cancelled")
	cmd.Flags().StringVar(&f.url, "url", "", "billing or cancellation page (opened by `leak open`)")
	cmd.Flags().StringVar(&f.trialEnds, "trial-ends", "", "free trial end date (YYYY-MM-DD); `leak due` warns before it converts")
	cmd.PreRun = func(cmd *cobra.Command, _ []string) {
		f.amountSet = cmd.Flags().Changed("amount")
	}
}

// applyTo overlays set flags onto a subscription, filling defaults.
func (f *subFlags) applyTo(sub *model.Subscription, p model.Profile) {
	if f.name != "" {
		sub.Name = f.name
	}
	if f.amountSet {
		sub.Amount = f.amount
	}
	if f.currency != "" {
		sub.Currency = strings.ToUpper(f.currency)
	} else if sub.Currency == "" {
		sub.Currency = p.DefaultCurrency
	}
	if f.cycle != "" {
		sub.BillingCycle = strings.ToLower(f.cycle)
	} else if sub.BillingCycle == "" {
		sub.BillingCycle = model.CycleMonthly
	}
	if f.category != "" {
		sub.Category = f.category
	}
	if f.payment != "" {
		sub.PaymentMethod = f.payment
	}
	if f.renewal != "" {
		if dt, err := model.ParseDate(f.renewal); err == nil {
			sub.RenewalDate = dt
		}
	}
	if f.notes != "" {
		sub.Notes = f.notes
	}
	if f.url != "" {
		sub.URL = strings.TrimSpace(f.url)
	}
	if f.trialEnds != "" {
		if dt, err := model.ParseDate(f.trialEnds); err == nil {
			sub.TrialEnds = dt
		}
	}
	if f.status != "" {
		sub.Status = strings.ToLower(f.status)
	} else if sub.Status == "" {
		sub.Status = model.StatusActive
	}
}

// attachFXRecord records the most recent charge Leak can infer, with the FX
// rate in effect that day, so historical spend stays stable as rates move. A
// subscription still in trial has not been charged yet, so nothing is recorded.
func attachFXRecord(d *Deps, sub *model.Subscription) {
	now := d.Clock.Now()
	billedOn, ok := model.LastBilled(*sub, now)
	if !ok {
		return
	}
	target := d.Profile.DefaultCurrency
	rate, estimated, err := d.FX.Rate(sub.Currency, target, billedOn)
	rec := model.BillingRecord{
		BilledOn: billedOn,
		Amount:   sub.Amount,
		Currency: sub.Currency,
		FXRate:   1.0,
	}
	if err == nil {
		rec.FXRate = rate
		if s, ok := d.FX.(fx.Sourced); ok {
			rec.FXSource = s.SourceName()
		}
		rec.FXDate = billedOn
		rec.Estimated = estimated
	}
	sub.BillingHistory = append(sub.BillingHistory, rec)
}

// monthlyInDefault converts a subscription's normalized monthly cost into the
// profile default currency using the FX provider.
func monthlyInDefault(d *Deps, sub model.Subscription) float64 {
	rate, _, err := d.FX.Rate(sub.Currency, d.Profile.DefaultCurrency, sub.RenewalDate)
	monthly := money.NormalizedMonthly(sub.Amount, sub.BillingCycle)
	if err != nil {
		return monthly
	}
	return monthly * rate
}

// resolveSub finds a subscription by id, then by name, then by a unique prefix
// of either — case-insensitively — so `leak show net` works without knowing
// the slug. Ambiguity is an error listing the matches.
func resolveSub(d *Deps, ref string) (model.Subscription, error) {
	data, err := d.Store.Load()
	if err != nil {
		return model.Subscription{}, err
	}
	want := strings.ToLower(strings.TrimSpace(ref))
	var prefix []model.Subscription
	for _, s := range data.Subscriptions {
		id, name := strings.ToLower(s.ID), strings.ToLower(s.Name)
		if id == want || name == want {
			return s, nil
		}
		if strings.HasPrefix(id, want) || strings.HasPrefix(name, want) {
			prefix = append(prefix, s)
		}
	}
	switch len(prefix) {
	case 0:
		return model.Subscription{}, fmt.Errorf("subscription %q not found — `leak list` shows ids", ref)
	case 1:
		return prefix[0], nil
	}
	ids := make([]string, len(prefix))
	for i, s := range prefix {
		ids[i] = s.ID
	}
	return model.Subscription{}, fmt.Errorf("%q matches several: %s", ref, strings.Join(ids, ", "))
}

// --- huh option + validation helpers ---

// addNewSentinel is the option value that signals "let me type a new value".
// The NUL prefix keeps it from colliding with any real currency/category/method.
const addNewSentinel = "\x00__add_new__"

func cycleOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("Monthly", model.CycleMonthly),
		huh.NewOption("Yearly", model.CycleYearly),
		huh.NewOption("Weekly", model.CycleWeekly),
		huh.NewOption("Quarterly", model.CycleQuarterly),
	}
}

// optionGroup returns a wizard step for a profile-backed choice. With values
// to choose from it is a select plus a hidden "add new" input; with none (a
// fresh profile has no payment methods) it is a single free-text input, so the
// user is not shown a menu with one entry.
func optionGroup(title string, values []string, pick, custom *string) []*huh.Group {
	if len(values) == 0 {
		*pick = addNewSentinel
		return []*huh.Group{huh.NewGroup(huh.NewInput().Title(title).Value(custom))}
	}
	return []*huh.Group{
		huh.NewGroup(huh.NewSelect[string]().Title(title).Options(stringOptions(values, *pick)...).Value(pick)),
		huh.NewGroup(huh.NewInput().Title("New " + strings.ToLower(strings.TrimSuffix(title, "?")) + "?").
			Value(custom).Validate(validateNonEmpty)).
			WithHideFunc(func() bool { return *pick != addNewSentinel }),
	}
}

// stringOptions builds select options, ensuring the current value is present.
func stringOptions(values []string, current string) []huh.Option[string] {
	seen := false
	opts := make([]huh.Option[string], 0, len(values)+1)
	for _, v := range values {
		opts = append(opts, huh.NewOption(v, v))
		if v == current {
			seen = true
		}
	}
	if current != "" && !seen {
		opts = append(opts, huh.NewOption(current, current))
	}
	opts = append(opts, huh.NewOption("＋ Add new…", addNewSentinel))
	return opts
}

// resolvePick returns custom when the sentinel was chosen, else the picked
// value. The result is trimmed.
func resolvePick(pick, custom string) string {
	if pick == addNewSentinel {
		return strings.TrimSpace(custom)
	}
	return strings.TrimSpace(pick)
}

func validateNonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("cannot be empty")
	}
	return nil
}

func validatePositiveFloat(s string) error {
	v, err := parseFloat(s)
	if err != nil {
		return fmt.Errorf("enter a number")
	}
	if v <= 0 {
		return fmt.Errorf("must be greater than zero")
	}
	return nil
}

func validateDateOptional(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if _, err := model.ParseDate(s); err != nil {
		return fmt.Errorf("use YYYY-MM-DD")
	}
	return nil
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// orDash renders an empty field as a dash so aligned detail views stay legible.
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
