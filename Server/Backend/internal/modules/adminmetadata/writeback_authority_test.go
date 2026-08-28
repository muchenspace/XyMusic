package adminmetadata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/shared/apperror"
)

func TestReadWriteSourceIsWritableWhenNoScanIsActive(t *testing.T) {
	raw, err := json.Marshal(MetadataSnapshot{
		Title: "Track", Credits: []MetadataCredit{}, AlbumArtists: []string{}, Genres: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	mode, trackStatus := "READ_WRITE", "READY"
	enabled := true
	record := MetadataRecord{
		TrackID: "track", Raw: raw, Overrides: json.RawMessage(`{}`), Version: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Source: &MetadataSourceRecord{
			ID: "source", SourcePath: "album/song.flac", Status: "READY",
			ChecksumSHA256: "checksum", RootMode: &mode, RootEnabled: &enabled,
			TrackStatus: &trackStatus, MappingCount: 1,
		},
	}
	dto, err := presentMetadata(record)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Source == nil || !dto.Source.CanWriteBack || dto.Source.WritebackBlockReason != nil {
		t.Fatalf("read-write source eligibility = %#v", dto.Source)
	}
	if err := assertWritableSource(mode, enabled, false, "READY"); err != nil {
		t.Fatalf("worker eligibility rejected read-write source: %v", err)
	}
}

func TestActiveScanIsTheOnlyScanStateThatBlocksReadWriteSource(t *testing.T) {
	if err := assertWritableSource("READ_WRITE", true, true, "READY"); !apperror.IsCode(err, apperror.CodeInvalidStateTransition) || !strings.Contains(err.Error(), "currently being scanned") {
		t.Fatalf("active scan error = %v", err)
	}
	if err := assertWritableSource("READ_WRITE", true, false, "READY"); err != nil {
		t.Fatalf("terminal scan state still blocked writeback: %v", err)
	}
}
