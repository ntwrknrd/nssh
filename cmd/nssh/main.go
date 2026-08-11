// Package main provides the nssh command-line entrypoint.
package main

import (
	"os"
	"runtime/debug"

	"github.com/ntwrknrd/nssh/internal/app"
)

// version is set via ldflags at build time from git describe.
// Falls back to "dev" for local builds without tags.
var version = "dev"

// getBuildInfo extracts commit hash and build time from Go's embedded build info.
// This is automatically populated by Go 1.18+ when building from a git repository.
func getBuildInfo() (commit, date string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
			if len(commit) > 7 {
				commit = commit[:7]
			}
		case "vcs.time":
			date = s.Value
		}
	}
	return commit, date
}

func main() {
	commit, date := getBuildInfo()
	os.Exit(app.Run(app.Options{
		Version: version,
		Commit:  commit,
		Date:    date,
	}))
}
