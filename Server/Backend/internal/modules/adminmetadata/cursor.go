package adminmetadata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type writebackCursorValue struct {
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
	Total     *int   `json:"total,omitempty"`
}

func writebackCursorTotalHint(cursor *WritebackCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func writebackCursorScope(input WritebackListInput) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		string(input.Status), input.TrackID,
	}, "\x00")))
	return fmt.Sprintf("admin:writebacks:%s", hex.EncodeToString(digest[:]))
}

func decodeWritebackCursor(codec *pagination.CursorCodec, scope, encoded string) (*WritebackCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[writebackCursorValue](codec, scope, encoded)
	if err != nil {
		return nil, err
	}
	if value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidWritebackCursor()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, value.CreatedAt)
	if err != nil {
		return nil, invalidWritebackCursor()
	}
	return &WritebackCursor{CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), ID: value.ID, Total: value.Total}, nil
}

func encodeWritebackCursor(codec *pagination.CursorCodec, scope string, job WritebackJob, total int) (string, error) {
	if job.ID == "" {
		return "", invalidWritebackCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, writebackCursorValue{
		CreatedAt: job.CreatedAt.UTC().Format(time.RFC3339Nano), ID: job.ID, Total: &totalValue,
	})
}

func invalidWritebackCursor() error {
	return apperror.InvalidCursor("\u5206\u9875\u6e38\u6807\u65e0\u6548")
}
