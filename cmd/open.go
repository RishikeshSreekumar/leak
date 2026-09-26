package cmd

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	var print bool
	cmd := &cobra.Command{
		Use:   "open <id>",
		Short: "Open a subscription's billing or cancellation page in the browser.",
		Long: "Launch the URL stored on the subscription (set with `leak add --url` or " +
			"`leak edit --url`). This is the one-command path from \"this is a zombie\" to " +
			"the provider's cancel flow.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			sub, err := resolveSub(d, args[0])
			if err != nil {
				return err
			}
			if sub.URL == "" {
				return fmt.Errorf("%s has no URL — set one with `leak edit %s --url https://…`", sub.Name, sub.ID)
			}
			if print {
				fmt.Fprintln(d.Out, sub.URL)
				return nil
			}
			if err := launchBrowser(sub.URL); err != nil {
				return fmt.Errorf("could not open a browser (%v) — the URL is %s", err, sub.URL)
			}
			fmt.Fprintf(d.Out, "%s Opened %s\n", d.Render.Accent("✓"), sub.URL)
			return nil
		},
	}
	cmd.Flags().BoolVar(&print, "print", false, "print the URL instead of opening it")
	return cmd
}

// launchBrowser is swapped out by tests so nothing actually opens.
var launchBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
