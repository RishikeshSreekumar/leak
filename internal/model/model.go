// Package model holds Leak's core domain types and their YAML representation.
package model

import (
	"strings"
	"time"
)

// DateLayout is the human-editable date format used throughout Leak's files.
const DateLayout = "2006-01-02"

// SchemaVersion is the current on-disk registry schema version. It is written
// into Data.Version on every save so future releases can migrate old files
// forward without breakage. Bump it whenever the persisted shape changes and
// add the corresponding step to Data.Migrate.
//
//	1 — versioning introduced.
//	2 — deletion tombstones (Data.Deleted) added for sync.
const SchemaVersion = 2

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
	ID            string  `yaml:"id" json:"id"`
	Name          string  `yaml:"name" json:"name"`
	Amount        float64 `yaml:"amount" json:"amount"`
	Currency      string  `yaml:"currency" json:"currency"`
	BillingCycle  string  `yaml:"billing_cycle" json:"billing_cycle"`
	Category      string  `yaml:"category" json:"category"`
	PaymentMethod string  `yaml:"payment_method" json:"payment_method"`
	RenewalDate   Date    `yaml:"renewal_date" json:"renewal_date"`
	Status        string  `yaml:"status" json:"status"`
	LastConfirmed Date    `yaml:"last_confirmed" json:"last_confirmed"`
	Notes         string  `yaml:"notes,omitempty" json:"notes,omitempty"`
	// URL is where to manage or cancel the subscription; `leak open` launches it.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// TrialEnds is the day a free trial converts to paid. While it is in the
	// future it is what `leak due` warns about, ahead of any renewal.
	TrialEnds      Date            `yaml:"trial_ends,omitempty" json:"trial_ends,omitempty"`
	BillingHistory []BillingRecord `yaml:"billing_history,omitempty" json:"billing_history,omitempty"`

	// Sync bookkeeping — written now so a future cloud sync can merge
	// field-agnostically without a data migration. See internal/sync.
	UpdatedAt time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	Rev       int       `yaml:"rev,omitempty" json:"rev,omitempty"`
}

// Active reports whether the subscription is currently active.
func (s Subscription) Active() bool { return s.Status == StatusActive }

// NextRenewal returns the first renewal on or after today, rolling the stored
// renewal date forward by whole billing cycles. The stored date is the anchor
// (the day of month the provider charges on) and is never rewritten by reads,
// so a hand-edited file stays honest; `leak doctor --fix` persists the roll.
// Zero if no renewal date is set.
func (s Subscription) NextRenewal(now time.Time) Date {
	if s.RenewalDate.IsZero() {
		return s.RenewalDate
	}
	today := DayOf(now)
	next := s.RenewalDate
	for n := 1; next.Before(today.Time) && n < 10000; n++ {
		next = AdvanceDate(s.RenewalDate, s.BillingCycle, n)
	}
	return next
}

// InTrial reports whether the subscription has a trial that has not ended yet.
func (s Subscription) InTrial(now time.Time) bool {
	return !s.TrialEnds.IsZero() && !s.TrialEnds.Before(DayOf(now).Time)
}

// DayOf truncates a time to a UTC calendar day, the granularity Leak reasons in.
func DayOf(t time.Time) Date {
	return NewDate(t.Year(), t.Month(), t.Day())
}

// DaysUntil returns whole days from now until d (negative if d has passed).
func (d Date) DaysUntil(now time.Time) int {
	return int(d.Sub(DayOf(now).Time).Hours() / 24)
}

// AdvanceDate moves d forward by n billing cycles. Month-based cycles keep the
// anchor day and clamp to the target month's length (Jan 31 → Feb 28 → Mar 31),
// which is how card issuers and app stores bill.
func AdvanceDate(d Date, cycle string, n int) Date {
	if d.IsZero() || n <= 0 {
		return d
	}
	switch strings.ToLower(cycle) {
	case CycleWeekly:
		return Date{d.AddDate(0, 0, 7*n)}
	case CycleQuarterly:
		return addMonths(d, 3*n)
	case CycleYearly:
		return addMonths(d, 12*n)
	default:
		return addMonths(d, n)
	}
}

