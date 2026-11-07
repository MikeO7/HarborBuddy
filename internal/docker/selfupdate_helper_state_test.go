package docker

import (
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
)

func TestHelperFinished(t *testing.T) {
	for state, want := range map[containertypes.ContainerState]bool{
		containertypes.StateRunning:    false,
		containertypes.StateRestarting: false,
		containertypes.StatePaused:     false,
		containertypes.StateCreated:    false,
		"":                             false,
		containertypes.StateExited:     true,
		containertypes.StateDead:       true,
		containertypes.StateRemoving:   true,
		"stopped":                      true,
	} {
		if got := helperFinished(state); got != want {
			t.Errorf("helperFinished(%q) = %v, want %v", state, got, want)
		}
	}
}
