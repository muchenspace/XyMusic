package app

import (
	"log/slog"
	"testing"
	"time"
)

func TestSlogWorkerLoggerPreservesLevelsAndStructuredFields(t *testing.T) {
	records := make(chan capturedLogRecord, 3)
	logger := newSlogWorkerLogger(slog.New(capturingLogHandler{records: records}))

	logger.Info("media.job.completed", map[string]any{"jobId": "job-1"})
	logger.Warn("metadata.writeback.failed", map[string]any{"attempt": 2})
	logger.Error("metadata.writeback.rollback_failed", map[string]any{"path": "track.flac"})

	expected := []struct {
		level   slog.Level
		message string
		key     string
		value   any
	}{
		{slog.LevelInfo, "media.job.completed", "jobId", "job-1"},
		{slog.LevelWarn, "metadata.writeback.failed", "attempt", int64(2)},
		{slog.LevelError, "metadata.writeback.rollback_failed", "path", "track.flac"},
	}
	for _, want := range expected {
		select {
		case record := <-records:
			if record.level != want.level || record.message != want.message || record.fields[want.key] != want.value {
				t.Fatalf("record = %#v, want level=%s message=%q %s=%v", record, want.level, want.message, want.key, want.value)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %q", want.message)
		}
	}
}
