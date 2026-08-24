package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/RishikeshSreekumar/leak/internal/sync"
	"github.com/spf13/cobra"
)

// newSyncCmd exposes opt-in multi-device sync. Bare `leak sync` runs a full
// cycle: fetch the remote snapshot, merge it record-by-record with the local
// registry, write the result locally, then publish it back.
func newSyncCmd() *cobra.Command {
	var dryRun, jsonOut bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync the registry with another device (opt-in).",
		Long: "Converge this machine's registry with a remote snapshot. Local files stay the " +
			"source of truth: nothing syncs until you run `leak sync init`, and every merge " +
			"is snapshotted first when auto_backup is on.\n\n" +
			"Transports: a shared directory (Dropbox/iCloud/Syncthing/USB) or any git remote.",
		Example: "  leak sync init --dir ~/Dropbox/leak\n" +
			"  leak sync init --git git@github.com:me/subs.git\n" +
			"  leak sync              # fetch, merge, publish\n" +
			"  leak sync status       # what would change?",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			eng, err := newEngine(d)
			if err != nil {
				return err
			}
			rep, err := eng.Sync(cmd.Context(), dryRun)
			if err != nil {
				return err
			}
			return reportSync(d, rep, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing anything")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	cmd.AddCommand(newSyncInitCmd(), newSyncStatusCmd(), newSyncPullCmd(), newSyncPushCmd(), newSyncDisableCmd())
	return cmd
}

func newSyncInitCmd() *cobra.Command {
	var dir, git, strategy, device string
	var auto bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Configure a sync transport (--dir or --git).",
		Long: "Point Leak at a shared folder or a git remote. The choice is stored in " +
			"config.yaml under `sync:` and never travels in a snapshot, so each device " +
			"can use a different transport.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			if (dir == "") == (git == "") {
				return fmt.Errorf("pass exactly one of --dir <path> or --git <remote>")
			}
			cfg := model.SyncConfig{Kind: model.SyncKindDir, Target: dir, Strategy: strategy, Device: device, Auto: auto}
			if git != "" {
				cfg.Kind, cfg.Target = model.SyncKindGit, git
			}
			// The target is stored in config.yaml and used from whatever
			// directory a later command runs in, so a relative path must be
			// resolved now. A git URL (git@host:repo, https://…) is left alone.
			if cfg.Kind == model.SyncKindDir || localPath(cfg.Target) {
				abs, err := filepath.Abs(cfg.Target)
				if err != nil {
					return err
				}
				cfg.Target = abs
			}
			if cfg.Strategy != model.StrategyLWW && cfg.Strategy != model.StrategyManual {
				return fmt.Errorf("unknown --strategy %q (lww|manual)", cfg.Strategy)
			}
			if cfg.Device == "" {
				cfg.Device = sync.DefaultDevice()
			}
			if cfg.Kind == model.SyncKindGit && !sync.GitAvailable() {
				return fmt.Errorf("git not found on PATH — install git or use --dir")
			}
			d.Profile.Sync = cfg
			if err := d.Store.SaveProfile(d.Profile); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Sync configured: %s (%s, device %s)\n",
				d.Render.Accent("✓"), cfg.Target, cfg.Kind, cfg.Device)
			fmt.Fprintln(d.Out, d.Render.Muted("Run `leak sync` to converge, `leak sync status` to preview."))
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "sync through a directory (Dropbox, iCloud, Syncthing, USB…)")
	cmd.Flags().StringVar(&git, "git", "", "sync through a git remote (URL or path)")
	cmd.Flags().StringVar(&strategy, "strategy", model.StrategyLWW, "conflict strategy: lww|manual")
	cmd.Flags().StringVar(&device, "device", "", "device label recorded on pushed snapshots (default: hostname)")
	cmd.Flags().BoolVar(&auto, "auto", false, "sync automatically after mutating commands")
	return cmd
}

func newSyncStatusCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show what a sync would change (writes nothing).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			if !d.Profile.Sync.Enabled() {
				if jsonOut {
					return writeJSON(d, map[string]any{"enabled": false})
				}
				fmt.Fprintln(d.Out, d.Render.Muted("Sync is not configured. Run `leak sync init --dir <path>` or `--git <remote>`."))
				return nil
			}
			eng, err := newEngine(d)
			if err != nil {
				return err
			}
			rep, err := eng.Status(cmd.Context())
			if err != nil {
				return err
			}
			if !jsonOut && !d.Profile.Sync.LastSynced.IsZero() {
				fmt.Fprintf(d.Out, "Last synced: %s\n", d.Profile.Sync.LastSynced.Format("2006-01-02 15:04 MST"))
			}
			return reportSync(d, rep, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	return cmd
}

