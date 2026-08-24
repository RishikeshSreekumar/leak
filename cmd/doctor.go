package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/backup"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/store"
	"github.com/spf13/cobra"
)

// severity levels for a diagnosis.
const (
	sevOK    = "ok"
	sevWarn  = "warn"
	sevError = "error"
)

// finding is one line of the health report.
type finding struct {
	Severity string `json:"severity"`
	Subject  string `json:"subject"`         // what was checked
	Detail   string `json:"detail"`          // what was found
	Fix      string `json:"fix,omitempty"`   // what --fix would do (or did)
	Fixed    bool   `json:"fixed,omitempty"` // whether --fix applied it
	ID       string `json:"id,omitempty"`    // subscription id, when record-scoped
}

func newDoctorCmd() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the registry and config for problems.",
		Long: "Inspect the config directory, the on-disk schema, and every subscription " +
			"record for anything that would produce wrong numbers or a confusing report. " +
			"With --fix, the safe repairs are applied (after a backup); everything else is " +
			"reported with the command that resolves it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			jsonOut, _ := cmd.Flags().GetBool("json")

			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			findings := diagnose(d, data)

			if fix {
				repaired, err := repair(d, data, findings)
				if err != nil {
					return err
				}
				findings = repaired
			}
			if jsonOut {
				return writeJSON(d, map[string]any{"findings": findings, "fixed": fix})
			}
			return printFindings(d, findings, fix)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "apply the safe repairs (backs up first)")
	jsonFlag(cmd)
	return cmd
}

// diagnose runs every check and returns the findings, worst first.
func diagnose(d *Deps, data *model.Data) []finding {
	var out []finding
	out = append(out, checkStorage(d, data)...)
	out = append(out, checkRecords(d, data)...)
	out = append(out, checkProfile(d, data)...)
	out = append(out, checkSync(d)...)

	rank := map[string]int{sevError: 0, sevWarn: 1, sevOK: 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Severity] < rank[out[j].Severity] })
	return out
}

// checkStorage inspects the config directory itself.
func checkStorage(d *Deps, data *model.Data) []finding {
	dir := d.Store.Dir()
	out := []finding{{
		Severity: sevOK,
		Subject:  "config dir",
		Detail:   fmt.Sprintf("%s (%d subscription(s), schema v%d)", dir, len(data.Subscriptions), data.Version),
	}}
	if info, err := os.Stat(dir); err == nil {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			out = append(out, finding{
				Severity: sevWarn,
				Subject:  "permissions",
				Detail:   fmt.Sprintf("%s is %#o — other users on this machine can read it", dir, mode),
				Fix:      "restrict to 0700",
			})
		}
	}
	snaps, err := backup.List(dir)
	switch {
	case err != nil:
		out = append(out, finding{Severity: sevWarn, Subject: "backups", Detail: err.Error()})
	case len(snaps) == 0:
		out = append(out, finding{
			Severity: sevWarn,
			Subject:  "backups",
			Detail:   "no snapshots yet",
			Fix:      "run `leak backup`",
		})
	default:
		out = append(out, finding{
			Severity: sevOK,
			Subject:  "backups",
			Detail:   fmt.Sprintf("%d snapshot(s), newest %s", len(snaps), snaps[0].Name),
		})
	}
	return out
}

