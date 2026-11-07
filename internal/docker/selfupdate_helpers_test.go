package docker

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

func securityHardenedDaemon() ContainerDetails {
	return ContainerDetails{
		Config: &containertypes.Config{User: "65532:65532"},
		Host: &containertypes.HostConfig{
			Binds:          []string{"/var/run/docker.sock:/var/run/docker.sock"},
			NetworkMode:    "app",
			DNS:            []netip.Addr{netip.MustParseAddr("10.0.0.53")},
			DNSOptions:     []string{"ndots:1"},
			DNSSearch:      []string{"example.internal"},
			ExtraHosts:     []string{"docker.example.net:10.0.0.2"},
			GroupAdd:       []string{"998"},
			SecurityOpt:    []string{"no-new-privileges:true", "label=disable"},
			CapAdd:         []string{"CAP_DAC_OVERRIDE"},
			CapDrop:        []string{"ALL"},
			Privileged:     true,
			ReadonlyRootfs: true,
			UsernsMode:     "host",
			Runtime:        "runc",
			RestartPolicy:  containertypes.RestartPolicy{Name: "unless-stopped"},
		},
		Networks: map[string]*network.EndpointSettings{
			"app": {
				IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: netip.MustParseAddr("172.30.0.10")},
				MacAddress: network.HardwareAddr{0x02, 0x42, 0xac, 0x1e, 0x00, 0x0a},
				Aliases:    []string{"harborbuddy"},
				DNSNames:   []string{"harborbuddy"},
				Links:      []string{"db:database"},
				DriverOpts: map[string]string{"com.example.opt": "1"},
				GwPriority: 5,
				NetworkID:  "network-id",
				EndpointID: "endpoint-id",
				IPAddress:  netip.MustParseAddr("172.30.0.10"),
			},
			"empty": nil,
		},
	}
}

func helperRequest(host string) SelfUpdateHelperRequest {
	return SelfUpdateHelperRequest{Name: "helper", TargetContainerID: "self", TargetImageID: "sha256:new", DockerHost: host}
}

func TestSelfUpdateHelperPreservesDaemonSecuritySettings(t *testing.T) {
	current := securityHardenedDaemon()
	for _, dockerHost := range []string{"unix:///var/run/docker.sock", "tcp://docker.example.net:2376"} {
		config, host, _, err := selfUpdateHelperConfig(current, helperRequest(dockerHost))
		if err != nil {
			t.Fatalf("selfUpdateHelperConfig(%s) error = %v", dockerHost, err)
		}
		source := current.Host
		if config.User != current.Config.User || !slices.Equal(host.GroupAdd, source.GroupAdd) || !slices.Equal(host.SecurityOpt, source.SecurityOpt) ||
			!slices.Equal(host.CapAdd, source.CapAdd) || !slices.Equal(host.CapDrop, source.CapDrop) || host.Privileged != source.Privileged ||
			host.ReadonlyRootfs != source.ReadonlyRootfs || host.UsernsMode != source.UsernsMode || host.Runtime != source.Runtime {
			t.Fatalf("helper security settings for %s = user %q %+v, want daemon settings %+v", dockerHost, config.User, host, source)
		}
	}
}

func TestSocketHelperDoesNotJoinDaemonNetworks(t *testing.T) {
	_, host, networking, err := selfUpdateHelperConfig(securityHardenedDaemon(), helperRequest("unix:///var/run/docker.sock"))
	if err != nil {
		t.Fatalf("selfUpdateHelperConfig() error = %v", err)
	}
	if host.NetworkMode != "none" || networking != nil {
		t.Fatalf("socket helper networking = mode %q config %+v, want isolated", host.NetworkMode, networking)
	}
	if host.DNS != nil || host.DNSOptions != nil || host.DNSSearch != nil || host.ExtraHosts != nil {
		t.Fatalf("isolated helper must not carry resolver settings: %+v", host)
	}
	if len(host.Binds) != 1 {
		t.Fatalf("socket bind was not copied: %v", host.Binds)
	}
}

