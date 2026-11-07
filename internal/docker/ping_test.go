package docker

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestPingReportsDaemonAvailability(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "up", status: http.StatusOK},
		{name: "down", status: http.StatusInternalServerError, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newMockTransport()
			for _, method := range []string{"HEAD", "GET"} {
				transport.register(method, "/_ping", func(*http.Request) (*http.Response, error) {
					return jsonResponse(test.status, map[string]string{"message": "ping"})
				})
			}
			err := testDockerClient(t, transport).Ping(context.Background())
			if test.wantErr != (err != nil) || (err != nil && !strings.Contains(err.Error(), "ping Docker daemon")) {
				t.Fatalf("Ping() error = %v", err)
			}
		})
	}
}
