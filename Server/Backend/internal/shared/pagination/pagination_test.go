package pagination

import (
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestCursorIsSignedAndScopeBound(t *testing.T) {
	codec := NewCursorCodec("01234567890123456789012345678901")
	type value struct {
		ID string `json:"id"`
	}
	cursor, err := EncodeCursor(codec, "tracks:published", value{ID: "track-1"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCursor[value](codec, "tracks:published", cursor)
	if err != nil || decoded == nil || decoded.ID != "track-1" {
		t.Fatalf("unexpected decoded cursor: %#v %v", decoded, err)
	}
	if _, err := DecodeCursor[value](codec, "albums", cursor); !apperror.IsCode(err, apperror.CodeInvalidCursor) {
		t.Fatalf("expected scope mismatch, got %v", err)
	}
	if _, err := DecodeCursor[value](codec, "tracks:published", cursor+"x"); !apperror.IsCode(err, apperror.CodeInvalidCursor) {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestOffsetPaginationBounds(t *testing.T) {
	page, err := ParseOffset(0, 0, 25)
	if err != nil || page.Page != 1 || page.PageSize != 25 || page.Offset != 0 {
		t.Fatalf("unexpected defaults: %#v %v", page, err)
	}
	if _, err := ParseOffset(102, 100, 25); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("expected offset bound error, got %v", err)
	}
	if pages := BoundedTotalPages(100_000, 25); pages != 401 {
		t.Fatalf("unexpected bounded pages: %d", pages)
	}
}