func TestRemoteDaemonHelperJoinsNetworksWithoutStaticAddressing(t *testing.T) {
	current := securityHardenedDaemon()
	_, host, networking, err := selfUpdateHelperConfig(current, helperRequest("tcp://docker.example.net:2376"))
	if err != nil {
		t.Fatalf("selfUpdateHelperConfig() error = %v", err)
	}
	if host.NetworkMode != current.Host.NetworkMode || !slices.Equal(host.DNS, current.Host.DNS) || !slices.Equal(host.DNSSearch, current.Host.DNSSearch) ||
		!slices.Equal(host.DNSOptions, current.Host.DNSOptions) || !slices.Equal(host.ExtraHosts, current.Host.ExtraHosts) {
		t.Fatalf("remote helper lost the daemon's network path: %+v", host)
	}
	if networking == nil || len(networking.EndpointsConfig) != 2 || networking.EndpointsConfig["empty"] != nil {
		t.Fatalf("remote helper networks = %+v", networking)
	}
	endpoint := networking.EndpointsConfig["app"]
	if endpoint.IPAMConfig != nil || endpoint.MacAddress != nil || endpoint.Aliases != nil || endpoint.DNSNames != nil ||
		endpoint.IPAddress.IsValid() || endpoint.NetworkID != "" || endpoint.EndpointID != "" {
		t.Fatalf("helper endpoint carries addressing that collides with the running daemon: %+v", endpoint)
	}
	if !slices.Equal(endpoint.Links, []string{"db:database"}) || endpoint.DriverOpts["com.example.opt"] != "1" || endpoint.GwPriority != 5 {
		t.Fatalf("helper endpoint lost non-addressing settings: %+v", endpoint)
	}
}

func TestRemoteDaemonHelperNetworkModesWithoutEndpoints(t *testing.T) {
	for _, mode := range []containertypes.NetworkMode{"host", "none"} {
		current := securityHardenedDaemon()
		current.Host.NetworkMode = mode
		_, host, networking, err := selfUpdateHelperConfig(current, helperRequest("tcp://docker.example.net:2376"))
		if err != nil || host.NetworkMode != mode || networking != nil {
			t.Fatalf("network mode %q: host mode %q networking %+v err %v", mode, host.NetworkMode, networking, err)
		}
	}
	current := securityHardenedDaemon()
	current.Networks = nil
	if _, _, networking, _ := selfUpdateHelperConfig(current, helperRequest("tcp://docker.example.net:2376")); networking != nil {
		t.Fatalf("daemon without networks produced helper networking %+v", networking)
	}
}

func TestListSelfUpdateHelpersIncludesFinishedHelpers(t *testing.T) {
	transport := newMockTransport()
	transport.register("GET", "/v1.41/containers/json", func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("all") != "1" || !strings.Contains(query.Get("filters"), SelfUpdateHelperLabel+"=true") {
			t.Fatalf("helper listing query = %v", query)
		}
		return jsonResponse(http.StatusOK, []containertypes.Summary{
			{ID: "finished", Names: []string{"/hb-harborbuddy-updater-1"}, State: containertypes.StateExited, Created: 100,
				Labels: map[string]string{SelfUpdateHelperLabel: "true", SelfUpdateTargetLabel: "self", SelfUpdateImageLabel: "sha256:new"}},
			{ID: "dead", State: containertypes.StateDead, Labels: map[string]string{SelfUpdateHelperLabel: "true"}},
			{ID: "running", State: containertypes.StateRunning, Labels: map[string]string{SelfUpdateHelperLabel: "true"}},
			{ID: "impostor", State: containertypes.StateExited, Labels: map[string]string{"app": "true"}},
		})
	})
	records, err := testDockerClient(t, transport).ListSelfUpdateHelpers(context.Background())
	if err != nil || len(records) != 3 {
		t.Fatalf("ListSelfUpdateHelpers() = %+v, %v", records, err)
	}
	want := SelfUpdateHelperRecord{ID: "finished", Name: "hb-harborbuddy-updater-1", TargetContainerID: "self", TargetImageID: "sha256:new", Finished: true, CreatedAt: time.Unix(100, 0)}
	if records[0] != want || !records[1].Finished || records[2].Finished || records[2].Name != "" {
		t.Fatalf("helper records = %+v", records)
	}

	failing := newMockTransport()
	failing.register("GET", "/v1.41/containers/json", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "daemon unavailable"})
	})
	if _, err := testDockerClient(t, failing).ListSelfUpdateHelpers(context.Background()); err == nil || !strings.Contains(err.Error(), "list self-update helpers") {
		t.Fatalf("ListSelfUpdateHelpers() error = %v", err)
	}
}

