package cmd

import (
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/backup"
	"github.com/spf13/cobra"
)

// newBackupCmd exposes timestamped snapshots of the config dir plus an optional
// git-repo transport. See internal/backup and spec §3.1.
func newBackupCmd() *cobra.Command {
	var useGit bool
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Snapshot the registry into a timestamped backup.",
		Long: "Copy the registry files into <config>/backups/<timestamp>/ so they can be " +
			"restored later. With --git, also commit the config dir to a local git repo " +
			"for full history.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			dir := d.Store.Dir()
			snap, err := backup.Create(dir, d.Clock.Now())
			if err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Backed up to %s\n", d.Render.Accent("✓"), d.Render.Muted(snap.Path))
			if useGit {
				if err := backup.GitInitAndCommit(dir, "leak backup "+snap.Name); err != nil {
					return fmt.Errorf("git commit: %w", err)
				}
				fmt.Fprintf(d.Out, "%s Committed config dir to git\n", d.Render.Accent("✓"))
			}
			// Keep the snapshot directory from growing without bound.
			if keep := d.Profile.BackupKeep; keep > 0 {
				removed, err := backup.Prune(dir, keep)
				if err != nil {
					return err
				}
				if n := len(removed); n > 0 {
					fmt.Fprintf(d.Out, "%s\n", d.Render.Muted(
						fmt.Sprintf("Pruned %d old snapshot(s), keeping the newest %d.", n, keep)))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&useGit, "git", false, "also commit the config dir to a local git repo")
	cmd.AddCommand(newBackupListCmd(), newBackupPruneCmd())
	return cmd
}

func newBackupPruneCmd() *cobra.Command {
	var keep int
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old snapshots, keeping the newest --keep.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			if keep <= 0 {
				keep = d.Profile.BackupKeep
			}
			removed, err := backup.Prune(d.Store.Dir(), keep)
			if err != nil {
				return err
			}
			if len(removed) == 0 {
				fmt.Fprintf(d.Out, "%s\n", d.Render.Muted(
					fmt.Sprintf("Nothing to prune — at or below the %d-snapshot limit.", keep)))
				return nil
			}
			for _, s := range removed {
				fmt.Fprintf(d.Out, "%s %s\n", d.Render.Warn("✗"), s.Name)
			}
			fmt.Fprintf(d.Out, "%s Pruned %d snapshot(s), kept the newest %d.\n",
				d.Render.Accent("✓"), len(removed), keep)
			return nil
		},
	}
	cmd.Flags().IntVar(&keep, "keep", 0, "snapshots to retain (default: profile backup_keep)")
	return cmd
}

func newBackupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved backups, newest first.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := depsFrom(cmd)
			snaps, err := backup.List(d.Store.Dir())
			if err != nil {
				return err
			}
			if len(snaps) == 0 {
				fmt.Fprintln(d.Out, d.Render.Muted("No backups yet. Run `leak backup`."))
				return nil
			}
			for _, s := range snaps {
				fmt.Fprintf(d.Out, "%s  %s\n", d.Render.Accent(s.Name), d.Render.Muted(s.Path))
			}
			return nil
		},
	}
}

// newRestoreCmd restores registry files from a backup snapshot.
func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore [name]",
		Short: "Restore the registry from a backup (latest if no name given).",
		Long: "Overwrite the current registry files with those from a backup snapshot. " +
			"The current state is snapshotted first, so a restore is itself reversible. " +
			"Run `leak backup list` to see available names.",
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			snaps, err := backup.List(depsFrom(cmd).Store.Dir())
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			names := make([]string, 0, len(snaps))
			for _, s := range snaps {
				names = append(names, s.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			d := depsFrom(cmd)
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			target, err := backup.Restore(d.Store.Dir(), name, d.Clock.Now(), false)
			if err != nil {
				return err
			}
			fmt.Fprintf(d.Out, "%s Restored from %s\n", d.Render.Accent("✓"), d.Render.Muted(target.Name))
			fmt.Fprintln(d.Out, d.Render.Muted("The pre-restore state was snapshotted first — `leak backup list` to step back."))
			return nil
		},
	}
	return cmd
}
