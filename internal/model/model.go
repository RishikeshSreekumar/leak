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
		AutoBackup:           Bool(true),
		BackupKeep:           DefaultBackupKeep,
	}
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
