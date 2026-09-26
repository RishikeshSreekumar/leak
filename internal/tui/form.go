package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// formMode distinguishes an add flow from an edit flow.
type formMode int

const (
	formAdd formMode = iota
	formEdit
)

// modalContentWidth is the inner width of the form inside the modal box.
// The modal border (2) + padding (4) adds 6 columns, giving ~62 total.
const modalContentWidth = 56

// addNewSentinel is the option value that reveals a free-text follow-up input.
// The NUL prefix keeps it from colliding with any real value.
const addNewSentinel = "\x00__add_new__"

// subForm is the add/edit overlay: a themed huh.Form whose selects surface the
// profile's currencies, categories, and payment methods. The original
// subscription is kept so an edit preserves fields the form does not expose.
type subForm struct {
	mode  formMode
	title string
	orig  model.Subscription
	form  *huh.Form

	name, amount, cycle, renewal, trial, url, notes string
	currencyPick, currencyCustom                    string
	categoryPick, categoryCustom                    string
	paymentPick, paymentCustom                      string
}

// newSubForm builds the overlay, prefilled from sub (zero value for add).
func newSubForm(mode formMode, sub model.Subscription, prof model.Profile) *subForm {
	sf := &subForm{
		mode:         mode,
		orig:         sub,
		name:         sub.Name,
		cycle:        sub.BillingCycle,
		renewal:      sub.RenewalDate.String(),
		trial:        sub.TrialEnds.String(),
		url:          sub.URL,
		notes:        sub.Notes,
		currencyPick: sub.Currency,
		categoryPick: sub.Category,
		paymentPick:  sub.PaymentMethod,
	}
	if sub.Amount != 0 {
		sf.amount = strconv.FormatFloat(sub.Amount, 'f', -1, 64)
	}
	if sf.currencyPick == "" {
		sf.currencyPick = prof.DefaultCurrency
	}
	if sf.cycle == "" {
		sf.cycle = model.CycleMonthly
	}

	sf.title = "Add Subscription"
	if mode == formEdit {
		sf.title = "Edit " + sub.Name
	}

	notShown := func(pick *string) func() bool {
		return func() bool { return *pick != addNewSentinel }
	}

	// One field per group: selects render their options full-screen, and each
	// "add new" follow-up lives in its own hideable group (WithHideFunc is a
	// Group method, not an Input method). The title/description Note is
	// rendered as modal chrome (formModal) so it persists across groups.
	// choice is a select over profile values plus a hidden "add new" input,
	// or a plain input when the profile has nothing to choose from yet.
	choice := func(title string, values []string, pick, custom *string) []*huh.Group {
		if len(values) == 0 {
			*pick = addNewSentinel
			return []*huh.Group{huh.NewGroup(huh.NewInput().Title(title).Value(custom))}
		}
		return []*huh.Group{
			huh.NewGroup(huh.NewSelect[string]().Title(title).
				Options(stringOptions(values, *pick)...).Value(pick)),
			huh.NewGroup(huh.NewInput().Title("New " + strings.ToLower(title)).Value(custom).Validate(nonEmpty)).
				WithHideFunc(notShown(pick)),
		}
	}

	groups := []*huh.Group{
		huh.NewGroup(huh.NewInput().Title("Name").Value(&sf.name).Validate(nonEmpty)),
		huh.NewGroup(huh.NewInput().Title("Amount").Value(&sf.amount).Validate(positiveFloat)),
	}
	groups = append(groups, choice("Currency", prof.Currencies, &sf.currencyPick, &sf.currencyCustom)...)
	groups = append(groups, huh.NewGroup(huh.NewSelect[string]().Title("Billing cycle").
		Options(cycleOpts()...).Value(&sf.cycle)))
	groups = append(groups, choice("Category", prof.Categories, &sf.categoryPick, &sf.categoryCustom)...)
	groups = append(groups, choice("Payment method", prof.PaymentMethods, &sf.paymentPick, &sf.paymentCustom)...)
	groups = append(groups,
		huh.NewGroup(huh.NewInput().Title("Renewal date (YYYY-MM-DD)").
			Value(&sf.renewal).Validate(dateOptional)),
		huh.NewGroup(huh.NewInput().Title("Free trial ends (YYYY-MM-DD, blank if none)").
			Value(&sf.trial).Validate(dateOptional)),
		huh.NewGroup(huh.NewInput().Title("Manage/cancel URL (blank if none)").Value(&sf.url)),
		huh.NewGroup(huh.NewInput().Title("Notes").Value(&sf.notes)),
	)
	sf.form = huh.NewForm(groups...).WithWidth(modalContentWidth).WithShowHelp(true).WithTheme(formTheme())
	return sf
}

