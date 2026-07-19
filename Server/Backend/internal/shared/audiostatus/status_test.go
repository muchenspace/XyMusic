package audiostatus

import (
	"strings"
	"testing"
)

func TestValidAcceptsOnlyPublicAudioStates(t *testing.T) {
	for _, status := range []Status{Processing, Ready, Error, Archived} {
		if !Valid(status) {
			t.Fatalf("status %q should be valid", status)
		}
	}
	for _, status := range []Status{"", "PENDING", "FAILED", "CANCELLED"} {
		if Valid(status) {
			t.Fatalf("status %q should be invalid", status)
		}
	}
}

func TestExpressionUsesCurrentGenerationScanEpochAndPlayableVariant(t *testing.T) {
	expression := Expression("track")
	for _, expected := range []string{
		"track.status = 'ARCHIVED'",
		"active_scan.status IN ('PENDING', 'RUNNING')",
		"scan_source.last_seen_at < COALESCE(active_scan.started_at, active_scan.created_at)",
		"active_job.generation = track.media_generation",
		"failed_job.generation = track.media_generation",
		"track.published_at IS NOT NULL",
		"ready_variant.status = 'READY'",
		"ready_asset.status = 'READY'",
	} {
		if !strings.Contains(expression, expected) {
			t.Fatalf("audio status expression does not contain %q\n%s", expected, expression)
		}
	}
	if strings.Contains(expression, "ORDER BY") {
		t.Fatalf("audio status must not select a historical latest job\n%s", expression)
	}
	precedence := []string{
		"track.status = 'ARCHIVED'",
		"active_scan.status IN ('PENDING', 'RUNNING')",
		"active_job.status IN ('PENDING', 'PROCESSING')",
		"track.status = 'ERROR'",
		"failed_job.status IN ('FAILED', 'CANCELLED')",
		"track.status = 'READY'",
		"track.published_at IS NOT NULL THEN 'ERROR'",
		"ELSE 'PROCESSING'",
	}
	previous := -1
	for _, fragment := range precedence {
		position := strings.Index(expression, fragment)
		if position <= previous {
			t.Fatalf("audio status precedence is invalid at %q\n%s", fragment, expression)
		}
		previous = position
	}
}
