package logging

import (
	"strings"
	"unicode/utf8"
)

const maxFieldLength = 256

// BoundedField keeps daemon-provided descriptions useful without allowing one
// Docker resource to dominate an operational log stream.
func BoundedField(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) <= maxFieldLength {
		return value, false
	}
	cut := maxFieldLength
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "…", true
}
