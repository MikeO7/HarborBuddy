// Package imageref interprets Docker image references the way the Docker CLI
// does, so filters and caches treat "nginx", "nginx:latest", and
// "docker.io/library/nginx:latest" as the same image.
package imageref

import (
	"regexp"

	"github.com/distribution/reference"
)

var imageIDPattern = regexp.MustCompile(`^(sha256:)?[a-f0-9]{64}$`)

// Variants returns the distinct forms a pattern may match: the raw reference,
// its familiar form with an implicit ":latest" tag (nginx:latest), and its
// fully qualified form (docker.io/library/nginx:latest). A reference that
// cannot be parsed yields only the raw value.
func Variants(ref string) []string {
	variants := []string{ref}
	named, ok := parse(ref)
	if !ok {
		return variants
	}
	named = reference.TagNameOnly(named)
	for _, candidate := range []string{reference.FamiliarString(named), named.String()} {
		if !contains(variants, candidate) {
			variants = append(variants, candidate)
		}
	}
	return variants
}

// Normalize returns the fully qualified form of ref with an implicit
// ":latest" tag, or ref unchanged when it cannot be parsed.
func Normalize(ref string) string {
	named, ok := parse(ref)
	if !ok {
		return ref
	}
	return reference.TagNameOnly(named).String()
}

// parse rejects image IDs, which the reference grammar would otherwise read
// as a repository named "sha256" with a hex tag.
func parse(ref string) (reference.Named, bool) {
	if IsImageID(ref) {
		return nil, false
	}
	named, err := reference.ParseNormalizedNamed(ref)
	return named, err == nil
}

// MatchAny reports whether match accepts any variant of ref for pattern.
func MatchAny(ref, pattern string, match func(image, pattern string) bool) bool {
	for _, variant := range Variants(ref) {
		if match(variant, pattern) {
			return true
		}
	}
	return false
}

// IsImageID reports whether ref is a full image ID ("sha256:<hex>" or bare
// 64-character hex) rather than a pullable repository reference.
func IsImageID(ref string) bool {
	return imageIDPattern.MatchString(ref)
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