// checkRecords validates every subscription.
func checkRecords(d *Deps, data *model.Data) []finding {
	var out []finding
	seen := map[string]string{} // lowercased name -> first id holding it
	cycles := map[string]bool{
		model.CycleWeekly: true, model.CycleMonthly: true,
		model.CycleQuarterly: true, model.CycleYearly: true,
	}
	statuses := map[string]bool{
		model.StatusActive: true, model.StatusCancelled: true, model.StatusPaused: true,
	}
	for _, s := range data.Subscriptions {
		switch {
		case s.ID == "":
			out = append(out, finding{Severity: sevError, Subject: "record", Detail: "a subscription has no id",
				Fix: "assign id " + store.Slugify(s.Name)})
		case s.Name == "":
			out = append(out, finding{Severity: sevError, ID: s.ID, Subject: "record", Detail: "missing name"})
		}
		if !cycles[strings.ToLower(s.BillingCycle)] {
			out = append(out, finding{
				Severity: sevError, ID: s.ID, Subject: "billing cycle",
				Detail: fmt.Sprintf("%q is not weekly|monthly|quarterly|yearly — it is excluded from every total", s.BillingCycle),
				Fix:    "set to monthly",
			})
		}
		if !statuses[strings.ToLower(s.Status)] {
			out = append(out, finding{
				Severity: sevError, ID: s.ID, Subject: "status",
				Detail: fmt.Sprintf("%q is not active|paused|cancelled", s.Status),
				Fix:    "set to active",
			})
		}
		if s.Currency == "" {
			out = append(out, finding{
				Severity: sevError, ID: s.ID, Subject: "currency", Detail: "missing currency",
				Fix: "set to " + d.Profile.DefaultCurrency,
			})
		} else if len(s.Currency) != 3 {
			out = append(out, finding{
				Severity: sevWarn, ID: s.ID, Subject: "currency",
				Detail: fmt.Sprintf("%q is not a 3-letter ISO code — FX conversion will fall back to 1:1", s.Currency),
			})
		}
		if s.Amount <= 0 {
			out = append(out, finding{
				Severity: sevWarn, ID: s.ID, Subject: "amount",
				Detail: fmt.Sprintf("amount is %.2f", s.Amount), Fix: "run `leak edit " + s.ID + "`",
			})
		}
		if s.Active() && s.RenewalDate.IsZero() {
			out = append(out, finding{
				Severity: sevWarn, ID: s.ID, Subject: "renewal date",
				Detail: "no renewal date — this subscription never appears in `leak due`",
				Fix:    "run `leak edit " + s.ID + "`",
			})
		}
		if s.Active() && s.LastConfirmed.IsZero() {
			out = append(out, finding{
				Severity: sevWarn, ID: s.ID, Subject: "last confirmed",
				Detail: "never confirmed — sweep cannot tell whether it is a zombie",
				Fix:    "mark it confirmed today",
			})
		}
		if s.Category == "" {
			out = append(out, finding{
				Severity: sevWarn, ID: s.ID, Subject: "category",
				Detail: "uncategorised — it lands in the blank bucket in `leak stats`",
				Fix:    "run `leak edit " + s.ID + "`",
			})
		} else if !contains(d.Profile.Categories, s.Category) {
			out = append(out, finding{
				Severity: sevWarn, ID: s.ID, Subject: "category",
				Detail: fmt.Sprintf("%q is not in your profile", s.Category),
				Fix:    fmt.Sprintf("run `leak category add %q`", s.Category),
			})
		}
		if prev, dup := seen[strings.ToLower(s.Name)]; dup {
			out = append(out, finding{
				Severity: sevWarn, ID: s.ID, Subject: "duplicate",
				Detail: fmt.Sprintf("same name as %s — you may be paying twice, or it was imported twice", prev),
				Fix:    "run `leak remove " + s.ID + " --hard` if it is a duplicate",
			})
		} else {
			seen[strings.ToLower(s.Name)] = s.ID
		}
	}
	if len(out) == 0 {
		out = append(out, finding{Severity: sevOK, Subject: "records",
			Detail: fmt.Sprintf("all %d subscription(s) look well-formed", len(data.Subscriptions))})
	}
	return out
}

// checkProfile validates config.yaml.
func checkProfile(d *Deps, data *model.Data) []finding {
	var out []finding
	p := d.Profile
	if len(p.DefaultCurrency) != 3 {
		out = append(out, finding{
			Severity: sevError, Subject: "default currency",
			Detail: fmt.Sprintf("%q is not a 3-letter ISO code", p.DefaultCurrency),
			Fix:    "run `leak currency set-default INR`",
		})
	}
	if p.StaleAfterDays <= p.ReviewAfterDays {
		out = append(out, finding{
			Severity: sevWarn, Subject: "thresholds",
			Detail: fmt.Sprintf("stale_after_days (%d) is not greater than review_after_days (%d) — everything reads as a zombie immediately",
				p.StaleAfterDays, p.ReviewAfterDays),
			Fix: "edit " + filepath.Join(d.Store.Dir(), "config.yaml"),
		})
	}
	if !p.AutoBackupEnabled() {
		out = append(out, finding{
			Severity: sevWarn, Subject: "auto backup",
			Detail: "disabled — bulk changes are not snapshotted first",
			Fix:    "set auto_backup: true in config.yaml",
		})
	}
	if n := len(data.Deleted); n > 0 {
		out = append(out, finding{
			Severity: sevOK, Subject: "tombstones",
			Detail: fmt.Sprintf("%d deletion(s) recorded for sync", n),
		})
	}
	return out
}

