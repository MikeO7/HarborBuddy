package config

import (
	"fmt"
	"strings"
)

// KeepsVolumeLabels reports whether a volume's labels match cleanup.keep_volume_labels. An entry
// "key=value" needs that exact value; an entry "key" matches the key with any value.
func (c CleanupConfig) KeepsVolumeLabels(labels map[string]string) bool {
	for _, entry := range c.KeepVolumeLabels {
		key, want, exact := strings.Cut(entry, "=")
		if got, ok := labels[key]; ok && (!exact || got == want) {
			return true
		}
	}
	return false
}

func validateKeepVolumeLabels(entries []string) error {
	for _, entry := range entries {
		if key, _, _ := strings.Cut(entry, "="); key == "" {
			return fmt.Errorf("cleanup.keep_volume_labels entry %q needs a label key", entry)
		}
	}
	return nil
}
