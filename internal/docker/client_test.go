package docker

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	// Create a mock Docker server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		}
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ApiVersion":"1.41"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Test connecting to the mock server
	// Docker SDK expects tcp:// for HTTP
	host := "tcp://" + server.Listener.Addr().String()

	client, err := NewClient(host)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	if client.cli == nil {
		t.Error("client.cli is nil")
	}
}

func TestNewClient_Fail(t *testing.T) {
	// Test with invalid host
	_, err := NewClient("invalid-protocol://host")
	if err == nil {
		t.Error("Expected error for invalid host, got nil")
	}
}

func TestNewClient_PingFail(t *testing.T) {
	// Create a server that fails pings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	host := "tcp://" + server.Listener.Addr().String()

	_, err := NewClient(host)
	if err == nil {
		t.Error("Expected error when ping fails, got nil")
	}
}
