package adminmetadata

import (
	"xymusic/server/internal/platform/mediafile"
)

type WritebackError = mediafile.WritebackError

var (
	NewWritebackError        = mediafile.NewWritebackError
	wrapWritebackError       = mediafile.WrapWritebackError
	writebackErrorCode       = mediafile.WritebackErrorCode
	safeWritebackError       = mediafile.SafeWritebackError
	filesystemWritebackError = mediafile.FilesystemWritebackError
	truncateASCII            = mediafile.TruncateASCII
)
