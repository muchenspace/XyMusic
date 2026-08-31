package adminmanagement

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type userCursorValue struct {
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
	Total     *int   `json:"total,omitempty"`
}

type sessionCursorValue struct {
	LastSeenAt string `json:"lastSeenAt"`
	ID         string `json:"id"`
	Total      *int   `json:"total,omitempty"`
}

func userCursorScope(input ListUsersInput) string {
	return scopedManagementCursor("users", strings.Join([]string{
		strings.TrimSpace(input.Query), string(input.Role), string(input.Status),
	}, "\x00"))
}

func sessionCursorScope(userID string) string {
	return scopedManagementCursor("sessions", userID)
}

func scopedManagementCursor(kind, value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("admin:%s:%s", kind, hex.EncodeToString(digest[:]))
}

func userCursorTotalHint(cursor *UserCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func sessionCursorTotalHint(cursor *SessionCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func decodeUserCursor(codec *pagination.CursorCodec, scope, encoded string) (*UserCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[userCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidManagementCursor()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, value.CreatedAt)
	if err != nil {
		return nil, invalidManagementCursor()
	}
	return &UserCursor{CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), ID: value.ID, Total: value.Total}, nil
}

func encodeUserCursor(codec *pagination.CursorCodec, scope string, record UserRecord, total int) (string, error) {
	if record.ID == "" {
		return "", invalidManagementCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, userCursorValue{
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339Nano), ID: record.ID, Total: &totalValue,
	})
}

func decodeSessionCursor(codec *pagination.CursorCodec, scope, encoded string) (*SessionCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[sessionCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidManagementCursor()
	}
	lastSeenAt, err := time.Parse(time.RFC3339Nano, value.LastSeenAt)
	if err != nil {
		return nil, invalidManagementCursor()
	}
	return &SessionCursor{LastSeenAt: lastSeenAt.UTC().Format(time.RFC3339Nano), ID: value.ID, Total: value.Total}, nil
}

func encodeSessionCursor(codec *pagination.CursorCodec, scope string, record SessionRecord, total int) (string, error) {
	if record.ID == "" {
		return "", invalidManagementCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, sessionCursorValue{
		LastSeenAt: record.LastSeenAt.UTC().Format(time.RFC3339Nano), ID: record.ID, Total: &totalValue,
	})
}

func invalidManagementCursor() error {
	return apperror.InvalidCursor("分页游标无效")
}
