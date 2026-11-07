package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMainVersion(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"harborbuddy", "--version"}
	t.Cleanup(func() { os.Args = originalArgs })
	main()
}

func TestMainHelpExitsSuccessfully(t *testing.T) {
	if os.Getenv("HARBORBUDDY_MAIN_HELP") == "1" {
		os.Args = []string{"harborbuddy", "--help"}
		main()
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestMainHelpExitsSuccessfully$") //nolint:gosec // Re-executes the trusted current test binary.
	command.Env = append(os.Environ(), "HARBORBUDDY_MAIN_HELP=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("--help subprocess failed: %v; output=%q", err, output)
	}
	if text := string(output); !strings.Contains(text, "--config") || strings.Contains(text, "help requested") {
		t.Fatalf("--help output = %q", text)
	}
}

func TestMainReportsApplicationError(t *testing.T) {
	if os.Getenv("HARBORBUDDY_MAIN_ERROR") == "1" {
		os.Args = []string{"harborbuddy", "--config", "/definitely/missing/harborbuddy.yml"}
		main()
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestMainReportsApplicationError$") //nolint:gosec // Re-executes the trusted current test binary.
	command.Env = append(os.Environ(), "HARBORBUDDY_MAIN_ERROR=1")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("error subprocess succeeded; output=%q", output)
	}
	if !strings.Contains(string(output), "open config file") {
		t.Fatalf("error subprocess output = %q", output)
	}
}
