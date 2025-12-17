package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

type mockTransport struct {
	mu       sync.Mutex
	handlers map[string]func(*http.Request) (*http.Response, error)
	calls    []string
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		handlers: make(map[string]func(*http.Request) (*http.Response, error)),
		calls:    make([]string, 0),
	}
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	key := req.Method + " " + req.URL.Path
	m.calls = append(m.calls, key)
	handler, ok := m.handlers[key]
	m.mu.Unlock()

	if ok {
		return handler(req)
	}

	// Try pattern matching if exact match fails
    // This is useful if we use prefixes
	m.mu.Lock()
	for k, h := range m.handlers {
		if strings.HasSuffix(k, "*") {
			prefix := strings.TrimSuffix(k, "*")
			if strings.HasPrefix(key, prefix) {
				m.mu.Unlock()
				return h(req)
			}
		}
	}
	m.mu.Unlock()

	return &http.Response{
		StatusCode: 404,
		Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"message": "page not found: %s"}`, key))),
		Header:     make(http.Header),
	}, nil
}

func (m *mockTransport) register(method, path string, handler func(*http.Request) (*http.Response, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[method+" "+path] = handler
}

func (m *mockTransport) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy to avoid races
	calls := make([]string, len(m.calls))
	copy(calls, m.calls)
	return calls
}

// Helper to create JSON response
func jsonResponse(statusCode int, body interface{}) (*http.Response, error) {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader(b)),
		Header:     make(http.Header),
	}, nil
}
