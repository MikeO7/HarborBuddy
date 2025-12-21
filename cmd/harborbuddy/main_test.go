package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected appConfig
		wantErr  bool
	}{
		{
			name: "defaults",
			args: []string{},
			expected: appConfig{
				configPath: "/config/harborbuddy.yml",
			},
		},
		{
			name: "override config path",
			args: []string{"--config", "/custom/config.yml"},
			expected: appConfig{
				configPath: "/custom/config.yml",
			},
		},
		{
			name: "set interval",
			args: []string{"--interval", "30m"},
			expected: appConfig{
				configPath: "/config/harborbuddy.yml",
				interval:   30 * time.Minute,
			},
		},
		{
			name: "dry run",
			args: []string{"--dry-run"},
			expected: appConfig{
				configPath: "/config/harborbuddy.yml",
				dryRun:     true,
			},
		},
		{
			name: "updater mode",
			args: []string{"--updater-mode", "--target-container-id", "123", "--new-image-id", "nginx:latest"},
			expected: appConfig{
				configPath:  "/config/harborbuddy.yml",
				updaterMode: true,
				targetID:    "123",
				newImage:    "nginx:latest",
			},
		},
		{
			name:    "invalid flag",
			args:    []string{"--invalid-flag"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFlags(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseFlags() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			// Verify specific fields we care about
			if got.configPath != tt.expected.configPath {
				t.Errorf("configPath = %v, want %v", got.configPath, tt.expected.configPath)
			}
			if tt.expected.interval != 0 && got.interval != tt.expected.interval {
				t.Errorf("interval = %v, want %v", got.interval, tt.expected.interval)
			}
			if tt.expected.dryRun && !got.dryRun {
				t.Errorf("dryRun = %v, want %v", got.dryRun, tt.expected.dryRun)
			}
			if tt.expected.updaterMode && !got.updaterMode {
				t.Errorf("updaterMode = %v, want %v", got.updaterMode, tt.expected.updaterMode)
			}
		})
	}
}

func TestRun_Version(t *testing.T) {
	// Capture stdout
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)

	// Run with --version
	exitCode := run(context.Background(), []string{"--version"}, stdout, stderr)

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	output := stdout.String()
	if !strings.Contains(output, "HarborBuddy version") {
		t.Errorf("Expected version output, got: %s", output)
	}
}
