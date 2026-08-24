package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/RishikeshSreekumar/leak/internal/backup"
	"github.com/RishikeshSreekumar/leak/internal/model"
	"github.com/spf13/cobra"
)

// writeJSON emits v as indented JSON — the scriptable, LLM-friendly output
// path every read command offers via --json.
func writeJSON(d *Deps, v any) error {
	enc := json.NewEncoder(d.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jsonFlag registers the standard --json flag.
func jsonFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "machine-readable output")
}

// jsonRequested reports whether --json was passed.
func jsonRequested(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}

// autoBackup snapshots the registry before a bulk or destructive change, then
// trims old snapshots to the retention limit. It is a no-op when the profile
// disables it, and a first-run empty registry is not an error.
func autoBackup(d *Deps, reason string) error {
	if !d.Profile.AutoBackupEnabled() {
		return nil
	}
	dir := d.Store.Dir()
	if _, err := backup.Create(dir, d.Clock.Now()); err != nil {
		if backup.IsNothingToBackUp(err) {
			return nil
		}
		return fmt.Errorf("backup before %s: %w", reason, err)
	}
	keep := d.Profile.BackupKeep
	if keep == 0 {
		keep = model.DefaultBackupKeep
	}
	if keep > 0 {
		if _, err := backup.Prune(dir, keep); err != nil {
			return err
		}
	}
	return nil
}
