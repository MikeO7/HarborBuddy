package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
)

func decodeJSON(request *http.Request, target any) error {
	return json.NewDecoder(request.Body).Decode(target)
}

const providerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func dependentsTransport(t *testing.T, others map[string]*containertypes.HostConfig) *mockTransport {
	t.Helper()
	transport := newMockTransport()
	items := []containertypes.Summary{{ID: providerID, Names: []string{"/gluetun"}}}
	for name, host := range others {
		id := name + "-id"
		items = append(items, containertypes.Summary{ID: id, Names: []string{"/" + name}})
		inspected := inspectedContainer(id, name, "sha256:img", true)
		inspected.HostConfig = host
		transport.register("GET", "/v1.41/containers/"+id+"/json", func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, inspected)
		})
	}
	transport.register("GET", "/v1.41/containers/json", func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("all") != "1" {
			t.Errorf("dependents check must include stopped containers; query=%v", request.URL.Query())
		}
		return jsonResponse(http.StatusOK, items)
	})
	return transport
}

func providerDetails() ContainerDetails {
	current := replacementFixture()
	current.Summary.ID = providerID
	current.Summary.Name = "gluetun"
	return current
}

func TestCheckNamespaceDependentsRejectsProvidersOfSharedNamespacesAndLinks(t *testing.T) {
	tests := []struct {
		name string
		host *containertypes.HostConfig
		want string
	}{
		{name: "network by full id", host: &containertypes.HostConfig{NetworkMode: "container:" + providerID}, want: "network namespace"},
		{name: "pid by name", host: &containertypes.HostConfig{PidMode: "container:gluetun"}, want: "PID namespace"},
		{name: "ipc by id prefix", host: &containertypes.HostConfig{IpcMode: "container:0123456789ab"}, want: "IPC namespace"},
		{name: "uts by slash name", host: &containertypes.HostConfig{UTSMode: "container:/gluetun"}, want: "UTS namespace"},
		{name: "link", host: &containertypes.HostConfig{Links: []string{"/gluetun:/qbittorrent/vpn"}}, want: "link"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := testDockerClient(t, dependentsTransport(t, map[string]*containertypes.HostConfig{"qbittorrent": test.host}))
			err := client.CheckNamespaceDependents(context.Background(), providerDetails())
			if !IsUnsupported(err) || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "qbittorrent") {
				t.Fatalf("CheckNamespaceDependents() error = %v, want unsupported %q", err, test.want)
			}
		})
	}
}

func TestCheckNamespaceDependentsAllowsUnrelatedAndVanishedContainers(t *testing.T) {
	transport := dependentsTransport(t, map[string]*containertypes.HostConfig{
		"web":   {NetworkMode: "bridge", Links: []string{"/other:/web/other"}},
		"bare":  nil,
		"other": {NetworkMode: "container:web-id"},
		// "01" is another container's name, so it does not resolve as an ID prefix.
		"01":       {},
		"sidecar":  {PidMode: "container:01"},
		"emptyref": {NetworkMode: "container:"},
	})
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{
			{ID: providerID, Names: []string{"/gluetun"}},
			{ID: "web-id", Names: []string{"/web"}}, {ID: "bare-id", Names: []string{"/bare"}},
			{ID: "other-id", Names: []string{"/other"}}, {ID: "01-id", Names: []string{"/01"}},
			{ID: "sidecar-id", Names: []string{"/sidecar"}}, {ID: "emptyref-id", Names: []string{"/emptyref"}},
			{ID: "gone-id", Names: []string{"/gone"}},
		})
	})
	transport.register("GET", "/v1.41/containers/gone-id/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, map[string]string{"message": "No such container"})
	})
	client := testDockerClient(t, transport)
	if err := client.CheckNamespaceDependents(context.Background(), providerDetails()); err != nil {
		t.Fatalf("CheckNamespaceDependents() error = %v", err)
	}
}

func TestCheckNamespaceDependentsReportsDockerFailures(t *testing.T) {
	transport := newMockTransport()
	registerServerError(transport, "GET", "/v1.41/containers/json", "list failed")
	client := testDockerClient(t, transport)
	if err := client.CheckNamespaceDependents(context.Background(), providerDetails()); err == nil || IsUnsupported(err) || !strings.Contains(err.Error(), "list containers") {
		t.Fatalf("list failure = %v", err)
	}

	transport = dependentsTransport(t, nil)
	transport.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []containertypes.Summary{{ID: "broken-id", Names: []string{"/broken"}}})
	})
	registerServerError(transport, "GET", "/v1.41/containers/broken-id/json", "inspect failed")
	client = testDockerClient(t, transport)
	if err := client.CheckNamespaceDependents(context.Background(), providerDetails()); err == nil || IsUnsupported(err) || !strings.Contains(err.Error(), "inspect container broken-id") {
		t.Fatalf("inspect failure = %v", err)
	}
}
