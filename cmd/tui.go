package cmd

import (
	"fmt"
	"os"

	"github.com/RishikeshSreekumar/leak/internal/render"
	"github.com/RishikeshSreekumar/leak/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive dashboard.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTUI(depsFrom(cmd))
		},
	}
}

// runTUI builds the dashboard model from deps and runs the Bubble Tea program.
// It refuses to start without a TTY (bubbletea needs an interactive terminal).
func runTUI(d *Deps) error {
	if !render.IsTTY(os.Stdout) {
		return fmt.Errorf("tui needs an interactive terminal; try `leak insights` or `leak list` instead")
	}
	m := tui.New(tui.Config{
		Store:   d.Store,
		FX:      d.FX,
		Clock:   d.Clock,
		Profile: d.Profile,
		Color:   render.ColorEnabled(os.Stdout),
	})
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(d.In), tea.WithOutput(d.Out))
	_, err := p.Run()
	return err
}
