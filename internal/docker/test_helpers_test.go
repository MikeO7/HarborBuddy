package docker

import "strings"

// contextPath extracts the context path (ignoring version prefix if present)
// e.g., /v1.41/containers/json -> /containers/json
func contextPath(path string) string {
	if strings.HasPrefix(path, "/v") {
		parts := strings.SplitN(path, "/", 3)
		if len(parts) == 3 {
			return "/" + parts[2]
		}
	}
	return path
}
