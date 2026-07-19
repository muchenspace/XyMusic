package retention

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRetentionCutoffsMatchLegacyDurations(t *testing.T) {
	now := time.Date(2026, time.March, 31, 23, 45, 0, 123_000_000, time.FixedZone("test", 9*60*60))
	cutoffs := RetentionCutoffs(now)
	assertTime(t, "refresh tokens", cutoffs.RefreshTokens, now.Add(-30*24*time.Hour))
	assertTime(t, "revoked sessions", cutoffs.RevokedSessions, now.Add(-90*24*time.Hour))
	assertTime(t, "uploads", cutoffs.Uploads, now.Add(-30*24*time.Hour))
	assertTime(t, "operational jobs", cutoffs.OperationalJobs, now.Add(-90*24*time.Hour))
	assertTime(t, "audit", cutoffs.Audit, now.Add(-365*24*time.Hour))
}

func TestRunIfDueAppliesEveryPolicyInBatchesAndLogsCounts(t *testing.T) {
	now := time.Date(2026, time.July, 16, 10, 30, 0, 0, time.UTC)
	executor := newScriptedExecutor()
	executor.responses[idempotencyStatement] = []executeResult{{rows: 500}, {rows: 500}, {rows: 3}}
	for range MaxBatchesPerPolicy {
		executor.responses[rateLimitsStatement] = append(
			executor.responses[rateLimitsStatement], executeResult{rows: BatchSize},
		)
	}
	executor.responses[refreshTokensStatement] = []executeResult{{rows: 4}}
	executor.responses[sessionsRevokedStatement] = []executeResult{{rows: 5}}
	executor.responses[sessionsDeletedStatement] = []executeResult{{rows: 6}}
	executor.responses[uploadsExpiredStatement] = []executeResult{{rows: 7}}
	executor.responses[uploadsDeletedStatement] = []executeResult{{rows: 8}}
	executor.responses[mediaJobsStatement] = []executeResult{{rows: 9}}
	executor.responses[libraryScansStatement] = []executeResult{{rows: 10}}
	executor.responses[writebacksStatement] = []executeResult{{rows: 11}}
	executor.responses[objectCleanupJobsStatement] = []executeResult{{rows: 12}}
	executor.responses[trackDeleteBatchesStatement] = []executeResult{{rows: 13}}
	executor.responses[auditStatement] = []executeResult{{rows: 14}}
	database := &fakeDatabase{executor: executor}
	logger := &recordingLogger{}
	clock := &mutableClock{now: now}
	worker, err := NewWorker(Dependencies{Database: database, Logger: logger, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}

	result, err := worker.RunIfDue(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	expected := Counts{
		Idempotency: 1003, RateLimits: BatchSize * MaxBatchesPerPolicy,
		RefreshTokens: 4, SessionsRevoked: 5, SessionsDeleted: 6,
		UploadsExpired: 7, UploadsDeleted: 8, MediaJobs: 9,
		LibraryScans: 10, Writebacks: 11, ObjectCleanupJobs: 12,
		TrackDeleteBatches: 13, Audit: 14,
	}
	if !result.Ran || result.Counts != expected {
		t.Fatalf("result=%+v", result)
	}
	if database.calls != 1 || database.names[0] != AdvisoryLockName {
		t.Fatalf("advisory lock calls/names=%d/%v", database.calls, database.names)
	}
	if got := len(executor.callsFor(rateLimitsStatement)); got != MaxBatchesPerPolicy {
		t.Fatalf("rate-limit batch calls=%d", got)
	}
	if got := len(executor.callsFor(idempotencyStatement)); got != 3 {
		t.Fatalf("idempotency batch calls=%d", got)
	}
	if len(logger.entries) != 1 || logger.entries[0].message != "retention.completed" {
		t.Fatalf("log entries=%+v", logger.entries)
	}
	if !reflect.DeepEqual(logger.entries[0].fields, expected.Fields()) {
		t.Fatalf("log fields=%v", logger.entries[0].fields)
	}
	assertTime(t, "next run", worker.NextRunAt(), now.Add(RunInterval))
	assertPolicyArguments(t, executor, now)

	// A call before the deadline is a no-op and does not acquire the lock.
	clock.now = now.Add(RunInterval - time.Nanosecond)
	skipped, err := worker.RunIfDue(context.Background(), false)
	if err != nil || skipped.Ran || database.calls != 1 || len(logger.entries) != 1 {
		t.Fatalf("skipped=%+v err=%v calls=%d logs=%d", skipped, err, database.calls, len(logger.entries))
	}
}

func TestRunIfDueRunsAtDeadlineAndForceReschedules(t *testing.T) {
	start := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: start}
	database := &fakeDatabase{executor: newScriptedExecutor()}
	logger := &recordingLogger{}
	worker, err := NewWorker(Dependencies{Database: database, Logger: logger, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := worker.RunIfDue(context.Background(), false); err != nil || !result.Ran {
		t.Fatalf("initial result=%+v err=%v", result, err)
	}
	clock.now = start.Add(RunInterval)
	if result, err := worker.RunIfDue(context.Background(), false); err != nil || !result.Ran {
		t.Fatalf("deadline result=%+v err=%v", result, err)
	}
	clock.now = start.Add(RunInterval + time.Minute)
	if result, err := worker.RunIfDue(context.Background(), true); err != nil || !result.Ran {
		t.Fatalf("forced result=%+v err=%v", result, err)
	}
	if database.calls != 3 || len(logger.entries) != 3 {
		t.Fatalf("calls/logs=%d/%d", database.calls, len(logger.entries))
	}
	assertTime(t, "forced next run", worker.NextRunAt(), clock.now.Add(RunInterval))
}

func TestRunIfDueReturnsPartialCountsAndUsesFailureTimeForRetry(t *testing.T) {
	start := time.Date(2026, time.July, 16, 14, 0, 0, 0, time.UTC)
	failureObservedAt := start.Add(2 * time.Minute)
	retryAt := failureObservedAt.Add(RetryInterval)
	clock := &sequenceClock{values: []time.Time{
		start,
		failureObservedAt,
		retryAt.Add(-time.Nanosecond),
		retryAt,
	}}
	expectedError := errors.New("database unavailable")
	executor := newScriptedExecutor()
	executor.responses[idempotencyStatement] = []executeResult{{rows: 2}, {rows: 0}}
	executor.responses[rateLimitsStatement] = []executeResult{{err: expectedError}, {rows: 0}}
	database := &fakeDatabase{executor: executor}
	logger := &recordingLogger{}
	worker, err := NewWorker(Dependencies{Database: database, Logger: logger, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}

	failed, err := worker.RunIfDue(context.Background(), false)
	if !errors.Is(err, expectedError) {
		t.Fatalf("error=%v", err)
	}
	if !failed.Ran || failed.Counts.Idempotency != 2 {
		t.Fatalf("failed result=%+v", failed)
	}
	if len(logger.entries) != 0 {
		t.Fatalf("failure logs=%+v", logger.entries)
	}
	assertTime(t, "retry deadline", worker.NextRunAt(), retryAt)

	skipped, err := worker.RunIfDue(context.Background(), false)
	if err != nil || skipped.Ran || database.calls != 1 {
		t.Fatalf("pre-retry result=%+v err=%v calls=%d", skipped, err, database.calls)
	}
	retried, err := worker.RunIfDue(context.Background(), false)
	if err != nil || !retried.Ran || database.calls != 2 || len(logger.entries) != 1 {
		t.Fatalf("retry result=%+v err=%v calls=%d logs=%d", retried, err, database.calls, len(logger.entries))
	}
}

func TestConcurrentNonForcedRunIsSkippedAfterScheduleAdvances(t *testing.T) {
	start := time.Date(2026, time.July, 16, 15, 0, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	database := &blockingDatabase{entered: entered, release: release, executor: newScriptedExecutor()}
	worker, err := NewWorker(Dependencies{Database: database, Clock: &mutableClock{now: start}})
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() {
		_, runErr := worker.RunIfDue(context.Background(), false)
		completed <- runErr
	}()
	<-entered
	second, err := worker.RunIfDue(context.Background(), false)
	if err != nil || second.Ran {
		t.Fatalf("concurrent result=%+v err=%v", second, err)
	}
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if database.calls != 1 {
		t.Fatalf("database calls=%d", database.calls)
	}
}

func TestNewWorkerValidatesDatabaseAndDefaultsOptionalPorts(t *testing.T) {
	if _, err := NewWorker(Dependencies{}); err == nil {
		t.Fatal("expected missing database error")
	}
	if _, err := NewPostgresDatabase(nil); err == nil {
		t.Fatal("expected missing PostgreSQL pool error")
	}
	worker, err := NewWorker(Dependencies{Database: &fakeDatabase{executor: newScriptedExecutor()}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.RunIfDue(context.Background(), true)
	if err != nil || !result.Ran {
		t.Fatalf("default ports result=%+v err=%v", result, err)
	}
}

func assertPolicyArguments(t *testing.T, executor *scriptedExecutor, now time.Time) {
	t.Helper()
	cutoffs := RetentionCutoffs(now)
	checks := []struct {
		statement string
		arguments []any
	}{
		{idempotencyStatement, []any{now}},
		{rateLimitsStatement, []any{now}},
		{refreshTokensStatement, []any{cutoffs.RefreshTokens}},
		{sessionsRevokedStatement, []any{now.Add(-time.Hour), now}},
		{sessionsDeletedStatement, []any{cutoffs.RevokedSessions}},
		{uploadsExpiredStatement, []any{now, now.Add(-10 * time.Minute)}},
		{uploadsDeletedStatement, []any{cutoffs.Uploads}},
		{mediaJobsStatement, []any{cutoffs.OperationalJobs}},
		{libraryScansStatement, []any{cutoffs.OperationalJobs}},
		{writebacksStatement, []any{cutoffs.OperationalJobs}},
		{objectCleanupJobsStatement, []any{cutoffs.OperationalJobs}},
		{trackDeleteBatchesStatement, []any{cutoffs.OperationalJobs}},
		{auditStatement, []any{cutoffs.Audit}},
	}
	for _, check := range checks {
		calls := executor.callsFor(check.statement)
		if len(calls) == 0 {
			t.Fatalf("policy was not executed: %s", firstSQLLine(check.statement))
		}
		if !reflect.DeepEqual(calls[0].arguments, check.arguments) {
			t.Fatalf("arguments for %s=%v, want %v", firstSQLLine(check.statement), calls[0].arguments, check.arguments)
		}
		if !strings.Contains(check.statement, "LIMIT 500") {
			t.Fatalf("policy has no batch limit: %s", firstSQLLine(check.statement))
		}
	}
}

func firstSQLLine(statement string) string {
	lines := strings.Split(strings.TrimSpace(statement), "\n")
	return lines[0]
}

func assertTime(t *testing.T, name string, actual, expected time.Time) {
	t.Helper()
	if !actual.Equal(expected) {
		t.Fatalf("%s=%s, want %s", name, actual, expected)
	}
}

type mutableClock struct {
	now time.Time
}

func (clock *mutableClock) Now() time.Time { return clock.now }

type sequenceClock struct {
	mu     sync.Mutex
	values []time.Time
	index  int
}

func (clock *sequenceClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if len(clock.values) == 0 {
		return time.Time{}
	}
	index := clock.index
	if index >= len(clock.values) {
		index = len(clock.values) - 1
	} else {
		clock.index++
	}
	return clock.values[index]
}

type logEntry struct {
	message string
	fields  map[string]any
}

type recordingLogger struct {
	entries []logEntry
}

func (logger *recordingLogger) Info(message string, fields map[string]any) {
	copyFields := make(map[string]any, len(fields))
	for key, value := range fields {
		copyFields[key] = value
	}
	logger.entries = append(logger.entries, logEntry{message: message, fields: copyFields})
}

type fakeDatabase struct {
	executor Executor
	calls    int
	names    []string
}

func (database *fakeDatabase) WithAdvisoryLock(
	_ context.Context,
	name string,
	operation func(Executor) error,
) error {
	database.calls++
	database.names = append(database.names, name)
	return operation(database.executor)
}

type blockingDatabase struct {
	entered  chan<- struct{}
	release  <-chan struct{}
	executor Executor
	calls    int
}

func (database *blockingDatabase) WithAdvisoryLock(
	_ context.Context,
	_ string,
	operation func(Executor) error,
) error {
	database.calls++
	close(database.entered)
	<-database.release
	return operation(database.executor)
}

type executeResult struct {
	rows int64
	err  error
}

type executeCall struct {
	statement string
	arguments []any
}

type scriptedExecutor struct {
	responses map[string][]executeResult
	indexes   map[string]int
	calls     []executeCall
}

func newScriptedExecutor() *scriptedExecutor {
	return &scriptedExecutor{
		responses: make(map[string][]executeResult),
		indexes:   make(map[string]int),
	}
}

func (executor *scriptedExecutor) Execute(
	_ context.Context,
	statement string,
	arguments ...any,
) (int64, error) {
	executor.calls = append(executor.calls, executeCall{
		statement: statement, arguments: append([]any(nil), arguments...),
	})
	index := executor.indexes[statement]
	executor.indexes[statement] = index + 1
	responses := executor.responses[statement]
	if index >= len(responses) {
		return 0, nil
	}
	return responses[index].rows, responses[index].err
}

func (executor *scriptedExecutor) callsFor(statement string) []executeCall {
	result := make([]executeCall, 0)
	for _, call := range executor.calls {
		if call.statement == statement {
			result = append(result, call)
		}
	}
	return result
}
