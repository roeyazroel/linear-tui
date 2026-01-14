package main

import (
	"fmt"
	"runtime"
)

// Version information set via ldflags at build time by GoReleaser.
// Example: -ldflags "-X main.Version=1.0.0 -X main.Commit=abc123 -X main.Date=2026-01-14"
var (
	// Version is the semantic version of the application.
	Version = "dev"
	// Commit is the git commit SHA of the build.
	Commit = "none"
	// Date is the build date.
	Date = "unknown"
)

// VersionInfo returns a formatted string with version details.
func VersionInfo() string {
	return fmt.Sprintf("linear-tui %s (commit: %s, built: %s, %s/%s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}
