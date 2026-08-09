package logging

import "strings"

const maxFieldLength = 256

// BoundedField keeps daemon-provided descriptions useful without allowing one
// Docker resource to dominate an operational log stream.
func BoundedField(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) <= maxFieldLength {
		return value, false
	}
	return value[:maxFieldLength] + "…", true
}
