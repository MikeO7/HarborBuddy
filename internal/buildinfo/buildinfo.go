package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("HarborBuddy %s (commit: %s, built: %s, %s/%s)", Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}
