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

func TestExpressionUsesLocalSourcesAndAssets(t *testing.T) {
	expression := Expression("track")
	for _, expected := range []string{
		"track.status = 'ARCHIVED'",
		"active_scan.status IN ('PENDING', 'RUNNING')",
		"scan_source.last_seen_at < COALESCE(active_scan.started_at, active_scan.created_at)",
		"track.published_at IS NOT NULL",
		"ready_source.status = 'READY'",
		"ready_asset.status = 'READY'",
	} {
		if !strings.Contains(expression, expected) {
			t.Fatalf("audio status expression does not contain %q\n%s", expected, expression)
		}
	}
	for _, forbidden := range []string{
		"track_variants",
		"media_jobs",
		"processing_mapping",
		"processing_source.status IN ('PENDING', 'PROCESSING')",
	} {
		if strings.Contains(expression, forbidden) {
			t.Fatalf("audio status must not contain forbidden legacy tables %q\n%s", forbidden, expression)
		}
	}
}
