package adminsettings

import (
	"reflect"
	"testing"
	"time"

	"xymusic/server/internal/modules/setup"
	"xymusic/server/internal/platform/runtimemetrics"
	"xymusic/server/internal/platform/workerstatus"
)

func TestSystemInformationDTOIncludesInjectedRuntimeMetrics(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 5, 0, time.UTC)
	wantMetrics := runtimemetrics.Snapshot{
		CollectedSince: "2026-07-16T12:00:00.000Z",
		Requests: runtimemetrics.RequestSnapshot{
			Total: 7, InFlight: 1, Errors: 2, ErrorRate: 2.0 / 7.0, Sampled: 7,
		},
	}
	service := &Service{
		metrics:            runtimeMetricsStub{snapshot: wantMetrics},
		applicationVersion: "1.2.3",
		configurationPath:  "runtime/.env",
		startedAt:          now.Add(-5 * time.Second),
		now:                func() time.Time { return now },
	}
	result := service.systemInformationDTO(
		setup.RuntimeSnapshot{Source: setup.RuntimeSourceManaged},
		"16.4", "202607160001", nil,
		workerstatus.Snapshot{Available: true},
		QueueDTO{Media: 2, Total: 2},
	)
	if result.ApplicationVersion != "1.2.3" || result.UptimeSeconds != 5 ||
		result.ConfigurationSource != setup.RuntimeSourceManaged || !reflect.DeepEqual(result.Metrics, wantMetrics) {
		t.Fatalf("system information = %#v", result)
	}
}

type runtimeMetricsStub struct {
	snapshot runtimemetrics.Snapshot
}

func (stub runtimeMetricsStub) Snapshot() runtimemetrics.Snapshot { return stub.snapshot }
