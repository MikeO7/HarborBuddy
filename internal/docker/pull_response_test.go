package docker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPullResponseRejectedBeforeImageInspection(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"stream error", `{"errorDetail":{"message":"registry denied"},"error":"registry denied"}`, "registry denied"},
		{"legacy error", `{"error":"registry denied"}`, "registry denied"},
		{"null", `null`, "missing pull message"},
		{"missing fields", `{}`, "missing pull message"},
		{"malformed", `{"status":`, "unexpected EOF"},
		{"wrong type", `[]`, "cannot unmarshal array"},
		{"empty", " \n", "empty pull response"},
		{"error after long progress stream", strings.Repeat(`{"status":"`+strings.Repeat("x", 1024*1024)+`"}`+"\n", 17) + `{"error":"late failure"}`, "late failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := newMockTransport()
			transport.register("POST", "/v1.41/images/create", func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			cli := testDockerClient(t, transport)
			if _, err := cli.PullImage(context.Background(), "app:latest"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			calls := transport.getCalls()
			if len(calls) != 1 || calls[0] != "POST /v1.41/images/create" {
				t.Fatalf("rejected stream touched cached image: %v", calls)
			}
		})
	}
}
