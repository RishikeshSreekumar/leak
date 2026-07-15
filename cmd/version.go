package cmd

import "fmt"

// Build metadata, injected at release time via -ldflags -X. Defaults apply to
// `go build`/`go install` from source and to development runs.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionString renders the full version line shown by `leak --version`.
func versionString() string {
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}
