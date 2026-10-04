package adminmetadata

import (
	"errors"

	"xymusic/server/internal/shared/tagwriteback"
)

// WritebackEligibilityError is the module domain error for a source that is
// not eligible for Tag writeback.
type WritebackEligibilityError struct {
	Reason  tagwriteback.BlockReason
	TrackID string
}

func (failure *WritebackEligibilityError) Error() string {
	return tagwriteback.Decision{BlockReason: failure.Reason}.Message()
}

// EvaluateWritebackEligibility evaluates the shared Tag writeback rules and
// returns a module domain error instead of a transport error.
func EvaluateWritebackEligibility(source tagwriteback.SourceContext, trackID string) error {
	decision := tagwriteback.Evaluate(source)
	if decision.CanWriteBack {
		return nil
	}
	return &WritebackEligibilityError{Reason: decision.BlockReason, TrackID: trackID}
}

// TranslateMetadataError converts a module domain error into the public
// application error. Messages, codes, and metadata are identical to
// tagwriteback.Decision.Error.
func TranslateMetadataError(err error) error {
	if err == nil {
		return nil
	}
	var eligibility *WritebackEligibilityError
	if !errors.As(err, &eligibility) {
		return err
	}
	decision := tagwriteback.Decision{BlockReason: eligibility.Reason}
	return decision.Error(eligibility.TrackID)
}
