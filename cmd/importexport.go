package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/detect"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var csvHeader = []string{"name", "amount", "currency", "billing_cycle", "category", "payment_method", "renewal_date", "status", "notes"}

func newExportCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export subscriptions (csv|json|yaml).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			switch strings.ToLower(format) {
			case "json":
				enc := json.NewEncoder(d.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(data)
			case "yaml", "yml":
				b, err := yaml.Marshal(data)
				if err != nil {
					return err
				}
				_, err = d.Out.Write(b)
				return err
			case "csv":
				return writeCSV(d, data.Subscriptions)
			default:
				return fmt.Errorf("unsupported format %q (csv|json|yaml)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: csv|json|yaml")
	return cmd
}

func writeCSV(d *Deps, subs []model.Subscription) error {
	w := csv.NewWriter(d.Out)
	defer w.Flush()
	if err := w.Write(csvHeader); err != nil {
		return err
	}
	for _, s := range subs {
		row := []string{
			s.Name, trimFloat(s.Amount), s.Currency, s.BillingCycle,
			s.Category, s.PaymentMethod, s.RenewalDate.String(), s.Status, s.Notes,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func newImportCmd() *cobra.Command {
	var dryRun, update bool
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import subscriptions from csv|json|yaml (merged, deduped by id/name).",
		Long: "Merge subscriptions from a file into the registry. Existing records are left " +
			"alone unless --update is given. The registry is snapshotted first, so an import " +
			"that goes wrong is one `leak restore` away.",
		Example: "  leak import subs.csv --dry-run\n  leak import subs.json --update",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			b, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			incoming, err := parseImport(args[0], b)
			if err != nil {
				return err
			}
			if len(incoming) == 0 {
				fmt.Fprintln(d.Out, d.Render.Muted("Nothing to import — the file has no subscriptions."))
				return nil
			}
			if dryRun {
				res := planImport(d, incoming)
				for _, p := range res {
					fmt.Fprintf(d.Out, "%-10s %s\n", p.action, p.name)
				}
				fmt.Fprintf(d.Out, "\n%s\n", d.Render.Muted("Dry run — nothing was written."))
				return nil
			}
			if err := autoBackup(d, "import"); err != nil {
				return err
			}
			added, updated, skipped := mergeImport(d, incoming, update)
			fmt.Fprintf(d.Out, "%s Imported %d, updated %d, skipped %d duplicate(s).\n",
				d.Render.Accent("✓"), added, updated, skipped)
			autoSync(cmd)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be imported without writing")
	cmd.Flags().BoolVar(&update, "update", false, "overwrite existing subscriptions that match by id or name")
	return cmd
}

// importPlan is one line of a dry-run report.
type importPlan struct{ action, name string }

// planImport reports what an import would do, without touching the store.
func planImport(d *Deps, incoming []model.Subscription) []importPlan {
	data, err := d.Store.Load()
	if err != nil {
		return nil
	}
	existing := indexRegistry(data.Subscriptions)
	out := make([]importPlan, 0, len(incoming))
	for _, s := range incoming {
		if _, dup := matchExisting(existing, s); dup {
			out = append(out, importPlan{"duplicate", s.Name})
			continue
		}
		out = append(out, importPlan{"add", s.Name})
	}
	return out
}

// parseImport picks a decoder based on file extension.
func parseImport(path string, b []byte) ([]model.Subscription, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		var data model.Data
		if err := json.Unmarshal(b, &data); err != nil {
			return nil, err
		}
		return data.Subscriptions, nil
	case ".yaml", ".yml":
		var data model.Data
		if err := yaml.Unmarshal(b, &data); err != nil {
			return nil, err
		}
		return data.Subscriptions, nil
	case ".csv":
		return parseCSV(b)
	default:
		return nil, fmt.Errorf("unsupported file type %q", filepath.Ext(path))
	}
}

func parseCSV(b []byte) ([]model.Subscription, error) {
	r := csv.NewReader(strings.NewReader(string(b)))
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, nil
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(row []string, key string) string {
		if i, ok := idx[key]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	var out []model.Subscription
	for _, row := range rows[1:] {
		amt, _ := strconv.ParseFloat(get(row, "amount"), 64)
		s := model.Subscription{
			Name: get(row, "name"), Amount: amt, Currency: get(row, "currency"),
			BillingCycle: get(row, "billing_cycle"), Category: get(row, "category"),
			PaymentMethod: get(row, "payment_method"), Status: get(row, "status"),
			Notes: get(row, "notes"),
		}
		if s.Status == "" {
			s.Status = model.StatusActive
		}
		if rd := get(row, "renewal_date"); rd != "" {
			if dt, err := model.ParseDate(rd); err == nil {
				s.RenewalDate = dt
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// registryIndex maps both ids and normalized names onto existing records, so an
// import can recognise "Netflix", "netflix", and "NETFLIX.COM" as the same
// subscription the user already tracks.
type registryIndex struct {
	byID   map[string]model.Subscription
	byName map[string]model.Subscription
}

func indexRegistry(subs []model.Subscription) registryIndex {
	idx := registryIndex{
		byID:   make(map[string]model.Subscription, len(subs)),
		byName: make(map[string]model.Subscription, len(subs)*2),
	}
	for _, s := range subs {
		idx.byID[s.ID] = s
		idx.byName[strings.ToLower(strings.TrimSpace(s.Name))] = s
		if k := detect.MerchantKey(s.Name); k != "" {
			idx.byName[k] = s
		}
	}
	return idx
}

// matchExisting finds the registry record an incoming subscription refers to.
func matchExisting(idx registryIndex, s model.Subscription) (model.Subscription, bool) {
	if s.ID != "" {
		if got, ok := idx.byID[s.ID]; ok {
			return got, true
		}
	}
	if got, ok := idx.byName[strings.ToLower(strings.TrimSpace(s.Name))]; ok {
		return got, true
	}
	if k := detect.MerchantKey(s.Name); k != "" {
		if got, ok := idx.byName[k]; ok {
			return got, true
		}
	}
	return model.Subscription{}, false
}

// mergeImport adds incoming subscriptions. Matching records are skipped, or
// overwritten when update is set (keeping the existing id and billing history).
func mergeImport(d *Deps, incoming []model.Subscription, update bool) (added, updated, skipped int) {
	data, err := d.Store.Load()
	if err != nil {
		return 0, 0, 0
	}
	idx := indexRegistry(data.Subscriptions)
	now := d.Clock.Now()
	for _, s := range incoming {
		if s.Currency == "" {
			s.Currency = d.Profile.DefaultCurrency
		}
		if s.BillingCycle == "" {
			s.BillingCycle = model.CycleMonthly
		}
		if s.Status == "" {
			s.Status = model.StatusActive
		}
		if existing, dup := matchExisting(idx, s); dup {
			if !update {
				skipped++
				continue
			}
			merged := s
			merged.ID = existing.ID
			// Billing history is Leak's own FX record; an import never owns it.
			merged.BillingHistory = existing.BillingHistory
			merged.Rev = existing.Rev
			merged.Touch(now)
			if err := d.Store.UpdateSub(merged); err != nil {
				continue
			}
			idx.byName[strings.ToLower(strings.TrimSpace(merged.Name))] = merged
			updated++
			continue
		}
		s.ID = "" // let the store assign a unique slug
		s.Touch(now)
		saved, err := d.Store.AddSub(s)
		if err != nil {
			continue
		}
		idx.byID[saved.ID] = saved
		idx.byName[strings.ToLower(strings.TrimSpace(saved.Name))] = saved
		if k := detect.MerchantKey(saved.Name); k != "" {
			idx.byName[k] = saved
		}
		added++
	}
	return added, updated, skipped
}
