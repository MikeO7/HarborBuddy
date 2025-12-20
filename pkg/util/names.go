package util

// GetImageFriendlyName tries to find a human-readable name from image labels
func GetImageFriendlyName(labels map[string]string) string {
	if labels == nil {
		return ""
	}

	// Priority list of labels to check
	keys := []string{
		"org.opencontainers.image.title",
		"org.label-schema.name",
		"com.docker.compose.service",
		"io.portainer.access.control", // Sometimes used for stack/service names
		"name",
	}

	for _, key := range keys {
		if val, ok := labels[key]; ok && val != "" {
			return val
		}
	}
	return ""
}
