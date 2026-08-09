package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

func TestStringIncludesBuildMetadata(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })
	Version, Commit, Date = "1.2.3", "abc123", "2026-07-30"

	got := String()
	for _, want := range []string{"1.2.3", "abc123", "2026-07-30", runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q, want containing %q", got, want)
		}
	}
}
