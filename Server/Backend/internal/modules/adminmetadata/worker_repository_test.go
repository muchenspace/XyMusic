package adminmetadata

import (
	"errors"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestResolveWritebackFailurePrioritizesRequestedCancellation(t *testing.T) {
	decision := resolveWritebackFailure(WritebackJob{
		CancelRequested: true,
		Attempts:        2,
		MaxAttempts:     3,
	}, errors.New("ffmpeg exited unexpectedly"))

	if !decision.Cancelled || decision.Code != "WRITEBACK_CANCELLED" {
		t.Fatalf("decision=%+v", decision)
	}
	if decision.Message != "Metadata writeback was cancelled" {
		t.Fatalf("message=%q", decision.Message)
	}
	if decision.RetryDelay != 10*time.Second {
		t.Fatalf("retry delay=%s", decision.RetryDelay)
	}
}

func TestResolveWritebackFailureKeepsRollbackFailureVisible(t *testing.T) {
	decision := resolveWritebackFailure(WritebackJob{
		CancelRequested: true,
		Attempts:        1,
		MaxAttempts:     3,
	}, NewWritebackError("ROLLBACK_FAILED", "original source could not be restored"))

	if decision.Cancelled || decision.Code != "ROLLBACK_FAILED" {
		t.Fatalf("decision=%+v", decision)
	}
	if !terminalWritebackCodes[decision.Code] {
		t.Fatal("a rollback failure must be terminal")
	}
}

func TestResolveWritebackFailureCapsRetryAndStoredMessage(t *testing.T) {
	decision := resolveWritebackFailure(WritebackJob{
		Attempts:    10,
		MaxAttempts: 10,
	}, errors.New(strings.Repeat("x", 4_100)))

	if decision.Cancelled || decision.Code == "WRITEBACK_CANCELLED" {
		t.Fatalf("decision=%+v", decision)
	}
	if len(decision.Message) != 4_000 {
		t.Fatalf("message length=%d", len(decision.Message))
	}
	if decision.RetryDelay != 5*time.Minute {
		t.Fatalf("retry delay=%s", decision.RetryDelay)
	}
}

func TestRepositoryRejectsNonPositiveWritebackLeases(t *testing.T) {
	repository := &Repository{}
	if _, err := repository.ClaimWriteback(t.Context(), "worker", 0); err == nil {
		t.Fatal("expected ClaimWriteback to reject a zero lease")
	}
	if err := repository.RenewWritebackLease(t.Context(), "job", "worker", "attempt", -time.Second); err == nil {
		t.Fatal("expected RenewWritebackLease to reject a negative lease")
	}
}

func TestSourcePathChangeIsTerminal(t *testing.T) {
	if !terminalWritebackCodes["SOURCE_PATH_CHANGED"] {
		t.Fatal("SOURCE_PATH_CHANGED must not be retried against a different path")
	}
}

func TestSlowTransientRollbackRetryClassification(t *testing.T) {
	for _, code := range []string{"ROLLBACK_FAILED", "SOURCE_CHANGED", "SOURCE_PATH_CHANGED", "UNSAFE_SOURCE_PATH"} {
		if !slowTransientRollbackRetry(code) {
			t.Fatalf("%s must use slow retry", code)
		}
	}
	if slowTransientRollbackRetry("FILESYSTEM_EBUSY") {
		t.Fatal("ordinary filesystem errors must keep the short retry")
	}
}

func TestCommittedWritebackCannotBeCancelled(t *testing.T) {
	err := validateWritebackCancellation(WritebackJob{
		Status: WritebackProcessing,
		Stage:  StageCommitted,
	})
	if !apperror.IsCode(err, apperror.CodeInvalidStateTransition) {
		t.Fatalf("error=%v", err)
	}
}
