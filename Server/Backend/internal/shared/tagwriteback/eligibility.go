// Package tagwriteback owns the shared eligibility rules for writing metadata
// back to managed local music sources.
package tagwriteback

import (
	"path/filepath"
	"strings"

	"xymusic/server/internal/shared/apperror"
)

type BlockReason string

const (
	BlockNone              BlockReason = ""
	BlockNoSource          BlockReason = "NO_SOURCE"
	BlockTrackArchived     BlockReason = "TRACK_ARCHIVED"
	BlockCueOrSharedSource BlockReason = "CUE_OR_SHARED_SOURCE"
	BlockReadOnly          BlockReason = "READ_ONLY"
	BlockRootDisabled      BlockReason = "ROOT_DISABLED"
	BlockRootNotReady      BlockReason = "ROOT_NOT_READY"
	BlockSourceNotReady    BlockReason = "SOURCE_NOT_READY"
	BlockUnsupportedFormat BlockReason = "UNSUPPORTED_FORMAT"
)

type SourceContext struct {
	HasSource    bool
	TrackStatus  string
	RootMode     string
	RootEnabled  bool
	RootStatus   string
	SourceStatus string
	SourcePath   string
	MappingCount int
	Cue          bool
}

type Decision struct {
	CanWriteBack bool
	BlockReason  BlockReason
}

func Evaluate(source SourceContext) Decision {
	switch {
	case !source.HasSource:
		return blocked(BlockNoSource)
	case source.TrackStatus == "ARCHIVED":
		return blocked(BlockTrackArchived)
	case source.MappingCount > 1 || source.Cue:
		return blocked(BlockCueOrSharedSource)
	case source.RootMode != "READ_WRITE":
		return blocked(BlockReadOnly)
	case !source.RootEnabled:
		return blocked(BlockRootDisabled)
	case source.RootStatus != "READY":
		return blocked(BlockRootNotReady)
	case source.SourceStatus != "READY":
		return blocked(BlockSourceNotReady)
	case source.SourcePath != "" && !Supports(source.SourcePath):
		return blocked(BlockUnsupportedFormat)
	default:
		return Decision{CanWriteBack: true}
	}
}

func Supports(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac", ".mp3", ".m4a", ".mp4", ".ogg", ".opus":
		return true
	default:
		return false
	}
}

func (decision Decision) Message() string {
	switch decision.BlockReason {
	case BlockNoSource:
		return "A writable local source for this track was not found"
	case BlockTrackArchived:
		return "Archived tracks cannot start Tag writeback"
	case BlockCueOrSharedSource:
		return "Tag writeback is unavailable for CUE or shared physical sources"
	case BlockReadOnly:
		return "The music source is read-only"
	case BlockRootDisabled:
		return "The music source is disabled"
	case BlockRootNotReady:
		return "The music source must be ready before writing tags"
	case BlockSourceNotReady:
		return "The source file must be ready before writing tags"
	case BlockUnsupportedFormat:
		return "This source container does not support safe Tag writeback"
	default:
		return ""
	}
}

func (decision Decision) MessagePointer() *string {
	if decision.CanWriteBack {
		return nil
	}
	message := decision.Message()
	return &message
}

func (decision Decision) Error(trackID string) error {
	if decision.CanWriteBack {
		return nil
	}
	metadata := map[string]any{}
	if trackID != "" {
		metadata["trackId"] = trackID
	}
	switch decision.BlockReason {
	case BlockNoSource:
		return apperror.New(apperror.CodeResourceNotFound, decision.Message(), apperror.WithMetadata(metadata))
	case BlockReadOnly:
		return apperror.New(apperror.CodeForbidden, decision.Message(), apperror.WithMetadata(metadata))
	case BlockCueOrSharedSource, BlockUnsupportedFormat:
		return apperror.New(apperror.CodeValidationError, decision.Message(), apperror.WithMetadata(metadata))
	default:
		return apperror.New(apperror.CodeInvalidStateTransition, decision.Message(), apperror.WithMetadata(metadata))
	}
}

func blocked(reason BlockReason) Decision {
	return Decision{BlockReason: reason}
}