// result overlays the edited fields onto the original subscription. For add,
// orig is the zero value and Status defaults to active.
func (sf *subForm) result() model.Subscription {
	sub := sf.orig
	sub.Name = strings.TrimSpace(sf.name)
	if amt, err := strconv.ParseFloat(strings.TrimSpace(sf.amount), 64); err == nil {
		sub.Amount = amt
	}
	sub.Currency = strings.ToUpper(resolvePick(sf.currencyPick, sf.currencyCustom))
	sub.BillingCycle = sf.cycle
	sub.Category = resolvePick(sf.categoryPick, sf.categoryCustom)
	sub.PaymentMethod = resolvePick(sf.paymentPick, sf.paymentCustom)
	sub.Notes = strings.TrimSpace(sf.notes)
	if r := strings.TrimSpace(sf.renewal); r != "" {
		if dt, err := model.ParseDate(r); err == nil {
			sub.RenewalDate = dt
		}
	} else {
		sub.RenewalDate = model.Date{}
	}
	sub.TrialEnds = model.Date{}
	if t := strings.TrimSpace(sf.trial); t != "" {
		if dt, err := model.ParseDate(t); err == nil {
			sub.TrialEnds = dt
		}
	}
	sub.URL = strings.TrimSpace(sf.url)
	if sub.Status == "" {
		sub.Status = model.StatusActive
	}
	return sub
}

// formTheme is a compact theme matching the dashboard's green accent.
func formTheme() *huh.Theme {
	t := huh.ThemeBase()
	accent := lipgloss.Color("42")  // green, matches render.accent
	subtle := lipgloss.Color("245") // muted grey
	warn := lipgloss.Color("214")   // amber, matches render.warn

	t.Focused.Title = t.Focused.Title.Foreground(accent).Bold(true)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(accent).Bold(true)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accent)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(accent).Bold(true)
	t.Focused.Option = t.Focused.Option.Foreground(subtle)
	t.Focused.Base = t.Focused.Base.BorderForeground(accent)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(warn)
	t.Help.ShortKey = t.Help.ShortKey.Foreground(accent)
	t.Blurred.Title = t.Blurred.Title.Foreground(subtle)
	return t
}

// --- options, validators (local mirrors of cmd/helpers so tui stays cycle-free) ---

// stringOptions builds a select from values, ensuring current is present and
// appending the "add new" sentinel.
func stringOptions(values []string, current string) []huh.Option[string] {
	seen := false
	opts := make([]huh.Option[string], 0, len(values)+2)
	for _, v := range values {
		opts = append(opts, huh.NewOption(v, v))
		if v == current {
			seen = true
		}
	}
	if current != "" && current != addNewSentinel && !seen {
		opts = append(opts, huh.NewOption(current, current))
	}
	opts = append(opts, huh.NewOption("＋ Add new…", addNewSentinel))
	return opts
}

// resolvePick returns the custom text when the sentinel was chosen, else the
// picked value; both trimmed.
func resolvePick(pick, custom string) string {
	if pick == addNewSentinel {
		return strings.TrimSpace(custom)
	}
	return strings.TrimSpace(pick)
}

func cycleOpts() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("Monthly", model.CycleMonthly),
		huh.NewOption("Yearly", model.CycleYearly),
		huh.NewOption("Weekly", model.CycleWeekly),
		huh.NewOption("Quarterly", model.CycleQuarterly),
	}
}

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("cannot be empty")
	}
	return nil
}

func positiveFloat(s string) error {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return fmt.Errorf("enter a number")
	}
	if v <= 0 {
		return fmt.Errorf("must be greater than zero")
	}
	return nil
}

func dateOptional(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if _, err := model.ParseDate(s); err != nil {
		return fmt.Errorf("use YYYY-MM-DD")
	}
	return nil
}
