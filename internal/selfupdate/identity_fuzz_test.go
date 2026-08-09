package selfupdate

import (
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

func FuzzDetectCurrentContainer(f *testing.F) {
	f.Add("aaaaaaaaaaaa", "aaaaaaaaaaaa1111", "bbbbbbbbbbbb2222", "")
	f.Add("", "aaaaaaaaaaaa1111", "aaaaaaaaaaaa2222", "aaaaaaaaaaaa")
	f.Add("cccccccccccc", "aaaaaaaaaaaa1111", "bbbbbbbbbbbb2222", "runtime/aaaaaaaaaaaa1111")

	f.Fuzz(func(t *testing.T, hostname, firstID, secondID, runtimeIdentity string) {
		containers := []docker.ContainerSummary{
			{ID: firstID, Name: "first"},
			{ID: secondID, Name: "second"},
		}
		got := detectCurrentContainer(containers, hostname, []byte(runtimeIdentity))
		if got != "" && got != firstID && got != secondID {
			t.Fatalf("detected ID %q is not present in the input set", got)
		}
		if firstID != secondID && got != "" && normalizedContainerID(firstID) == normalizedContainerID(secondID) {
			t.Fatalf("ambiguous normalized IDs produced match %q", got)
		}
	})
}

func FuzzUniquePrefixMatchRejectsShortIdentity(f *testing.F) {
	f.Add("short")
	f.Add("12345678901")
	f.Fuzz(func(t *testing.T, identity string) {
		if len(normalizedContainerID(identity)) >= 12 {
			t.Skip()
		}
		containers := []docker.ContainerSummary{{ID: "123456789012abcdef"}}
		if got := uniquePrefixMatch(containers, identity); got != "" {
			t.Fatalf("short identity %q matched %q", identity, got)
		}
	})
}