func TestRemoveSelfUpdateHelperNeverForces(t *testing.T) {
	transport := newMockTransport()
	transport.register("DELETE", "/v1.41/containers/finished", func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("force") == "1" {
			t.Fatal("helper removal must not force a running helper")
		}
		return noContent(request)
	})
	transport.register("DELETE", "/v1.41/containers/running", func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusConflict, map[string]string{"message": "container is running"})
	})
	client := testDockerClient(t, transport)
	if err := client.RemoveSelfUpdateHelper(context.Background(), "finished"); err != nil {
		t.Fatalf("RemoveSelfUpdateHelper(finished) error = %v", err)
	}
	if err := client.RemoveSelfUpdateHelper(context.Background(), "running"); err == nil || !strings.Contains(err.Error(), "remove self-update helper running") {
		t.Fatalf("RemoveSelfUpdateHelper(running) error = %v", err)
	}
}

type recoveryRoutes struct {
	updateStatus int
	running      bool
	inspectFails bool
	startStatus  int
}

func recoveryTransport(t *testing.T, routes recoveryRoutes) *mockTransport {
	t.Helper()
	transport := newMockTransport()
	transport.register("POST", "/v1.41/containers/self/update", func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"Name":"on-failure"`) || !strings.Contains(string(body), `"MaximumRetryCount":3`) {
			t.Fatalf("restart policy restore body = %s", body)
		}
		if routes.updateStatus != 0 {
			return jsonResponse(routes.updateStatus, map[string]string{"message": "update rejected"})
		}
		return jsonResponse(http.StatusOK, containertypes.UpdateResponse{})
	})
	transport.register("GET", "/v1.41/containers/self/json", func(*http.Request) (*http.Response, error) {
		if routes.inspectFails {
			return jsonResponse(http.StatusInternalServerError, map[string]string{"message": "inspect failed"})
		}
		return jsonResponse(http.StatusOK, inspectedContainer("self", "harborbuddy", "sha256:old", routes.running))
	})
	transport.register("POST", "/v1.41/containers/self/start", func(request *http.Request) (*http.Response, error) {
		if routes.startStatus != 0 {
			return jsonResponse(routes.startStatus, map[string]string{"message": "start rejected"})
		}
		return noContent(request)
	})
	return transport
}

func TestRecoverSelfUpdateTargetRestoresPolicyAndStartsStoppedDaemon(t *testing.T) {
	transport := recoveryTransport(t, recoveryRoutes{})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	policy := containertypes.RestartPolicy{Name: "on-failure", MaximumRetryCount: 3}
	if err := testDockerClient(t, transport).RecoverSelfUpdateTarget(cancelled, "self", policy); err != nil {
		t.Fatalf("RecoverSelfUpdateTarget() error = %v", err)
	}
	calls := transport.getCalls()
	if !slices.Equal(calls, []string{"POST /v1.41/containers/self/update", "GET /v1.41/containers/self/json", "POST /v1.41/containers/self/start"}) {
		t.Fatalf("recovery calls = %v", calls)
	}
}

func TestRecoverSelfUpdateTargetLeavesRunningDaemonAndReportsFailures(t *testing.T) {
	policy := containertypes.RestartPolicy{Name: "on-failure", MaximumRetryCount: 3}

	running := recoveryTransport(t, recoveryRoutes{running: true})
	if err := testDockerClient(t, running).RecoverSelfUpdateTarget(context.Background(), "self", policy); err != nil || slices.Contains(running.getCalls(), "POST /v1.41/containers/self/start") {
		t.Fatalf("running daemon recovery = %v calls %v", err, running.getCalls())
	}

	noPolicy := recoveryTransport(t, recoveryRoutes{running: true})
	if err := testDockerClient(t, noPolicy).RecoverSelfUpdateTarget(context.Background(), "self", containertypes.RestartPolicy{Name: "no"}); err != nil || slices.Contains(noPolicy.getCalls(), "POST /v1.41/containers/self/update") {
		t.Fatalf("disabled policy recovery = %v calls %v", err, noPolicy.getCalls())
	}

	failures := recoveryTransport(t, recoveryRoutes{updateStatus: http.StatusInternalServerError, startStatus: http.StatusInternalServerError})
	err := testDockerClient(t, failures).RecoverSelfUpdateTarget(context.Background(), "self", policy)
	if err == nil || !strings.Contains(err.Error(), "restore original restart policy") || !strings.Contains(err.Error(), "restart original container") {
		t.Fatalf("recovery failure error = %v", err)
	}

	inspect := recoveryTransport(t, recoveryRoutes{inspectFails: true})
	if err := testDockerClient(t, inspect).RecoverSelfUpdateTarget(context.Background(), "self", policy); err == nil || !strings.Contains(err.Error(), "inspect container self") {
		t.Fatalf("recovery inspect error = %v", err)
	}
}