func addMonths(d Date, months int) Date {
	y, m, day := d.Date()
	first := time.Date(y, m+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return NewDate(first.Year(), first.Month(), day)
}

// Touch bumps the sync bookkeeping fields; call on every mutation.
func (s *Subscription) Touch(now time.Time) {
	s.UpdatedAt = now.UTC()
	s.Rev++
}

// Sync transport kinds and merge strategies.
const (
	SyncKindNone = ""
	SyncKindDir  = "dir"
	SyncKindGit  = "git"

	StrategyLWW    = "lww"
	StrategyManual = "manual"
)

// SyncConfig describes the opt-in sync transport. A zero value (Kind == "")
// means sync is disabled — every core command works without it.
type SyncConfig struct {
	Kind       string    `yaml:"kind,omitempty" json:"kind,omitempty"`         // dir | git
	Target     string    `yaml:"target,omitempty" json:"target,omitempty"`     // directory path or git remote URL
	Strategy   string    `yaml:"strategy,omitempty" json:"strategy,omitempty"` // lww (default) | manual
	Device     string    `yaml:"device,omitempty" json:"device,omitempty"`     // label recorded on pushed snapshots
	Auto       bool      `yaml:"auto,omitempty" json:"auto,omitempty"`         // sync after every mutating command
	LastSynced time.Time `yaml:"last_synced,omitempty" json:"last_synced,omitempty"`
}

// Enabled reports whether a transport has been configured.
func (s SyncConfig) Enabled() bool { return s.Kind != SyncKindNone && s.Target != "" }

// Profile holds reusable user preferences (spec §7 / §12).
type Profile struct {
	DefaultCurrency      string   `yaml:"default_currency" json:"default_currency"`
	ExchangeRateProvider string   `yaml:"exchange_rate_provider" json:"exchange_rate_provider"`
	ReviewAfterDays      int      `yaml:"review_after_days" json:"review_after_days"`
	StaleAfterDays       int      `yaml:"stale_after_days" json:"stale_after_days"`
	Currencies           []string `yaml:"currencies" json:"currencies"`
	Categories           []string `yaml:"categories" json:"categories"`
	PaymentMethods       []string `yaml:"payment_methods" json:"payment_methods"`

	// AutoBackup snapshots the registry before any bulk or destructive change
	// (import, gc --apply, restore, sync merge). It is a pointer so a config
	// file written before the field existed (nil) still gets the safe default
	// of true, while an explicit `auto_backup: false` is honored.
	AutoBackup *bool `yaml:"auto_backup,omitempty" json:"auto_backup,omitempty"`
	// BackupKeep caps retained snapshots. 0 falls back to DefaultBackupKeep;
	// a negative value means keep everything.
	BackupKeep int `yaml:"backup_keep,omitempty" json:"backup_keep,omitempty"`

	Sync SyncConfig `yaml:"sync,omitempty" json:"sync,omitempty"`
}

// DefaultBackupKeep is the retention used when backup_keep is unset.
const DefaultBackupKeep = 20

// AutoBackupEnabled reports whether pre-change snapshots are on (default true).
func (p Profile) AutoBackupEnabled() bool { return p.AutoBackup == nil || *p.AutoBackup }

// Normalize fills zero-valued fields with defaults, so a hand-edited or older
// config.yaml missing keys behaves like a fresh one instead of yielding zeros.
func (p *Profile) Normalize() {
	def := DefaultProfile()
	if p.DefaultCurrency == "" {
		p.DefaultCurrency = def.DefaultCurrency
	}
	if p.ExchangeRateProvider == "" {
		p.ExchangeRateProvider = def.ExchangeRateProvider
	}
	if p.ReviewAfterDays <= 0 {
		p.ReviewAfterDays = def.ReviewAfterDays
	}
	if p.StaleAfterDays <= 0 {
		p.StaleAfterDays = def.StaleAfterDays
	}
	if len(p.Currencies) == 0 {
		p.Currencies = def.Currencies
	}
	if p.BackupKeep == 0 {
		p.BackupKeep = DefaultBackupKeep
	}
	if p.Sync.Kind != SyncKindNone && p.Sync.Strategy == "" {
		p.Sync.Strategy = StrategyLWW
	}
}

// Bool returns a pointer to v, for setting optional profile flags.
func Bool(v bool) *bool { return &v }

// DefaultProfile returns the first-run profile with USD as the reporting
// currency. Use DefaultProfileFor to pick the currency from the user's locale.
func DefaultProfile() Profile { return DefaultProfileFor("USD") }

// DefaultProfileFor returns the first-run profile reporting in currency. There
// are no payment methods by default: they are personal (a card name, a wallet)
// and the add wizard offers to create one the first time it is needed.
func DefaultProfileFor(currency string) Profile {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "USD"
	}
	currencies := []string{currency}
	for _, c := range []string{"USD", "EUR", "GBP", "INR", "JPY", "AUD", "CAD", "SGD", "AED", "CHF"} {
		if c != currency {
			currencies = append(currencies, c)
		}
	}
	return Profile{
		DefaultCurrency:      currency,
		ExchangeRateProvider: "frankfurter.dev",
		ReviewAfterDays:      90,
		StaleAfterDays:       180,
		Currencies:           currencies,
		Categories:           []string{"Development", "Entertainment", "Storage", "Utilities", "AI"},
		AutoBackup:           Bool(true),
		BackupKeep:           DefaultBackupKeep,
	}
}

