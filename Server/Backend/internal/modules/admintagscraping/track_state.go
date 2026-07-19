package admintagscraping

import (
	"strings"

	"xymusic/server/internal/shared/apperror"
)

const (
	archivedTrackStatus       = "ARCHIVED"
	archivedTrackErrorReason  = "TRACK_ARCHIVED"
	archivedTrackErrorMessage = "Archived tracks cannot be scraped or modified"
	archivedBatchItemMessage  = "曲目已归档，任务已取消，不再处理"
)

func trackIsArchived(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), archivedTrackStatus)
}

func archivedTrackError(trackID string) error {
	metadata := map[string]any{"reason": archivedTrackErrorReason}
	if trackID != "" {
		metadata["trackId"] = trackID
	}
	return apperror.Conflict(
		apperror.CodeInvalidStateTransition,
		archivedTrackErrorMessage,
		metadata,
	)
}

func isArchivedTrackError(err error) bool {
	applicationError, ok := apperror.As(err)
	if !ok || applicationError.Code != apperror.CodeInvalidStateTransition {
		return false
	}
	reason, _ := applicationError.Metadata["reason"].(string)
	return reason == archivedTrackErrorReason
}