// checkSync reports the transport state without touching the network.
func checkSync(d *Deps) []finding {
	cfg := d.Profile.Sync
	if !cfg.Enabled() {
		return []finding{{
			Severity: sevOK, Subject: "sync",
			Detail: "not configured (local-only)",
			Fix:    "run `leak sync init --dir <path>` to sync across devices",
		}}
	}
	out := []finding{{
		Severity: sevOK, Subject: "sync",
		Detail: fmt.Sprintf("%s → %s, strategy %s", cfg.Kind, cfg.Target, cfg.Strategy),
	}}
	if cfg.Kind == model.SyncKindDir {
		if _, err := os.Stat(cfg.Target); err != nil {
			out = append(out, finding{
				Severity: sevError, Subject: "sync target",
				Detail: fmt.Sprintf("%s is not reachable: %v", cfg.Target, err),
				Fix:    "reconnect the folder, or run `leak sync init --dir <path>` again",
			})
		}
	}
	if cfg.LastSynced.IsZero() {
		out = append(out, finding{
			Severity: sevWarn, Subject: "sync", Detail: "configured but never run",
			Fix: "run `leak sync`",
		})
	} else if days := int(d.Clock.Now().Sub(cfg.LastSynced).Hours() / 24); days > 30 {
		out = append(out, finding{
			Severity: sevWarn, Subject: "sync",
			Detail: fmt.Sprintf("last synced %d days ago", days), Fix: "run `leak sync`",
		})
	}
	return out
}

// repair applies the safe, unambiguous fixes and marks them as applied. Fixes
// that need a human decision (duplicates, wrong amounts) are left untouched.
func repair(d *Deps, data *model.Data, findings []finding) ([]finding, error) {
	if !anyAutoFixable(findings) {
		return findings, nil
	}
	if err := autoBackup(d, "doctor --fix"); err != nil {
		return nil, err
	}
	now := d.Clock.Now()
	byID := map[string]*model.Subscription{}
	for i := range data.Subscriptions {
		byID[data.Subscriptions[i].ID] = &data.Subscriptions[i]
	}

	changed := false
	for i, f := range findings {
		if !autoFixable(f) {
			continue
		}
		if f.Subject == "permissions" {
			if err := os.Chmod(d.Store.Dir(), 0o700); err != nil {
				return nil, err
			}
			findings[i].Fixed = true
			continue
		}
		sub := byID[f.ID]
		if sub == nil {
			continue
		}
		switch f.Subject {
		case "billing cycle":
			sub.BillingCycle = model.CycleMonthly
		case "status":
			sub.Status = model.StatusActive
		case "currency":
			sub.Currency = d.Profile.DefaultCurrency
		default:
			continue
		}
		sub.Touch(now)
		findings[i].Fixed = true
		changed = true
	}
	if changed {
		if err := d.Store.Save(data); err != nil {
			return nil, err
		}
	}
	return findings, nil
}

// autoFixable reports whether a finding has a repair Leak can apply without
// guessing at the user's intent.
func autoFixable(f finding) bool {
	if f.Fixed {
		return false
	}
	if f.Subject == "permissions" {
		return true // tightening the config dir cannot lose data
	}
	if f.ID == "" {
		return false
	}
	switch f.Subject {
	case "billing cycle", "status":
		return true
	case "currency":
		// Only a missing currency is unambiguous; a malformed one might be a
		// code Leak simply does not know.
		return strings.HasPrefix(f.Detail, "missing")
	}
	// "last confirmed" is deliberately not auto-fixable: stamping a confirmation
	// the user never made would hide a zombie.
	return false
}

func printFindings(d *Deps, findings []finding, fixed bool) error {
	fmt.Fprintf(d.Out, "%s\n\n", d.Render.Heading("Doctor"))
	var errs, warns int
	for _, f := range findings {
		var marker string
		switch f.Severity {
		case sevError:
			marker, errs = d.Render.Warn("✗"), errs+1
		case sevWarn:
			marker, warns = d.Render.Warn("!"), warns+1
		default:
			marker = d.Render.Accent("✓")
		}
		subject := f.Subject
		if f.ID != "" {
			subject = f.ID + " · " + f.Subject
		}
		fmt.Fprintf(d.Out, "%s %-28s %s\n", marker, subject, f.Detail)
		switch {
		case f.Fixed:
			fmt.Fprintf(d.Out, "  %s\n", d.Render.Accent("fixed: "+f.Fix))
		case f.Fix != "":
			fmt.Fprintf(d.Out, "  %s\n", d.Render.Muted("→ "+f.Fix))
		}
	}
	fmt.Fprintf(d.Out, "\n%d error(s), %d warning(s).\n", errs, warns)
	if !fixed && anyAutoFixable(findings) {
		fmt.Fprintln(d.Out, d.Render.Muted("Run `leak doctor --fix` to apply the safe repairs."))
	}
	return nil
}

func anyAutoFixable(findings []finding) bool {
	for _, f := range findings {
		if autoFixable(f) {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}
