package pagination

import (
	"fmt"

	"xymusic/server/internal/shared/apperror"
)

const (
	MaxOffsetRows = 10_000
	MaxPage       = MaxOffsetRows + 1
)

type Offset struct {
	Page     int
	PageSize int
	Offset   int
}

func ParseOffset(page, pageSize, defaultPageSize int) (Offset, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if page < 1 || page > MaxPage || pageSize < 1 || pageSize > 100 {
		return Offset{}, apperror.Validation("分页参数无效")
	}
	offset := (page - 1) * pageSize
	if offset > MaxOffsetRows {
		return Offset{}, apperror.Validation(fmt.Sprintf("分页不能跳过超过 %d 行，请缩小筛选或搜索范围", MaxOffsetRows))
	}
	return Offset{Page: page, PageSize: pageSize, Offset: offset}, nil
}

func BoundedTotalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	actual := (total + pageSize - 1) / pageSize
	accessible := MaxOffsetRows/pageSize + 1
	if actual < accessible {
		return actual
	}
	return accessible
}
