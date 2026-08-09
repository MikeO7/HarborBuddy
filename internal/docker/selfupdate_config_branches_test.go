package docker

import (
	"reflect"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func TestSelfUpdateHelperConfigRejectsIncompleteInputs(t *testing.T) {
	validCurrent := ContainerDetails{Config: &containertypes.Config{}, Host: &containertypes.HostConfig{}}
	validRequest := SelfUpdateHelperRequest{Name: "helper", TargetContainerID: "target", TargetImageID: "image"}
	for _, test := range []struct {
		name    string
		current ContainerDetails
		request SelfUpdateHelperRequest
		want    string
	}{
		{name: "missing config", current: ContainerDetails{Host: validCurrent.Host}, request: validRequest, want: "inspection is incomplete"},
		{name: "missing host", current: ContainerDetails{Config: validCurrent.Config}, request: validRequest, want: "inspection is incomplete"},
		{name: "missing name", current: validCurrent, request: SelfUpdateHelperRequest{TargetContainerID: "target", TargetImageID: "image"}, want: "helper name"},
		{name: "missing target container", current: validCurrent, request: SelfUpdateHelperRequest{Name: "helper", TargetImageID: "image"}, want: "target container"},
		{name: "missing target image", current: validCurrent, request: SelfUpdateHelperRequest{Name: "helper", TargetContainerID: "target"}, want: "target image"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := selfUpdateHelperConfig(test.current, test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("selfUpdateHelperConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSelfUpdateEnvironmentAndPathFilteringPrecedence(t *testing.T) {
	source := []string{
		"SECRET=hidden", "DOCKER_HOST=tcp://docker:2375", "DOCKER_HOST=tcp://last:2375",
		"HARBORBUDDY_DOCKER_HOST=unix:///custom.sock", "DOCKER_CERT_PATH=/certs", "MALFORMED",
	}
	environment, host, certPath := helperDockerEnvironment(source, "")
	if host != "unix:///custom.sock" || certPath != "/certs" || !containsString(environment, "DOCKER_HOST=tcp://last:2375") || !containsString(environment, "HARBORBUDDY_DOCKER_HOST=unix:///custom.sock") || containsString(environment, "SECRET=hidden") {
		t.Fatalf("helper environment = %v host=%q cert=%q", environment, host, certPath)
	}
	environment, host, _ = helperDockerEnvironment([]string{"DOCKER_HOST=tcp://docker:2375"}, "tcp://configured:2376")
	if host != "tcp://configured:2376" || !containsString(environment, "HARBORBUDDY_DOCKER_HOST=tcp://configured:2376") {
		t.Fatalf("configured host precedence = %v host=%q", environment, host)
	}
	if dockerSocketPath("tcp://docker:2375") != "" || dockerSocketPath("unix:///run/docker.sock") != "/run/docker.sock" {
		t.Fatalf("dockerSocketPath precedence failed")
	}
	if got := dockerSocketPath("unix://relative/../socket"); got != "socket" {
		t.Fatalf("dockerSocketPath clean = %q", got)
	}
}

func TestHelperMountAndPathFiltersCoverSyntaxEdges(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "", want: ""},
		{value: "only-one", want: ""},
		{value: "/source:/target", want: "/target"},
		{value: "/source:/target:ro,z", want: "/target"},
		{value: "/source:/target:invalid", want: "invalid"},
	} {
		if got := bindTarget(test.value); got != test.want {
			t.Errorf("bindTarget(%q) = %q, want %q", test.value, got, test.want)
		}
	}
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "", want: false}, {value: "ro,z", want: true}, {value: "rw", want: true}, {value: "bad", want: false},
	} {
		if got := isBindMode(test.value); got != test.want {
			t.Errorf("isBindMode(%q) = %v, want %v", test.value, got, test.want)
		}
	}
	if pathRelevant("", []string{"/socket"}) || pathRelevant("/other", []string{""}) || pathRelevant("/other", []string{"/socket"}) {
		t.Fatal("pathRelevant accepted an irrelevant path")
	}
	if !pathRelevant("/run", []string{"/run/docker.sock"}) || !pathRelevant("/run/docker.sock", []string{"/run"}) || !pathRelevant("/run/docker.sock", []string{"/run/docker.sock"}) {
		t.Fatal("pathRelevant rejected an ancestor or exact path")
	}
	if containsString([]string{"one", "two"}, "three") || !containsString([]string{"one", "two"}, "two") {
		t.Fatal("containsString boundary failed")
	}
	if !reflect.DeepEqual(relevantBinds([]string{"/a:/run/socket:ro", "/b:/other:ro"}, []string{"/run/socket"}), []string{"/a:/run/socket:ro"}) {
		t.Fatal("relevantBinds filtering failed")
	}
	if len(relevantMounts([]mount.Mount{{Target: "/run/socket"}, {Target: "/other"}}, []string{"/run"})) != 1 {
		t.Fatal("relevantMounts filtering failed")
	}
}
