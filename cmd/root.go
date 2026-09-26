// Package cmd wires Leak's cobra command tree over the internal packages.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/clock"
	"github.com/RishikeshSreekumar/leak/internal/fx"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/render"
	"github.com/RishikeshSreekumar/leak/internal/store"
	"github.com/spf13/cobra"
)

// Deps is the shared dependency bundle every command reads from. It is resolved
// once in PersistentPreRunE and stashed on the command context, or injected
// directly by tests.
type Deps struct {
	Store   store.Store
	FX      fx.Provider
	Clock   clock.Clock
	Profile model.Profile
	Out     io.Writer
	In      io.Reader
	Render  *render.Styler
}

type depsKey struct{}

// depsFrom retrieves Deps from a command's context.
func depsFrom(cmd *cobra.Command) *Deps {
	return cmd.Context().Value(depsKey{}).(*Deps)
}

// NewRoot builds the root command. When d is nil, real dependencies are
// resolved at run time; tests pass a pre-built Deps to inject fakes.
func NewRoot(d *Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "leak",
		Short:         "Mark. Sweep. Save. — a terminal-first subscription manager.",
		Version:       versionString(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			resolved := d
			if resolved == nil {
				var err error
				resolved, err = realDeps(cmd)
				if err != nil {
					return err
				}
			}
			ctx := contextWithDeps(cmd.Context(), resolved)
			cmd.SetContext(ctx)
			return nil
		},
		// Bare `leak` launches the dashboard in an interactive terminal;
		// otherwise (pipes, scripts, tests) it prints help.
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !render.IsTTY(os.Stdout) {
				return cmd.Help()
			}
			return runTUI(depsFrom(cmd))
		},
	}

	root.AddCommand(
		newTUICmd(),
		newAddCmd(), newListCmd(), newShowCmd(), newEditCmd(), newRemoveCmd(), newOpenCmd(),
		newDueCmd(), newStatsCmd(), newInsightsCmd(), newCategoriesCmd(), newPaymentMethodsCmd(),
		newReviewCmd(), newMarkCmd(), newSweepCmd(), newGCCmd(),
		newProfileCmd(),
		// The profile mutators live under `leak profile …`; the bare forms stay
		// as hidden aliases so existing scripts keep working.
		hidden(newCurrencyCmd()), hidden(newCategoryCmd()), hidden(newPaymentCmd()),
		newImportCmd(), newExportCmd(), newScanCmd(),
		newBackupCmd(), newRestoreCmd(), newDoctorCmd(),
		newSyncCmd(),
	)
	// Group the commands so `leak --help` reads as a workflow rather than an
	// alphabetical dump.
	root.AddGroup(
		&cobra.Group{ID: groupManage, Title: "Manage:"},
		&cobra.Group{ID: groupAnalyze, Title: "Analyze:"},
		&cobra.Group{ID: groupAudit, Title: "Audit (mark & sweep):"},
		&cobra.Group{ID: groupData, Title: "Data:"},
	)
	assignGroups(root)
	return root
}

// hidden marks a command as a back-compat alias that stays out of `--help`.
func hidden(c *cobra.Command) *cobra.Command {
	c.Hidden = true
	return c
}

// Command group ids used in help output.
const (
	groupManage  = "manage"
	groupAnalyze = "analyze"
	groupAudit   = "audit"
	groupData    = "data"
)

// assignGroups labels each command with its help group and wires subscription-id
// completion onto the commands that take one, so `leak show <TAB>` works in any
// shell that has `leak completion` installed.
func assignGroups(root *cobra.Command) {
	groups := map[string]string{
		"add": groupManage, "list": groupManage, "show": groupManage,
		"edit": groupManage, "remove": groupManage, "open": groupManage, "tui": groupManage,

		"stats": groupAnalyze, "due": groupAnalyze, "insights": groupAnalyze,
		"categories": groupAnalyze, "payment-methods": groupAnalyze,

		"review": groupAudit, "mark": groupAudit, "sweep": groupAudit, "gc": groupAudit,

		"import": groupData, "export": groupData, "scan": groupData,
		"backup": groupData, "restore": groupData, "sync": groupData,
		"doctor": groupData, "profile": groupData,
	}
	takesID := map[string]bool{"show": true, "edit": true, "remove": true, "mark": true, "open": true}
	for _, c := range root.Commands() {
		if g, ok := groups[c.Name()]; ok {
			c.GroupID = g
		}
		if takesID[c.Name()] {
			c.ValidArgsFunction = completeSubIDs
		}
	}
}

// completeSubIDs offers subscription ids (with the name as the description) for
// shell completion.
func completeSubIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	d := depsFrom(cmd)
	data, err := d.Store.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, 0, len(data.Subscriptions))
	for _, s := range data.Subscriptions {
		if strings.HasPrefix(s.ID, toComplete) {
			out = append(out, s.ID+"\t"+s.Name+" ("+s.Status+")")
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// realDeps builds production dependencies from the environment.
func realDeps(cmd *cobra.Command) (*Deps, error) {
	st, err := store.New()
	if err != nil {
		return nil, err
	}
	prof, err := st.LoadProfile()
	if err != nil {
		return nil, err
	}
	color := render.ColorEnabled(os.Stdout)
	return &Deps{
		Store:   st,
		FX:      fx.NewHTTP(st.FXCachePath()),
		Clock:   clock.Real{},
		Profile: prof,
		Out:     cmd.OutOrStdout(),
		In:      cmd.InOrStdin(),
		Render:  render.New(color),
	}, nil
}

// Execute runs the CLI (entrypoint from main).
func Execute() {
	root := NewRoot(nil)
	if err := root.Execute(); err != nil {
		if !errors.Is(err, ErrSomethingDue) {
			fmt.Fprintln(os.Stderr, "leak: "+err.Error())
		}
		os.Exit(1)
	}
}
