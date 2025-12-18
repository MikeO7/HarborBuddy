package scheduler

import (
	"context"
	"os"
	"syscall"

	"github.com/MikeO7/HarborBuddy/pkg/log"
)

// handleSignals handles incoming OS signals
func handleSignals(sigChan <-chan os.Signal, cancel context.CancelFunc) {
	for {
		sig := <-sigChan
		if sig == syscall.SIGUSR1 {
			log.ToggleDebug()
			continue
		}
		log.Infof("Received signal %v, shutting down gracefully...", sig)
		cancel()
		return
	}
}
