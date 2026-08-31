package adminmetadata

import (
	"testing"
	"time"

	"xymusic/server/internal/shared/pagination"
)

func TestWritebackCursorRoundTripAndScopeBinding(t *testing.T) {
	codec := pagination.NewCursorCodec("01234567890123456789012345678901")
	created := time.Date(2026, 8, 30, 1, 2, 3, 456000000, time.UTC)
	job := WritebackJob{ID: "job-1", CreatedAt: created}
	scope := writebackCursorScope(WritebackListInput{Status: WritebackPending, TrackID: "track-1"})
	cursor, err := encodeWritebackCursor(codec, scope, job, 10)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWritebackCursor(codec, scope, cursor)
	if err != nil || decoded == nil || decoded.ID != job.ID || decoded.CreatedAt != created.Format(time.RFC3339Nano) || decoded.Total == nil || *decoded.Total != 10 {
		t.Fatalf("decoded cursor = %#v/%v", decoded, err)
	}
	if _, err := decodeWritebackCursor(codec, scope+":other", cursor); err == nil {
		t.Fatal("cursor should be bound to its filter scope")
	}
}
