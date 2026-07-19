package adminmetadata

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPresentMetadataIncludesWritebackBlockReason(t *testing.T) {
	raw, err := json.Marshal(MetadataSnapshot{
		Title: "Track", Credits: []MetadataCredit{}, AlbumArtists: []string{}, Genres: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	overrides := json.RawMessage(`{}`)
	now := time.Now().UTC()
	mode, rootStatus, trackStatus := "READ_ONLY", "READY", "READY"
	enabled := true
	dto, err := presentMetadata(MetadataRecord{
		TrackID: "track", Raw: raw, Overrides: overrides, Version: 1, CreatedAt: now, UpdatedAt: now,
		Source: &MetadataSourceRecord{
			ID: "source", SourcePath: "album/song.flac", Status: "READY",
			ChecksumSHA256: "checksum", RootMode: &mode, RootEnabled: &enabled,
			RootStatus: &rootStatus, TrackStatus: &trackStatus, MappingCount: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Source == nil || dto.Source.CanWriteBack || dto.Source.WritebackBlockReason == nil ||
		*dto.Source.WritebackBlockReason != "The music source is read-only" {
		t.Fatalf("source eligibility = %#v", dto.Source)
	}
}
