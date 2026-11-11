package logging

import (
	"strings"
	"unicode/utf8"
)

const maxFieldLength = 256

// ShortID shortens a container or image ID to the 12 characters Docker shows,
// without the "sha256:" prefix.
func ShortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

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
