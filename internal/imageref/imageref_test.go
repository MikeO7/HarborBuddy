package imageref

import (
	"slices"
	"strings"
	"testing"
)

func TestVariantsIncludeRawFamiliarAndQualifiedForms(t *testing.T) {
	tests := []struct {
		ref  string
		want []string
	}{
		{ref: "nginx", want: []string{"nginx", "nginx:latest", "docker.io/library/nginx:latest"}},
		{ref: "nginx:1.27", want: []string{"nginx:1.27", "docker.io/library/nginx:1.27"}},
		{ref: "docker.io/library/nginx:1.27", want: []string{"docker.io/library/nginx:1.27", "nginx:1.27"}},
		{ref: "ghcr.io/org/app", want: []string{"ghcr.io/org/app", "ghcr.io/org/app:latest"}},
		{ref: "sha256:" + strings.Repeat("a", 64), want: []string{"sha256:" + strings.Repeat("a", 64)}},
		{ref: "Not A Reference", want: []string{"Not A Reference"}},
	}
	for _, test := range tests {
		if got := Variants(test.ref); !slices.Equal(got, test.want) {
			t.Errorf("Variants(%q) = %q, want %q", test.ref, got, test.want)
		}
	}
}

func TestNormalizeUsesQualifiedTaggedForm(t *testing.T) {
	for ref, want := range map[string]string{
		"nginx":                          "docker.io/library/nginx:latest",
		"nginx:latest":                   "docker.io/library/nginx:latest",
		"docker.io/library/nginx:latest": "docker.io/library/nginx:latest",
		"ghcr.io/org/app:1":              "ghcr.io/org/app:1",
		"INVALID":                        "INVALID",
	} {
		if got := Normalize(ref); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestMatchAnyTriesEveryVariant(t *testing.T) {
	equal := func(image, pattern string) bool { return image == pattern }
	if !MatchAny("nginx", "docker.io/library/nginx:latest", equal) || !MatchAny("nginx", "nginx:latest", equal) {
		t.Fatal("MatchAny did not try normalized variants")
	}
	if MatchAny("nginx", "redis:latest", equal) {
		t.Fatal("MatchAny matched an unrelated pattern")
	}
}

func TestIsImageIDRecognizesFullIDsOnly(t *testing.T) {
	hex := strings.Repeat("0123456789abcdef", 4)
	for ref, want := range map[string]bool{
		"sha256:" + hex:                  true,
		hex:                              true,
		hex[:12]:                         false,
		"nginx:latest":                   false,
		"sha256:" + strings.ToUpper(hex): false,
	} {
		if got := IsImageID(ref); got != want {
			t.Errorf("IsImageID(%q) = %v, want %v", ref, got, want)
		}
	}
}