func newSyncPullCmd() *cobra.Command {
	var dryRun, jsonOut bool
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Merge the remote registry into this device (no publish).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			eng, err := newEngine(d)
			if err != nil {
				return err
			}
			rep, err := eng.Pull(cmd.Context(), dryRun)
			if err != nil {
				return err
			}
			return reportSync(d, rep, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing anything")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	return cmd
}

func newSyncPushCmd() *cobra.Command {
	var force, dryRun, jsonOut bool
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Publish this device's registry to the remote.",
		Long: "Publish the local registry. Push refuses when the remote holds changes this " +
			"device has not merged yet — pull first, or pass --force to overwrite the remote.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			eng, err := newEngine(d)
			if err != nil {
				return err
			}
			rep, err := eng.Push(cmd.Context(), force, dryRun)
			if err != nil {
				return err
			}
			return reportSync(d, rep, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the remote snapshot with this device's registry")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing anything")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	return cmd
}

func newSyncDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Turn sync off (local data is untouched).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			d.Profile.Sync = model.SyncConfig{}
			if err := d.Store.SaveProfile(d.Profile); err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Sync disabled. Your registry stays exactly where it is.\n", d.Render.Accent("✓"))
			return nil
		},
	}
}

// localPath reports whether a git target is a filesystem path rather than a
// remote URL, so only the former gets resolved to an absolute path.
func localPath(target string) bool {
	if strings.Contains(target, "://") || strings.Contains(target, "@") {
		return false
	}
	return strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") ||
		strings.HasPrefix(target, "~") || !strings.Contains(target, ":")
}

// newEngine builds a sync engine from the resolved deps, wiring the pre-change
// backup hook so a merge is always recoverable.
func newEngine(d *Deps) (*sync.Engine, error) {
	cfg := d.Profile.Sync
	if !cfg.Enabled() {
		return nil, fmt.Errorf("sync is not configured — run `leak sync init --dir <path>` or `--git <remote>`")
	}
	tr, err := sync.NewTransport(cfg, d.Store.Dir())
	if err != nil {
		return nil, err
	}
	return &sync.Engine{
		Store:       d.Store,
		Transport:   tr,
		Config:      cfg,
		Now:         d.Clock.Now,
		StatePath:   filepath.Join(d.Store.Dir(), sync.StateFile),
		BeforeApply: func() error { return autoBackup(d, "sync") },
	}, nil
}

// autoSync runs a sync cycle after a mutating command when sync.auto is set.
// Failures are reported but never fail the command that triggered them — a
// missing network must not make `leak add` look broken.
func autoSync(cmd *cobra.Command) {
	d := depsFrom(cmd)
	if !d.Profile.Sync.Enabled() || !d.Profile.Sync.Auto {
		return
	}
	eng, err := newEngine(d)
	if err != nil {
		return
	}
	if _, err := eng.Sync(cmd.Context(), false); err != nil {
		fmt.Fprintf(d.Out, "%s auto-sync skipped: %v\n", d.Render.Warn("!"), err)
		return
	}
	fmt.Fprintf(d.Out, "%s synced\n", d.Render.Muted("↻"))
}

// reportSync prints a sync report as text or JSON.
func reportSync(d *Deps, rep sync.Report, jsonOut bool) error {
	if jsonOut {
		return writeJSON(d, rep)
	}
	verb := "Applied"
	if rep.DryRun {
		verb = "Would apply"
	}
	fmt.Fprintf(d.Out, "%s  %s\n", d.Render.Heading("Sync"), d.Render.Muted(rep.Transport))
	if !rep.RemoteFound {
		fmt.Fprintln(d.Out, d.Render.Muted("No remote snapshot yet — this device seeds it."))
	}
	fmt.Fprintf(d.Out, "%s: %d added, %d updated, %d removed  (%d local change(s) for the remote)\n",
		verb, rep.Added, rep.Updated, rep.Removed, rep.Pushed)

	if len(rep.Conflicts) > 0 {
		fmt.Fprintf(d.Out, "\n%s\n", d.Render.Warn(fmt.Sprintf("%d diverged record(s):", len(rep.Conflicts))))
		for _, c := range rep.Conflicts {
			winner := c.Resolved
			if winner == "unresolved" {
				winner = d.Render.Warn("unresolved — kept local")
			} else {
				winner = winner + " won"
			}
			fmt.Fprintf(d.Out, "  %-20s %s\n", c.ID, winner)
		}
		if strings.EqualFold(rep.Strategy, model.StrategyManual) {
			fmt.Fprintln(d.Out, d.Render.Muted("\nStrategy is manual: edit the record you want to keep, then re-run."))
		}
	}
	switch {
	case rep.DryRun:
		fmt.Fprintln(d.Out, d.Render.Muted("\nDry run — nothing was written."))
	case rep.Published:
		fmt.Fprintf(d.Out, "%s Published to %s\n", d.Render.Accent("✓"), d.Render.Muted(rep.Transport))
	case rep.Applied:
		fmt.Fprintf(d.Out, "%s Local registry updated\n", d.Render.Accent("✓"))
	case rep.UpToDate:
		fmt.Fprintf(d.Out, "%s Already in sync\n", d.Render.Accent("✓"))
	}
	return nil
}
