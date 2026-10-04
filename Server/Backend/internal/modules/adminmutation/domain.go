package adminmutation

import (
	"errors"

	"xymusic/server/internal/shared/apperror"
)

// Domain errors returned by the administrator mutation repository. Services
// translate them into the public application errors without changing any
// message, code, or metadata.
var (
	ErrTrackAlreadyArchived     = errors.New("Track is already archived")
	ErrTrackNotArchived         = errors.New("Only archived tracks can be restored")
	ErrTrackDurationNotPositive = errors.New("Track duration must be positive")
	ErrTrackHasNoReadySource    = errors.New("Track has no ready audio source")
	ErrTrackMustBeArchived      = errors.New("Track must be in the recycle bin before permanent deletion")
	ErrTrackArchivedEdit        = errors.New("Archived tracks cannot be edited")
	ErrTrackArchivedLyrics      = errors.New("Archived tracks cannot edit lyrics")
	ErrTrackArchivedPublish     = errors.New("Archived tracks must be restored before publishing")
	ErrTrackVersionStale        = errors.New("Track version is stale")
)

// TrackRuleError associates a track state rule violation with a track. The
// track ID is optional: single-track operations may not expose it in public
// metadata.
type TrackRuleError struct {
	Rule    error
	TrackID string
}

func (e *TrackRuleError) Error() string { return e.Rule.Error() }
func (e *TrackRuleError) Unwrap() error { return e.Rule }

// TrackVersionError reports a stale expected track version.
type TrackVersionError struct {
	Expected int
	Current  int
	TrackID  string
}

func (e *TrackVersionError) Error() string { return ErrTrackVersionStale.Error() }
func (e *TrackVersionError) Unwrap() error { return ErrTrackVersionStale }

// CheckTrackVersion validates an expected track version against the locked
// version. A non-empty trackID is attached to public metadata.
func CheckTrackVersion(expected, current int, trackID string) error {
	if expected != current {
		return &TrackVersionError{Expected: expected, Current: current, TrackID: trackID}
	}
	return nil
}

// CanArchiveTrack validates that the locked track is not already archived.
func CanArchiveTrack(status, trackID string) error {
	if status == "ARCHIVED" {
		return &TrackRuleError{Rule: ErrTrackAlreadyArchived, TrackID: trackID}
	}
	return nil
}

// CanRestoreTrack validates that the locked track is archived.
func CanRestoreTrack(status, trackID string) error {
	if status != "ARCHIVED" {
		return &TrackRuleError{Rule: ErrTrackNotArchived, TrackID: trackID}
	}
	return nil
}

// CanPublishTrack validates that the locked track is not archived.
func CanPublishTrack(status, trackID string) error {
	if status == "ARCHIVED" {
		return &TrackRuleError{Rule: ErrTrackArchivedPublish, TrackID: trackID}
	}
	return nil
}

// CanEditTrack validates that the locked track is not archived.
func CanEditTrack(status, trackID string) error {
	if status == "ARCHIVED" {
		return &TrackRuleError{Rule: ErrTrackArchivedEdit, TrackID: trackID}
	}
	return nil
}

// CanEditTrackLyrics validates that the locked track is not archived.
func CanEditTrackLyrics(status, trackID string) error {
	if status == "ARCHIVED" {
		return &TrackRuleError{Rule: ErrTrackArchivedLyrics, TrackID: trackID}
	}
	return nil
}

// CheckTrackDuration validates that the track reports a positive duration.
func CheckTrackDuration(durationMS int64, trackID string) error {
	if durationMS <= 0 {
		return &TrackRuleError{Rule: ErrTrackDurationNotPositive, TrackID: trackID}
	}
	return nil
}

// RequireReadyAudioSource validates that a ready local or uploaded source
// exists.
func RequireReadyAudioSource(ready bool, trackID string) error {
	if !ready {
		return &TrackRuleError{Rule: ErrTrackHasNoReadySource, TrackID: trackID}
	}
	return nil
}

// CanDeleteTrackPermanently validates that the locked track is archived.
func CanDeleteTrackPermanently(status, trackID string) error {
	if status != "ARCHIVED" {
		return &TrackRuleError{Rule: ErrTrackMustBeArchived, TrackID: trackID}
	}
	return nil
}

// DeletionWritebackConflict returns the job that must be cancelled before a
// permanent deletion can proceed. It returns the empty string when no active
// worker holds a writeback for the track.
func DeletionWritebackConflict(jobs []writebackDeletionRecord) string {
	for _, job := range jobs {
		if job.status == "PROCESSING" || (job.status == "PENDING" && job.workerHeld) {
			return job.id
		}
	}
	return ""
}

// TranslateMutationError converts a module domain error into the public
// application error. Messages, codes, and metadata are identical to the
// previously inlined errors.
func TranslateMutationError(err error) error {
	if err == nil {
		return nil
	}
	var versionError *TrackVersionError
	if errors.As(err, &versionError) {
		var extra map[string]any
		if versionError.TrackID != "" {
			extra = map[string]any{"trackId": versionError.TrackID}
		}
		return versionConflict("Track", versionError.Expected, versionError.Current, extra)
	}
	var ruleError *TrackRuleError
	if !errors.As(err, &ruleError) {
		return err
	}
	var metadata map[string]any
	if ruleError.TrackID != "" {
		metadata = map[string]any{"trackId": ruleError.TrackID}
	}
	switch {
	case errors.Is(err, ErrTrackAlreadyArchived):
		return apperror.New(apperror.CodeInvalidStateTransition, "Track is already archived", apperror.WithMetadata(metadata))
	case errors.Is(err, ErrTrackNotArchived):
		return apperror.New(apperror.CodeInvalidStateTransition, "Only archived tracks can be restored", apperror.WithMetadata(metadata))
	case errors.Is(err, ErrTrackDurationNotPositive):
		return apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Track duration must be positive", metadata)
	case errors.Is(err, ErrTrackHasNoReadySource):
		return apperror.Unprocessable(apperror.CodeTrackNotPlayable, "Track has no ready audio source", metadata)
	case errors.Is(err, ErrTrackMustBeArchived):
		return apperror.Conflict(apperror.CodeInvalidStateTransition, "Track must be in the recycle bin before permanent deletion", metadata)
	case errors.Is(err, ErrTrackArchivedPublish):
		return apperror.New(apperror.CodeInvalidStateTransition, "Archived tracks must be restored before publishing")
	case errors.Is(err, ErrTrackArchivedLyrics):
		return apperror.New(apperror.CodeInvalidStateTransition, "Archived tracks cannot edit lyrics")
	case errors.Is(err, ErrTrackArchivedEdit):
		return apperror.New(apperror.CodeInvalidStateTransition, "Archived tracks cannot be edited")
	default:
		return err
	}
}
