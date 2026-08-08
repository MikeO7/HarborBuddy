package updater

import "testing"

func TestResultMessageCoversEveryStatus(t *testing.T) {
	tests := []struct {
		status Status
		dryRun bool
		want   string
	}{
		{status: StatusExcluded, want: "Container excluded: reason"},
		{status: StatusCurrent, want: "Container image is current"},
		{status: StatusWouldUpdate, want: "Container update is available"},
		{status: StatusWouldUpdate, dryRun: true, want: "Container would be updated"},
		{status: StatusUpdated, want: "Container updated"},
		{status: StatusSelfUpdateStarted, want: "Self-update helper started; HarborBuddy will shut down gracefully"},
		{status: StatusUnsupported, want: "Container cannot be updated safely: reason"},
		{status: StatusFailed, want: "Container update failed"},
		{status: StatusCancelled, want: "Container check cancelled"},
		{status: StatusChangedExternally, want: "Container changed during the cycle; skipping"},
		{status: Status("invalid"), want: "Container update result is invalid"},
	}
	for _, test := range tests {
		result := ContainerResult{Status: test.status, Reason: "reason"}
		if got := resultMessage(result, test.dryRun); got != test.want {
			t.Errorf("resultMessage(%q, %v) = %q, want %q", test.status, test.dryRun, got, test.want)
		}
	}
}
