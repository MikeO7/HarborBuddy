package docker

import (
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	containertypes "github.com/moby/moby/api/types/container"
)

// FailureStageCancelledBeforeStart reports that shutdown was requested before
// the replacement touched the original container.
const FailureStageCancelledBeforeStart = "cancelled_before_start"

const (
	backupNameMarker = "-harborbuddy-backup-"
	// transactionSlack covers Docker API round trips beyond the configured
	// stop and startup waits.
	transactionSlack = time.Minute
	// rollbackSlack bounds rollback calls other than stopping the replacement.
	rollbackSlack = 30 * time.Second
)

// backupNamePattern matches "<name>-harborbuddy-backup-<nanos>" with an
// optional ".<restart policy>[.<max retries>]" suffix that records the policy
// HarborBuddy suppressed, so a stranded backup can be restored after a crash.
var backupNamePattern = regexp.MustCompile(`^(.+)` + backupNameMarker + `([0-9]+)(?:\.(always|unless-stopped|on-failure)(?:\.([0-9]+))?)?$`)

var activeTransactions sync.Map

// beginTransaction marks a container as being replaced by this process so
// stranded-backup recovery leaves it alone; call the result to release it.
func beginTransaction(containerID string) func() {
	activeTransactions.Store(containerID, struct{}{})
	return func() { activeTransactions.Delete(containerID) }
}

func transactionActive(containerID string) bool {
	_, active := activeTransactions.Load(containerID)
	return active
}

// transactionTimeout bounds a whole replacement: stopping the original,
// waiting for readiness, and stopping a failed replacement during rollback.
func transactionTimeout(options ReplaceOptions) time.Duration {
	return 2*options.StopTimeout + options.StartupTimeout + options.StabilizationTime + transactionSlack
}

func rollbackTimeout(timeoutSeconds int) time.Duration {
	return time.Duration(timeoutSeconds)*time.Second + rollbackSlack
}

func backupContainerName(name string, now time.Time, restartSuppressed bool, policy containertypes.RestartPolicy) string {
	backup := fmt.Sprintf("%s%s%d", name, backupNameMarker, now.UnixNano())
	if !restartSuppressed {
		return backup
	}
	backup += "." + string(policy.Name)
	if policy.Name == containertypes.RestartPolicyOnFailure && policy.MaximumRetryCount > 0 {
		backup += "." + strconv.Itoa(policy.MaximumRetryCount)
	}
	return backup
}

// parseBackupName returns the original name and, when recorded, the restart
// policy HarborBuddy suppressed before stopping the original container.
func parseBackupName(backup string) (string, *containertypes.RestartPolicy, bool) {
	match := backupNamePattern.FindStringSubmatch(backup)
	if match == nil {
		return "", nil, false
	}
	if match[3] == "" {
		return match[1], nil, true
	}
	policy := &containertypes.RestartPolicy{Name: containertypes.RestartPolicyMode(match[3])}
	if match[4] != "" {
		retries, err := strconv.Atoi(match[4])
		if err != nil {
			return "", nil, false
		}
		policy.MaximumRetryCount = retries
	}
	return match[1], policy, true
}

// backupNameTime returns when HarborBuddy created a backup name.
func backupNameTime(backup string) (time.Time, bool) {
	match := backupNamePattern.FindStringSubmatch(backup)
	if match == nil {
		return time.Time{}, false
	}
	nanos, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, nanos), true
}
