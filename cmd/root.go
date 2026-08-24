// Package cmd wires Leak's cobra command tree over the internal packages.
package cmd

import (
	"fmt"
	"io"
	"os"

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
		newAddCmd(), newListCmd(), newShowCmd(), newEditCmd(), newRemoveCmd(),
		newDueCmd(), newStatsCmd(), newInsightsCmd(), newCategoriesCmd(), newPaymentMethodsCmd(),
		newReviewCmd(), newMarkCmd(), newSweepCmd(), newGCCmd(),
		newProfileCmd(), newCurrencyCmd(), newCategoryCmd(), newPaymentCmd(),
		newImportCmd(), newExportCmd(),
		newBackupCmd(), newRestoreCmd(), newDoctorCmd(),
		newSyncCmd(),
	)
	return root
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
		fmt.Fprintln(os.Stderr, "leak: "+err.Error())
		os.Exit(1)
	}
}
