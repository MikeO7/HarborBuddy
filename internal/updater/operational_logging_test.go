package updater

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/MikeO7/HarborBuddy/internal/docker"
	"github.com/rs/zerolog"
)

func TestOperationalResultLoggingCoversSeverityAndTransactionFields(t *testing.T) {
	var output bytes.Buffer
	logger := zerolog.New(&output).Level(zerolog.DebugLevel)
	base := docker.ContainerSummary{
		ID: "container-1234567890", Name: "app", ImageRef: strings.Repeat("image", 70), ImageID: "sha256:old-image-123456",
	}
	results := []ContainerResult{
		{Container: base, Status: StatusCurrent},
		{Container: base, Status: StatusExcluded, Reason: "policy"},
		{Container: base, Status: StatusWouldUpdate, TargetImageID: "sha256:new-image-123456"},
		{Container: base, Status: StatusUpdated, TargetImageID: "sha256:new-image-123456", Warning: errors.New("backup retained")},
		{Container: base, Status: StatusSelfUpdateStarted, TargetImageID: "sha256:new-image-123456", HelperID: "helper-1234567890"},
		{Container: base, Status: StatusUnsupported, Reason: "unsafe"},
		{Container: base, Status: StatusCancelled},
		{Container: base, Status: StatusChangedExternally},
		{
			Container: base, Status: StatusFailed, TargetImageID: "sha256:new-image-123456", FailureStage: "start_replacement",
			RollbackTried: true, RollbackErr: errors.New("rollback failed"), Err: errors.New("update failed"),
		},
	}
	logReport(logger, Report{Results: results}, true)
	text := output.String()
	for _, want := range []string{
		`"level":"debug"`, `"level":"warn"`, `"level":"error"`, `"transaction_id":`,
		`"helper_container_id":`, `"failure_stage":"start_replacement"`, `"rollback_outcome":"failed"`,
		`"image_ref_truncated":true`, `"event":"update_complete"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("operational update log missing %q: %s", want, text)
		}
	}
}

func TestUpdateSummaryOutcomesAndEmptyTransaction(t *testing.T) {
	logger := zerolog.Nop()
	warningReport := Report{Results: []ContainerResult{{Status: StatusUpdated, Warning: errors.New("warning")}}}
	logReport(logger, warningReport, false)
	logReport(logger, Report{Results: []ContainerResult{{Status: StatusCancelled}}}, false)
	logReport(logger, Report{Results: []ContainerResult{{Status: StatusSelfUpdateStarted}}}, false)
	if updateOutcome(Report{Results: []ContainerResult{{Status: StatusCancelled}}}) != "cancelled" {
		t.Fatal("cancelled report outcome was not preserved")
	}
	if updateOutcome(Report{Results: []ContainerResult{{Status: StatusSelfUpdateStarted}}}) != "self_update_handoff" {
		t.Fatal("self-update report outcome was not preserved")
	}
	if transactionID(ContainerResult{}) != "" {
		t.Fatal("empty target produced a transaction ID")
	}
	if warningReport.WarningCount() != 1 || (Report{}).WarningCount() != 0 {
		t.Fatal("warning summary count is incorrect")
	}
}
