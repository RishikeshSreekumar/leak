// Package model holds Leak's core domain types and their YAML representation.
package model

import (
	"strings"
	"time"
)

// DateLayout is the human-editable date format used throughout Leak's files.
const DateLayout = "2006-01-02"

// Status values for a subscription.
const (
	StatusActive    = "active"
	StatusCancelled = "cancelled"
	StatusPaused    = "paused"
)

// Billing cycle values.
const (
	CycleWeekly    = "weekly"
	CycleMonthly   = "monthly"
	CycleQuarterly = "quarterly"
	CycleYearly    = "yearly"
)

// Date wraps time.Time and (un)marshals as YYYY-MM-DD, keeping the on-disk
// YAML human-readable. A zero Date marshals as an empty string.
type Date struct{ time.Time }

// NewDate builds a Date at UTC midnight.
func NewDate(y int, m time.Month, d int) Date {
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// ParseDate parses a YYYY-MM-DD string.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(DateLayout, strings.TrimSpace(s))
	if err != nil {
		return Date{}, err
	}
	return Date{t}, nil
}

// String renders the date as YYYY-MM-DD (empty for the zero value).
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.Format(DateLayout)
}

// MarshalYAML implements yaml.Marshaler.
func (d Date) MarshalYAML() (any, error) {
	if d.IsZero() {
		return "", nil
	}
	return d.String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler, accepting YYYY-MM-DD or empty.
func (d *Date) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if strings.TrimSpace(s) == "" {
		d.Time = time.Time{}
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// MarshalJSON renders as a quoted YYYY-MM-DD string (empty for zero).
func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON parses a quoted YYYY-MM-DD string.
func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" {
		d.Time = time.Time{}
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// DaysSince returns whole days between d and now (negative if d is in future).
func (d Date) DaysSince(now time.Time) int {
	if d.IsZero() {
		return 0
	}
	return int(now.Sub(d.Time).Hours() / 24)
}

// BillingRecord is one historical charge, capturing the FX rate in effect so
// past spending stays stable even as rates move.
type BillingRecord struct {
	BilledOn  Date    `yaml:"billed_on" json:"billed_on"`
	Amount    float64 `yaml:"amount" json:"amount"`
	Currency  string  `yaml:"currency" json:"currency"`
	FXRate    float64 `yaml:"fx_rate" json:"fx_rate"`
	FXSource  string  `yaml:"fx_source,omitempty" json:"fx_source,omitempty"`
	FXDate    Date    `yaml:"fx_date,omitempty" json:"fx_date,omitempty"`
	Estimated bool    `yaml:"estimated,omitempty" json:"estimated,omitempty"`
}

// Subscription is a tracked recurring expense.
type Subscription struct {
	ID             string          `yaml:"id" json:"id"`
	Name           string          `yaml:"name" json:"name"`
	Amount         float64         `yaml:"amount" json:"amount"`
	Currency       string          `yaml:"currency" json:"currency"`
	BillingCycle   string          `yaml:"billing_cycle" json:"billing_cycle"`
	Category       string          `yaml:"category" json:"category"`
	PaymentMethod  string          `yaml:"payment_method" json:"payment_method"`
	RenewalDate    Date            `yaml:"renewal_date" json:"renewal_date"`
	Status         string          `yaml:"status" json:"status"`
	LastConfirmed  Date            `yaml:"last_confirmed" json:"last_confirmed"`
	Notes          string          `yaml:"notes,omitempty" json:"notes,omitempty"`
	BillingHistory []BillingRecord `yaml:"billing_history,omitempty" json:"billing_history,omitempty"`

	// Sync bookkeeping — written now so a future cloud sync can merge
	// field-agnostically without a data migration. See internal/sync.
	UpdatedAt time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	Rev       int       `yaml:"rev,omitempty" json:"rev,omitempty"`
}

// Active reports whether the subscription is currently active.
func (s Subscription) Active() bool { return s.Status == StatusActive }

// Touch bumps the sync bookkeeping fields; call on every mutation.
func (s *Subscription) Touch(now time.Time) {
	s.UpdatedAt = now.UTC()
	s.Rev++
}

// Profile holds reusable user preferences (spec §7 / §12).
type Profile struct {
	DefaultCurrency      string   `yaml:"default_currency" json:"default_currency"`
	ExchangeRateProvider string   `yaml:"exchange_rate_provider" json:"exchange_rate_provider"`
	ReviewAfterDays      int      `yaml:"review_after_days" json:"review_after_days"`
	StaleAfterDays       int      `yaml:"stale_after_days" json:"stale_after_days"`
	Currencies           []string `yaml:"currencies" json:"currencies"`
	Categories           []string `yaml:"categories" json:"categories"`
	PaymentMethods       []string `yaml:"payment_methods" json:"payment_methods"`
}

// DefaultProfile returns the first-run profile.
func DefaultProfile() Profile {
	return Profile{
		DefaultCurrency:      "INR",
		ExchangeRateProvider: "frankfurter.dev",
		ReviewAfterDays:      90,
		StaleAfterDays:       180,
		Currencies:           []string{"INR", "USD", "EUR", "GBP", "JPY", "AUD", "CAD", "SGD", "AED", "CHF"},
		Categories:           []string{"Development", "Entertainment", "Storage", "Utilities", "AI"},
		PaymentMethods:       []string{"ICICI Amazon Pay", "HDFC Millennia", "SBI Cashback", "UPI", "PayPal"},
	}
}

// Data is the full subscription registry persisted to disk.
type Data struct {
	Subscriptions []Subscription `yaml:"subscriptions" json:"subscriptions"`
}
