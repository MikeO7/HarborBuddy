package docker

import (
	"context"
	"testing"

	"github.com/moby/moby/api/types/network"
)

func TestDisconnectNetworksSkipsModesWithoutEndpoints(t *testing.T) {
	transport := newMockTransport()
	transport.register("POST", "/v1.41/networks/app/disconnect", noContent)
	client := testDockerClient(t, transport)
	current := replacementFixture()
	current.Networks = map[string]*network.EndpointSettings{
		"app":         {},
		"host":        {},
		"none":        {},
		"pasta":       {},
		"slirp4netns": {},
	}

	disconnected, err := client.disconnectNetworks(context.Background(), current)
	if err != nil {
		t.Fatalf("disconnectNetworks() error = %v", err)
	}
	if len(disconnected) != 1 || disconnected[0] != "app" {
		t.Fatalf("disconnectNetworks() = %v, want [app]", disconnected)
	}
}

func TestDetachableNetwork(t *testing.T) {
	for name, want := range map[string]bool{
		"bridge": true, "app_default": true,
		"host": false, "none": false, "pasta": false, "slirp4netns": false,
	} {
		if got := detachableNetwork(name); got != want {
			t.Errorf("detachableNetwork(%q) = %v, want %v", name, got, want)
		}
	}
}
