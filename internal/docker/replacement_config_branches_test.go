package docker

import (
	"context"
	"net/http"
	"net/netip"
	"reflect"
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func TestReplacementConfigClearsImageDefaultsAndPreservesDistinctOverrides(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/images/sha256:old/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{
			"Id": "sha256:old",
			"Config": map[string]any{
				"Cmd":         []string{"serve"},
				"Entrypoint":  []string{"/entry"},
				"Healthcheck": map[string]any{"Test": []string{"CMD", "true"}},
			},
		})
	})
	client := testDockerClient(t, transport)
	current := replacementFixture()
	zeroSwappiness := int64(0)
	current.Host.MemorySwappiness = &zeroSwappiness
	current.Config.Cmd = []string{"serve"}
	current.Config.Entrypoint = []string{"/entry"}
	current.Config.Healthcheck = &containertypes.HealthConfig{Test: []string{"CMD", "true"}}
	current.Config.Hostname = " OLD "
	current.Summary.ID = "old-container"
	config, host, _ := client.replacementConfig(context.Background(), current)
	if config.Cmd != nil || config.Entrypoint != nil || config.Healthcheck != nil {
		t.Fatalf("image defaults were not cleared: %+v", config)
	}
	if host.MemorySwappiness != nil {
		t.Fatalf("engine-generated zero memory swappiness was preserved: %v", *host.MemorySwappiness)
	}

	current.Config.Cmd = []string{"custom"}
	current.Config.Entrypoint = []string{"/custom"}
	current.Config.Healthcheck = &containertypes.HealthConfig{Test: []string{"CMD", "custom"}}
	config, _, _ = client.replacementConfig(context.Background(), current)
	if !reflect.DeepEqual(config.Cmd, current.Config.Cmd) || !reflect.DeepEqual(config.Entrypoint, current.Config.Entrypoint) || !reflect.DeepEqual(config.Healthcheck, current.Config.Healthcheck) {
		t.Fatalf("explicit config was cleared: %+v", config)
	}

	current = replacementFixture()
	current.Networks = map[string]*network.EndpointSettings{"bridge": {NetworkID: "network", IPAddress: netip.MustParseAddr("192.0.2.1"), MacAddress: network.HardwareAddr{0x02, 0x42, 0xac, 0x11, 0x00, 0x02}}, "nil": nil}
	_, _, networking := client.replacementConfig(context.Background(), current)
	if networking.EndpointsConfig["nil"] != nil || networking.EndpointsConfig["bridge"].IPAddress.IsValid() || networking.EndpointsConfig["bridge"].NetworkID != "" {
		t.Fatalf("network endpoints were not sanitized: %+v", networking)
	}
}

func TestPreserveInspectedMountsAndReusableMountConversions(t *testing.T) {
	host := &containertypes.HostConfig{Binds: []string{"/host/existing:/existing:ro"}, Mounts: []mount.Mount{{Target: "/configured"}}}
	inspected := []containertypes.MountPoint{
		{Destination: ""},
		{Type: mount.TypeVolume, Name: "vol", Destination: "/vol", RW: true, Mode: "nocopy"},
		{Type: mount.TypeBind, Source: "/host/bind", Destination: "/bind", Propagation: "rshared"},
		{Type: mount.TypeTmpfs, Destination: "/tmp"},
		{Type: mount.TypeVolume, Name: "duplicate", Destination: "/configured"},
		{Type: mount.TypeImage, Destination: "/unsupported"},
	}
	preserveInspectedMounts(host, inspected)
	if len(host.Mounts) != 4 {
		t.Fatalf("preserved mounts = %+v, want 4", host.Mounts)
	}
	if host.Mounts[1].Source != "vol" || host.Mounts[1].VolumeOptions == nil || !host.Mounts[1].VolumeOptions.NoCopy {
		t.Fatalf("volume mount conversion = %+v", host.Mounts[1])
	}
	if host.Mounts[2].Source != "/host/bind" || host.Mounts[2].BindOptions == nil || host.Mounts[2].BindOptions.Propagation != "rshared" {
		t.Fatalf("bind mount conversion = %+v", host.Mounts[2])
	}
	if _, ok := reusableMount(containertypes.MountPoint{Type: mount.TypeNamedPipe, Destination: "/pipe"}); ok {
		t.Fatal("named pipe mount was marked reusable")
	}
	if _, ok := reusableMount(containertypes.MountPoint{Type: mount.Type("unknown"), Destination: "/unknown"}); ok {
		t.Fatal("unknown mount was marked reusable")
	}
}

func TestSanitizedEndpointPreservesIPAMMacButClearsRuntimeIdentity(t *testing.T) {
	settings := &network.EndpointSettings{
		NetworkID: "network", EndpointID: "endpoint", Gateway: netip.MustParseAddr("192.0.2.1"),
		IPAddress: netip.MustParseAddr("192.0.2.2"), IPPrefixLen: 24, MacAddress: network.HardwareAddr{0x02, 0x42, 0xac, 0x11, 0x00, 0x02},
		Aliases: []string{"app"}, IPAMConfig: &network.EndpointIPAMConfig{},
	}
	cloned := sanitizedEndpoint(settings)
	if cloned == nil || cloned.NetworkID != "" || cloned.EndpointID != "" || cloned.IPAddress.IsValid() || cloned.Gateway.IsValid() || len(cloned.MacAddress) == 0 {
		t.Fatalf("sanitized endpoint = %+v", cloned)
	}
	if sanitizedEndpoint(nil) != nil {
		t.Fatal("sanitizedEndpoint(nil) must return nil")
	}
}
