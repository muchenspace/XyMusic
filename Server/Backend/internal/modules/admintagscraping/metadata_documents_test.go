package admintagscraping

import (
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestDecodeMetadataDocumentsRejectsStoredLyricsWithMismatchedTiming(t *testing.T) {
	_, _, err := decodeMetadataDocuments(
		[]byte(`{"lyrics":{"content":"[00:01.00]line","format":"LRC","language":"und","timing":"WORD"}}`),
		[]byte(`{}`),
	)
	if !apperror.IsCode(err, apperror.CodeInternalError) {
		t.Fatalf("decodeMetadataDocuments() error = %v, want internal error", err)
	}
}

func TestApplyOverridesRejectsStoredLyricsWithMismatchedTiming(t *testing.T) {
	raw := MetadataSnapshot{
		Lyrics: &MetadataLyrics{
			Content: "[00:01.00]line", Format: "LRC", Language: "und", Timing: "LINE",
		},
	}
	overrides := map[string]any{
		"lyrics": map[string]any{
			"content": "[00:01.00]line", "format": "LRC", "language": "und", "timing": "WORD",
		},
	}
	_, err := applyOverrides(raw, overrides)
	if !apperror.IsCode(err, apperror.CodeInternalError) {
		t.Fatalf("applyOverrides() error = %v, want internal error", err)
	}
}
