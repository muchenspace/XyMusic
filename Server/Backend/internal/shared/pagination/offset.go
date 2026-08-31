package pagination

import (
	"math"

	"xymusic/server/internal/shared/apperror"
)

const (
	// MaxPage is a transport-level guard for the page number itself. It is not a
	// cap on the number of records that can be browsed; cursor pagination can
	// continue through every matching record.
	MaxPage     = 1_000_000_000
	MaxPageSize = 100_000

	// These names are kept for source compatibility with older callers. They no
	// longer impose a total-row boundary; MaxPageSize is the only pagination
	// size limit.
	MaxPaginationRows = math.MaxInt
	MaxOffsetRows     = MaxPaginationRows
)

type Offset struct {
	Page     int
	PageSize int
	Offset   int
}

func ParseOffset(page, pageSize, defaultPageSize int) (Offset, error) {
	parsed, err := ParsePage(page, pageSize, defaultPageSize)
	if err != nil {
		return Offset{}, err
	}
	if parsed.Page-1 > math.MaxInt/parsed.PageSize {
		return Offset{}, apperror.Validation("分页参数无效")
	}
	return Offset{Page: parsed.Page, PageSize: parsed.PageSize, Offset: (parsed.Page - 1) * parsed.PageSize}, nil
}

// ParseCursor validates a page number used together with a keyset cursor.
// Unlike ParseOffset it does not calculate or send a database OFFSET. There is
// deliberately no logical row-count boundary here: pageSize is the only
// pagination-size limit.
func ParseCursor(page, pageSize, defaultPageSize int) (Offset, error) {
	return ParsePage(page, pageSize, defaultPageSize)
}

// ParsePage validates logical page dimensions without calculating an OFFSET.
// It is useful for callers that need to validate page dimensions before
// applying a module-specific cursor policy.
func ParsePage(page, pageSize, defaultPageSize int) (Offset, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if page < 1 || page > MaxPage || pageSize < 1 || pageSize > MaxPageSize {
		return Offset{}, apperror.Validation("分页参数无效")
	}
	return Offset{Page: page, PageSize: pageSize}, nil
}

// MaxPages returns the transport-level maximum page number. It is retained for
// callers that need to validate a page number, but it no longer derives from a
// total-row boundary.
func MaxPages(pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return MaxPage
}

// CursorLimit returns the number of rows to request for a cursor page,
// including one look-ahead row so the caller can determine whether another
// page exists. The page number is intentionally not used to impose a total
// record limit.
func CursorLimit(page, pageSize int) int {
	if page < 1 || pageSize < 1 {
		return 0
	}
	return pageSize + 1
}

func BoundedTotalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	// Keep the old function name for source compatibility. It now returns the
	// exact number of pages and does not cap the total at any row boundary.
	return (total-1)/pageSize + 1
}
