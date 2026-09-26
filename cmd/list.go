package cmd

import (
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List subscriptions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			data, err := d.Store.Load()
			if err != nil {
				return err
			}
			subs := data.Subscriptions
			if !all {
				subs = activeOnly(subs)
			}
			if jsonRequested(cmd) {
				return writeJSON(d, subs)
			}
			fmt.Fprint(d.Out, d.Render.List(subs, d.Clock.Now()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include cancelled/paused subscriptions")
	jsonFlag(cmd)
	return cmd
}

func activeOnly(subs []model.Subscription) []model.Subscription {
	out := make([]model.Subscription, 0, len(subs))
	for _, s := range subs {
		if s.Active() {
			out = append(out, s)
		}
	}
	return out
}
