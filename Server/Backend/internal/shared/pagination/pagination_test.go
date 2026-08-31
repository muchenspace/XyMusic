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
	if page, err := ParseOffset(1, MaxPageSize, 25); err != nil || page.PageSize != MaxPageSize {
		t.Fatalf("unexpected maximum page size: %#v %v", page, err)
	}
	if _, err := ParseOffset(1, MaxPageSize+1, 25); !apperror.IsCode(err, apperror.CodeValidationError) {
		t.Fatalf("expected page size bound error, got %v", err)
	}
	if page, err := ParseOffset(4_001, 25, 25); err != nil || page.Offset != 100_000 {
		t.Fatalf("expected deep offsets to remain available, got %#v/%v", page, err)
	}
	if pages := BoundedTotalPages(200_000, 100); pages != 2_000 {
		t.Fatalf("unexpected exact page count: %d", pages)
	}
}

func TestPaginationHasNoTotalRowBoundary(t *testing.T) {
	page, err := ParseCursor(1_001, 100, 25)
	if err != nil || page.Page != 1_001 || page.PageSize != 100 || CursorLimit(page.Page, page.PageSize) != 101 {
		t.Fatalf("unexpected deep cursor page: %#v/%v", page, err)
	}
	if page, err := ParseOffset(1, 100_000, 25); err != nil || page.PageSize != 100_000 {
		t.Fatalf("expected 100000 rows per page to be accepted, got %#v/%v", page, err)
	}
	if pages := BoundedTotalPages(20_000_000, 100_000); pages != 200 {
		t.Fatalf("unexpected page count for large result: %d", pages)
	}
	if got := CursorLimit(301, 333); got != 334 {
		t.Fatalf("unexpected cursor look-ahead limit: %d", got)
	}
}
