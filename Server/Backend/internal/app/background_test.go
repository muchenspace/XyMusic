package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundGroupStopsPollingBeforeCloseReturns(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	group := startBackgroundGroup(logger, backgroundTask{
		name: "blocking-test",
		run: func(ctx context.Context) (bool, error) {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-ctx.Done()
			return false, ctx.Err()
		},
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background task did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := group.Close(ctx); err != nil {
		t.Fatal(err)
	}
	observed := calls.Load()
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != observed {
		t.Fatalf("background task continued after close: %d -> %d", observed, calls.Load())
	}
}

func TestBackgroundPollerRecordsReturnedErrors(t *testing.T) {
	records := make(chan capturedLogRecord, 1)
	logger := slog.New(capturingLogHandler{records: records})
	expected := errors.New("claim failed")
	group := startBackgroundGroup(logger, backgroundTask{
		name: "metadata-writeback",
		run: func(context.Context) (bool, error) {
			return false, expected
		},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := group.Close(ctx); err != nil {
			t.Errorf("close background group: %v", err)
		}
	})

	select {
	case record := <-records:
		if record.level != slog.LevelError || record.message != "background.poller.failed" {
			t.Fatalf("record level/message = %s/%q", record.level, record.message)
		}
		if record.fields["task"] != "metadata-writeback" ||
			fmt.Sprint(record.fields["error"]) != expected.Error() ||
			record.fields["retryIn"] != time.Second {
			t.Fatalf("record fields = %#v", record.fields)
		}
	case <-time.After(time.Second):
		t.Fatal("background poller error was not logged")
	}
}

type capturedLogRecord struct {
	level   slog.Level
	message string
	fields  map[string]any
}

type capturingLogHandler struct {
	records chan<- capturedLogRecord
}

func (capturingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (handler capturingLogHandler) Handle(_ context.Context, record slog.Record) error {
	fields := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attribute slog.Attr) bool {
		fields[attribute.Key] = attribute.Value.Resolve().Any()
		return true
	})
	handler.records <- capturedLogRecord{level: record.Level, message: record.Message, fields: fields}
	return nil
}

func (handler capturingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler capturingLogHandler) WithGroup(string) slog.Handler      { return handler }
