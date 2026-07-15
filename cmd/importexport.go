package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	return &cobra.Command{
		Use:   "import <file>",
		Short: "Import subscriptions from csv|json|yaml (merged, deduped by id/name).",
		Args:  cobra.ExactArgs(1),
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
			added, skipped := mergeImport(d, incoming)
			fmt.Fprintf(d.Out, "%s Imported %d subscription(s), skipped %d duplicate(s).\n",
				d.Render.Accent("✓"), added, skipped)
			return nil
		},
	}
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

// mergeImport adds incoming subscriptions, skipping ones whose id or name
// already exists in the registry.
func mergeImport(d *Deps, incoming []model.Subscription) (added, skipped int) {
	data, err := d.Store.Load()
	if err != nil {
		return 0, 0
	}
	byName := map[string]bool{}
	byID := map[string]bool{}
	for _, s := range data.Subscriptions {
		byName[strings.ToLower(s.Name)] = true
		byID[s.ID] = true
	}
	for _, s := range incoming {
		if byName[strings.ToLower(s.Name)] || (s.ID != "" && byID[s.ID]) {
			skipped++
			continue
		}
		s.ID = "" // let store assign a unique slug
		if s.Currency == "" {
			s.Currency = d.Profile.DefaultCurrency
		}
		if s.BillingCycle == "" {
			s.BillingCycle = model.CycleMonthly
		}
		s.Touch(d.Clock.Now())
		if _, err := d.Store.AddSub(s); err != nil {
			continue
		}
		byName[strings.ToLower(s.Name)] = true
		added++
	}
	return added, skipped
}
