package adminsources

import (
	"testing"
	"time"
)

func TestPresentRootUsesLatestScanAsStatusAuthority(t *testing.T) {
	completedAt := time.Date(2026, 8, 26, 1, 2, 3, 0, time.UTC)
	staleError := "stale root error"
	view := RootView{
		Root: Root{
			ID: "root", Name: "Music", Path: "music", Mode: RootModeReadWrite,
			Enabled: true, Status: RootStatusScanning, LastError: &staleError, Version: 4,
			CreatedAt: completedAt.Add(-time.Hour), UpdatedAt: completedAt.Add(-time.Minute),
		},
		LatestRun: &ScanRun{
			ID: "scan", RootID: "root", RootVersion: 4, Status: ScanStatusCompleted,
			CompletedAt: &completedAt, CreatedAt: completedAt.Add(-time.Minute), UpdatedAt: completedAt,
		},
	}
	dto := presentRoot(view)
	if dto.Status != RootStatusReady || dto.LastError != nil || dto.LastScanAt == nil || *dto.LastScanAt != formatTimestamp(completedAt) {
		t.Fatalf("completed root presentation = %#v", dto)
	}

	view.LatestRun.RootVersion = 3
	dto = presentRoot(view)
	if dto.Status != RootStatusUnknown || dto.LastError != nil {
		t.Fatalf("stale completed root presentation = %#v", dto)
	}
}

func TestPresentRootNeverLeavesTerminalScanAsScanning(t *testing.T) {
	now := time.Date(2026, 8, 26, 2, 3, 4, 0, time.UTC)
	lastScanAt := now.Add(-time.Hour)
	for _, status := range []ScanStatus{ScanStatusPending, ScanStatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			dto := presentRoot(RootView{
				Root: Root{
					ID: "root", Name: "Music", Path: "music", Mode: RootModeReadWrite,
					Enabled: true, Status: RootStatusScanning, LastScanAt: &lastScanAt, Version: 2,
					CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
				},
				LatestRun: &ScanRun{
					ID: "scan", RootID: "root", RootVersion: 2, Status: status,
					CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
				},
			})
			if dto.Status != RootStatusReady {
				t.Fatalf("%s root status = %s", status, dto.Status)
			}
		})
	}
}

func TestPresentRootKeepsActualActiveScanAboveTerminalLatestRun(t *testing.T) {
	now := time.Date(2026, 8, 26, 3, 4, 5, 0, time.UTC)
	lastScanAt := now.Add(-time.Hour)
	dto := presentRoot(RootView{
		Root: Root{
			ID: "root", Name: "Music", Path: "music", Mode: RootModeReadWrite,
			Enabled: true, Status: RootStatusScanning, LastScanAt: &lastScanAt, Version: 3,
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
		},
		LatestRun: &ScanRun{
			ID: "newer-terminal-scan", RootID: "root", RootVersion: 3, Status: ScanStatusCompleted,
			CompletedAt: &now, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
		},
		ScanActive: true,
	})
	if dto.Status != RootStatusScanning || dto.LastError != nil {
		t.Fatalf("active scan root presentation = %#v", dto)
	}
}

func TestPresentRootTreatsQueuedScanAsActive(t *testing.T) {
	now := time.Date(2026, 8, 26, 4, 5, 6, 0, time.UTC)
	dto := presentRoot(RootView{
		Root: Root{
			ID: "root", Name: "Music", Path: "music", Mode: RootModeReadWrite,
			Enabled: true, Status: RootStatusReady, Version: 2,
			CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		},
		LatestRun: &ScanRun{
			ID: "queued-scan", RootID: "root", RootVersion: 2, Status: ScanStatusPending,
			CreatedAt: now, UpdatedAt: now,
		},
		ScanActive: true,
	})
	if dto.Status != RootStatusScanning {
		t.Fatalf("queued scan root status = %s", dto.Status)
	}
}
