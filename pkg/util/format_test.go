package util

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{
			name:     "Bytes",
			input:    500,
			expected: "500 B",
		},
		{
			name:     "Kilobytes",
			input:    1024,
			expected: "1.00 KB",
		},
		{
			name:     "Kilobytes fractional",
			input:    1536,
			expected: "1.50 KB",
		},
		{
			name:     "Megabytes",
			input:    1024 * 1024,
			expected: "1.00 MB",
		},
		{
			name:     "Gigabytes",
			input:    1024 * 1024 * 1024,
			expected: "1.00 GB",
		},
		{
			name:     "Terabytes",
			input:    1024 * 1024 * 1024 * 1024,
			expected: "1.00 TB",
		},
		{
			name:     "Zero",
			input:    0,
			expected: "0 B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatBytes(tt.input)
			if result != tt.expected {
				t.Errorf("FormatBytes(%d) = %s; want %s", tt.input, result, tt.expected)
			}
		})
	}
}
