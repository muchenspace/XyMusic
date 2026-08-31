package adminjobs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type jobCursorValue struct {
	Value string `json:"value"`
	ID    string `json:"id"`
	Total *int   `json:"total,omitempty"`
}

func jobCursorScope(input ListInput, sort SortField, order SortOrder) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(input.Search), string(input.Status), string(input.Type), string(sort), string(order),
	}, "\x00")))
	return fmt.Sprintf("admin:jobs:%s", hex.EncodeToString(digest[:]))
}

func jobCursorTotalHint(cursor *JobCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func decodeJobCursor(codec *pagination.CursorCodec, scope, encoded string, sort SortField) (*JobCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[jobCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidJobCursor()
	}
	if sort == SortCreatedAt || sort == SortUpdatedAt {
		parsed, parseErr := time.Parse(time.RFC3339Nano, value.Value)
		if parseErr != nil {
			return nil, invalidJobCursor()
		}
		value.Value = parsed.UTC().Format(time.RFC3339Nano)
	}
	return &JobCursor{Value: value.Value, ID: value.ID, Total: value.Total}, nil
}

func encodeJobCursor(codec *pagination.CursorCodec, scope string, sort SortField, record JobRecord, total int) (string, error) {
	if record.ID == "" {
		return "", invalidJobCursor()
	}
	value := jobCursorValue{ID: record.ID}
	totalValue := total
	value.Total = &totalValue
	switch sort {
	case SortCreatedAt:
		value.Value = record.CreatedAt.UTC().Format(time.RFC3339Nano)
	case SortUpdatedAt:
		value.Value = record.UpdatedAt.UTC().Format(time.RFC3339Nano)
	case SortStatus:
		value.Value = string(record.Status)
	case SortType:
		value.Value = string(record.Type)
	case SortTitle:
		value.Value = record.Title
	default:
		return "", invalidJobCursor()
	}
	return pagination.EncodeCursor(codec, scope, value)
}

func invalidJobCursor() error {
	return apperror.InvalidCursor("分页游标无效")
}
