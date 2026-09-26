package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/detect"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/money"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	var (
		currency      string
		minOccur      int
		minConfidence float64
		tolerance     float64
		apply         bool
	)
	cmd := &cobra.Command{
		Use:   "scan <statement.csv>",
		Short: "Find recurring charges in a bank or card statement.",
		Long: "Read a CSV statement export and report the charges that repeat on a steady " +
			"cadence for a steady amount. Nothing is written until you pass --apply, and " +
			"charges that match an existing subscription are always skipped.\n\n" +
			"The parser adapts to the common export shapes: a single signed amount column " +
			"or separate debit/credit columns, day-first or month-first dates, thousands " +
			"separators, currency symbols, and DR/CR markers.",
		Example: "  leak scan statement.csv\n" +
			"  leak scan statement.csv --min-occurrences 2 --json\n" +
			"  leak scan statement.csv --apply",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			if currency == "" {
				currency = d.Profile.DefaultCurrency
			}
			txns, err := detect.ParseCSV(raw, currency)
			if err != nil {
				return err
			}
			cands := detect.Recurring(txns, detect.Options{
				MinOccurrences:  minOccur,
				AmountTolerance: tolerance,
				Now:             d.Clock.Now(),
			})

			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			known := knownMerchants(data.Subscriptions)

			type row struct {
				detect.Candidate
				Known bool `json:"already_tracked"`
			}
			rows := make([]row, 0, len(cands))
			for _, c := range cands {
				if c.Confidence < minConfidence {
					continue
				}
				rows = append(rows, row{Candidate: c, Known: known[detect.MerchantKey(c.Name)]})
			}

			mismatches := detect.Reconcile(data.Subscriptions, txns, d.Clock.Now())

			if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
				return writeJSON(d, map[string]any{
					"transactions": len(txns),
					"candidates":   rows,
					"mismatches":   mismatches,
				})
			}

			fmt.Fprintf(d.Out, "%s  %s\n\n", d.Render.Heading("Statement Scan"),
				d.Render.Muted(fmt.Sprintf("%d transactions read", len(txns))))
			if len(rows) == 0 {
				fmt.Fprintln(d.Out, d.Render.Muted(
					"No recurring charges found. Try --min-occurrences 2 for a shorter statement."))
			}
			for _, r := range rows {
				tag := ""
				if r.Known {
					tag = d.Render.Muted("  (already tracked)")
				}
				fmt.Fprintf(d.Out, "%-22s %10s %-10s %d charges  conf %.2f  next %s%s\n",
					r.Name, money.Format(r.Amount, r.Currency), r.Cycle, r.Occurrences,
					r.Confidence, r.NextRenewal.Format(model.DateLayout), tag)
			}
			printMismatches(d, mismatches)
			if len(rows) == 0 {
				return nil
			}

			if !apply {
				fmt.Fprintf(d.Out, "\n%s\n", d.Render.Muted(
					"Dry run. Re-run with --apply to add the untracked ones."))
				return nil
			}
			shown := make([]detect.Candidate, 0, len(rows))
			for _, r := range rows {
				shown = append(shown, r.Candidate)
			}
			added, skipped, err := applyCandidates(d, shown, known)
			if err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "\n%s Added %d subscription(s), skipped %d already tracked.\n",
				d.Render.Accent("✓"), added, skipped)
			if added > 0 {
				fmt.Fprintln(d.Out, d.Render.Muted("Review them with `leak list`, fix any detail with `leak edit <id>`."))
			}
			autoSync(cmd)
			return nil
		},
	}
	cmd.Flags().StringVar(&currency, "currency", "", "currency for statements without a currency column (default: profile default)")
	cmd.Flags().IntVar(&minOccur, "min-occurrences", 3, "charges required before a merchant counts as recurring")
	cmd.Flags().Float64Var(&minConfidence, "min-confidence", 0.4, "hide candidates below this confidence (0-1)")
	cmd.Flags().Float64Var(&tolerance, "tolerance", 0.15, "allowed spread around the median charge (0.15 = ±15%)")
	cmd.Flags().BoolVar(&apply, "apply", false, "add the detected subscriptions (default is a dry run)")
	jsonFlag(cmd)
	return cmd
}

// printMismatches reports where the registry and the statement disagree, each
// with the command that resolves it.
func printMismatches(d *Deps, ms []detect.Mismatch) {
	if len(ms) == 0 {
		return
	}
	fmt.Fprintf(d.Out, "\n%s\n\n", d.Render.Heading("Registry vs statement"))
	for _, m := range ms {
		var detail, fix string
		switch m.Kind {
		case detect.MismatchStillCharging:
			detail = fmt.Sprintf("marked %s but charged on %s", m.Status, m.LastCharged.Format(model.DateLayout))
			fix = "chase the provider, or `leak edit " + m.ID + " --status active`"
		case detect.MismatchStopped:
			detail = fmt.Sprintf("last charged %s — no charge since", m.LastCharged.Format(model.DateLayout))
			fix = "already cancelled? `leak remove " + m.ID + "`"
		default:
			detail = "not in this statement at all"
			fix = "paid another way, or already gone: `leak remove " + m.ID + "`"
		}
		fmt.Fprintf(d.Out, "%s %-22s %s\n%s\n", d.Render.Warn("!"), m.Name, detail, d.Render.Muted("    "+fix))
	}
}

// applyCandidates writes the untracked candidates into the registry.
func applyCandidates(d *Deps, cands []detect.Candidate, known map[string]bool) (added, skipped int, err error) {
	if len(cands) == 0 {
		return 0, 0, nil
	}
	if err := autoBackup(d, "scan --apply"); err != nil {
		return 0, 0, err
	}
	now := d.Clock.Now()
	for _, c := range cands {
		key := detect.MerchantKey(c.Name)
		if known[key] {
			skipped++
			continue
		}
		sub := model.Subscription{
			Name:          c.Name,
			Amount:        c.Amount,
			Currency:      c.Currency,
			BillingCycle:  c.Cycle,
			Category:      c.Category,
			Status:        model.StatusActive,
			RenewalDate:   model.Date{Time: c.NextRenewal},
			LastConfirmed: model.Date{Time: now},
			Notes: fmt.Sprintf("detected from statement: %d charges, last %s (%q)",
				c.Occurrences, c.Last.Format(model.DateLayout), c.Descriptor),
		}
		attachFXRecord(d, &sub)
		sub.Touch(now)
		if _, err := d.Store.AddSub(sub); err != nil {
			return added, skipped, err
		}
		known[key] = true
		added++
	}
	return added, skipped, nil
}

// knownMerchants indexes the registry by normalized merchant key so a detected
// charge can be matched against a subscription the user already tracks, even
// when the names differ ("Netflix" vs "NETFLIX.COM").
func knownMerchants(subs []model.Subscription) map[string]bool {
	out := make(map[string]bool, len(subs)*2)
	for _, s := range subs {
		if k := detect.MerchantKey(s.Name); k != "" {
			out[k] = true
		}
		out[strings.ToLower(strings.TrimSpace(s.Name))] = true
	}
	return out
}
