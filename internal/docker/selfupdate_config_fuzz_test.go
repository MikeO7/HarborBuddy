package docker

import (
	"strings"
	"testing"
)

func FuzzHelperDockerEnvironmentFiltersSecrets(f *testing.F) {
	f.Add("SECRET=value", "unix:///var/run/docker.sock")
	f.Add("DOCKER_HOST=tcp://docker:2376", "")
	f.Add("DOCKER_CERT_PATH=/certs/client", "tcp://remote:2376")

	f.Fuzz(func(t *testing.T, entry, configuredHost string) {
		environment, host, _ := helperDockerEnvironment([]string{entry}, configuredHost)
		if host == "" {
			t.Fatal("helper Docker host must never be empty")
		}
		allowed := map[string]bool{
			"HARBORBUDDY_DOCKER_HOST": true,
			"DOCKER_HOST":             true,
			"DOCKER_TLS_VERIFY":       true,
			"DOCKER_CERT_PATH":        true,
			"DOCKER_API_VERSION":      true,
			"DOCKER_CONTEXT":          true,
			"HARBORBUDDY_LOG_LEVEL":   true,
			"HARBORBUDDY_LOG_JSON":    true,
		}
		for _, item := range environment {
			key, _, ok := strings.Cut(item, "=")
			if !ok || !allowed[key] {
				t.Fatalf("unapproved helper environment entry %q", item)
			}
		}
	})
}

func FuzzPathRelevant(f *testing.F) {
	f.Add("/var/run/docker.sock", "/var/run/docker.sock")
	f.Add("/run/docker", "/run/docker/client/cert.pem")
	f.Add("", "/run/docker.sock")

	f.Fuzz(func(t *testing.T, target, relevant string) {
		got := pathRelevant(target, []string{relevant})
		if target == "" && got {
			t.Fatal("empty target must not be relevant")
		}
		if target != "" && target == relevant && !got {
			t.Fatalf("identical paths were not relevant: %q", target)
		}
	})
}
