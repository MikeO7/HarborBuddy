package updater

import (
	"sync"

	"github.com/MikeO7/HarborBuddy/internal/docker"
)

// failedTargetStage is the replacement stage at which a target image proved
// unable to start or become ready. Retrying the same image would stop the
// working container again every cycle for the same result.
const failedTargetStage = docker.FailureStageStartReplacement

// failedTargetRecord remembers, per container name, the target image whose
// replacement failed and was rolled back. It is in-memory only: a restart of
// HarborBuddy, or a newly pulled image ID, allows another attempt.
type failedTargetRecord struct {
	mu      sync.Mutex
	targets map[string]string
}

var failedTargets = newFailedTargetRecord()

func newFailedTargetRecord() *failedTargetRecord {
	return &failedTargetRecord{targets: make(map[string]string)}
}

func (r *failedTargetRecord) failed(name, targetImageID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	recorded, ok := r.targets[name]
	return ok && recorded == targetImageID
}

// observe updates the record from one replacement attempt's outcome.
func (r *failedTargetRecord) observe(name, targetImageID string, replaced docker.ReplaceResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case err == nil:
		delete(r.targets, name)
	case replaced.FailureStage == failedTargetStage:
		r.targets[name] = targetImageID
	}
}
