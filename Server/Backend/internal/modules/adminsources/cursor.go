package adminsources

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"xymusic/server/internal/shared/apperror"
	"xymusic/server/internal/shared/pagination"
)

type sourceRootCursorValue struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	Total *int   `json:"total,omitempty"`
}

type sourceFileCursorValue struct {
	Path  string `json:"path"`
	ID    string `json:"id"`
	Total *int   `json:"total,omitempty"`
}

type sourceRunCursorValue struct {
	CreatedAt string `json:"createdAt"`
	ID        string `json:"id"`
	Total     *int   `json:"total,omitempty"`
}

type directoryCursorValue struct {
	Name  string `json:"name"`
	Total *int   `json:"total,omitempty"`
}

func sourceCursorScope(kind string, values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return fmt.Sprintf("admin:sources:%s:%s", kind, hex.EncodeToString(digest[:]))
}

func rootCursorScope() string { return sourceCursorScope("roots") }

func fileCursorScope(rootID string, query FileQuery) string {
	return sourceCursorScope("files", rootID, strings.TrimSpace(query.Query), string(query.Status))
}

func runCursorScope(rootID string) string { return sourceCursorScope("runs", rootID) }

func directoryCursorScope(path string) string { return sourceCursorScope("directories", path) }

func rootCursorTotalHint(cursor *RootCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func fileCursorTotalHint(cursor *SourceFileCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func runCursorTotalHint(cursor *ScanCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func directoryCursorTotalHint(cursor *DirectoryCursor) *int {
	if cursor == nil {
		return nil
	}
	return cursor.Total
}

func decodeRootCursor(codec *pagination.CursorCodec, scope, encoded string) (*RootCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[sourceRootCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidSourceCursor()
	}
	return &RootCursor{Name: value.Name, ID: value.ID, Total: value.Total}, nil
}

func encodeRootCursor(codec *pagination.CursorCodec, scope string, root Root, total int) (string, error) {
	if root.ID == "" {
		return "", invalidSourceCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, sourceRootCursorValue{Name: root.Name, ID: root.ID, Total: &totalValue})
}

func decodeFileCursor(codec *pagination.CursorCodec, scope, encoded string) (*SourceFileCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[sourceFileCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidSourceCursor()
	}
	return &SourceFileCursor{Path: value.Path, ID: value.ID, Total: value.Total}, nil
}

func encodeFileCursor(codec *pagination.CursorCodec, scope string, file SourceFile, total int) (string, error) {
	if file.ID == "" {
		return "", invalidSourceCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, sourceFileCursorValue{Path: file.Path, ID: file.ID, Total: &totalValue})
}

func decodeRunCursor(codec *pagination.CursorCodec, scope, encoded string) (*ScanCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[sourceRunCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.ID == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidSourceCursor()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, value.CreatedAt)
	if err != nil {
		return nil, invalidSourceCursor()
	}
	return &ScanCursor{CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), ID: value.ID, Total: value.Total}, nil
}

func encodeRunCursor(codec *pagination.CursorCodec, scope string, run ScanRun, total int) (string, error) {
	if run.ID == "" {
		return "", invalidSourceCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, sourceRunCursorValue{CreatedAt: run.CreatedAt.UTC().Format(time.RFC3339Nano), ID: run.ID, Total: &totalValue})
}

func decodeDirectoryCursor(codec *pagination.CursorCodec, scope, encoded string) (*DirectoryCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	value, err := pagination.DecodeCursor[directoryCursorValue](codec, scope, encoded)
	if err != nil || value == nil || value.Name == "" || value.Total != nil && *value.Total < 0 {
		return nil, invalidSourceCursor()
	}
	return &DirectoryCursor{Name: value.Name, Total: value.Total}, nil
}

func encodeDirectoryCursor(codec *pagination.CursorCodec, scope string, directory DirectoryDTO, total int) (string, error) {
	if directory.Name == "" {
		return "", invalidSourceCursor()
	}
	totalValue := total
	return pagination.EncodeCursor(codec, scope, directoryCursorValue{Name: directory.Name, Total: &totalValue})
}

func invalidSourceCursor() error {
	return apperror.InvalidCursor("分页游标无效")
}