// localeCurrencies maps an ISO 3166 country code (the part after "_" in a
// POSIX locale such as en_IN.UTF-8) to the currency used there.
var localeCurrencies = map[string]string{
	"US": "USD", "IN": "INR", "GB": "GBP", "IE": "EUR", "DE": "EUR", "FR": "EUR",
	"ES": "EUR", "IT": "EUR", "NL": "EUR", "BE": "EUR", "AT": "EUR", "PT": "EUR",
	"FI": "EUR", "GR": "EUR", "JP": "JPY", "AU": "AUD", "NZ": "NZD", "CA": "CAD",
	"SG": "SGD", "AE": "AED", "CH": "CHF", "SE": "SEK", "NO": "NOK", "DK": "DKK",
	"PL": "PLN", "CZ": "CZK", "BR": "BRL", "MX": "MXN", "ZA": "ZAR", "KR": "KRW",
	"CN": "CNY", "HK": "HKD", "TW": "TWD", "ID": "IDR", "MY": "MYR", "PH": "PHP",
	"TH": "THB", "VN": "VND", "TR": "TRY", "IL": "ILS", "SA": "SAR", "NG": "NGN",
	"KE": "KES", "PK": "PKR", "BD": "BDT", "LK": "LKR", "AR": "ARS", "CL": "CLP",
	"CO": "COP", "RU": "RUB", "UA": "UAH", "HU": "HUF", "RO": "RON",
}

// CurrencyForLocale guesses a reporting currency from a POSIX locale string
// ("en_IN.UTF-8" → "INR"). Empty when the locale carries no known country.
func CurrencyForLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	parts := strings.FieldsFunc(locale, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) < 2 {
		return ""
	}
	return localeCurrencies[strings.ToUpper(parts[len(parts)-1])]
}

// Tombstone records a hard-deleted subscription. Without it a delete on one
// device would be silently resurrected by the next sync from another device,
// because the remote still holds the record.
type Tombstone struct {
	ID        string    `yaml:"id" json:"id"`
	DeletedAt time.Time `yaml:"deleted_at" json:"deleted_at"`
}

// Data is the full subscription registry persisted to disk.
type Data struct {
	// Version is the schema version the file was written with. A zero value
	// means a pre-versioning file (implicitly schema 0); Migrate upgrades it.
	Version       int            `yaml:"version" json:"version"`
	Subscriptions []Subscription `yaml:"subscriptions" json:"subscriptions"`
	// Deleted holds tombstones for hard-deleted ids so sync propagates the
	// deletion instead of resurrecting the record.
	Deleted []Tombstone `yaml:"deleted,omitempty" json:"deleted,omitempty"`
}

// Tombstone records (or refreshes) a deletion marker for id.
func (d *Data) Tombstone(id string, at time.Time) {
	at = at.UTC()
	for i, t := range d.Deleted {
		if t.ID == id {
			if at.After(t.DeletedAt) {
				d.Deleted[i].DeletedAt = at
			}
			return
		}
	}
	d.Deleted = append(d.Deleted, Tombstone{ID: id, DeletedAt: at})
}

// DropTombstone clears any deletion marker for id — called when a record with
// that id is (re-)created, so the resurrection is intentional and sticks.
func (d *Data) DropTombstone(id string) {
	out := d.Deleted[:0]
	for _, t := range d.Deleted {
		if t.ID != id {
			out = append(out, t)
		}
	}
	d.Deleted = out
	if len(d.Deleted) == 0 {
		d.Deleted = nil
	}
}

// Migrate upgrades a loaded registry to the current SchemaVersion in place,
// applying each intermediate step. It is safe to call on an already-current
// file. Returns true if the on-disk version differed and a rewrite is wanted.
func (d *Data) Migrate() (changed bool) {
	from := d.Version
	// Step 0 → 1: versioning introduced; no field transforms needed.
	// Step 1 → 2: tombstones added; absent means "no deletions recorded yet".
	// Future steps switch on d.Version here before the final stamp.
	if d.Version < SchemaVersion {
		d.Version = SchemaVersion
	}
	return from != d.Version
}

// LastBilled infers the most recent day the subscription was charged: the
// renewal date itself when it has passed, otherwise one cycle before the next
// renewal. False when nothing can have been charged yet — no renewal date, or a
// trial still running.
func LastBilled(s Subscription, now time.Time) (Date, bool) {
	if s.RenewalDate.IsZero() || s.InTrial(now) {
		return Date{}, false
	}
	today := DayOf(now)
	if !s.RenewalDate.After(today.Time) {
		return s.RenewalDate, true
	}
	next := s.RenewalDate
	prev := retreatOne(next, s.BillingCycle)
	if prev.After(today.Time) {
		return Date{}, false
	}
	return prev, true
}

// retreatOne steps a date back by one billing cycle.
func retreatOne(d Date, cycle string) Date {
	switch strings.ToLower(cycle) {
	case CycleWeekly:
		return Date{d.AddDate(0, 0, -7)}
	case CycleQuarterly:
		return addMonths(d, -3)
	case CycleYearly:
		return addMonths(d, -12)
	default:
		return addMonths(d, -1)
	}
}
