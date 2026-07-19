package tagwriteback

import (
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestEvaluateWritebackEligibility(t *testing.T) {
	writable := SourceContext{
		HasSource: true, TrackStatus: "READY", RootMode: "READ_WRITE", RootEnabled: true,
		RootStatus: "READY", SourceStatus: "READY", SourcePath: "album/song.flac", MappingCount: 1,
	}
	tests := []struct {
		name   string
		mutate func(*SourceContext)
		reason BlockReason
		code   apperror.Code
	}{
		{name: "writable"},
		{name: "missing source", mutate: func(value *SourceContext) { value.HasSource = false }, reason: BlockNoSource, code: apperror.CodeResourceNotFound},
		{name: "archived track", mutate: func(value *SourceContext) { value.TrackStatus = "ARCHIVED" }, reason: BlockTrackArchived, code: apperror.CodeInvalidStateTransition},
		{name: "cue source", mutate: func(value *SourceContext) { value.Cue = true }, reason: BlockCueOrSharedSource, code: apperror.CodeValidationError},
		{name: "shared source", mutate: func(value *SourceContext) { value.MappingCount = 2 }, reason: BlockCueOrSharedSource, code: apperror.CodeValidationError},
		{name: "read only", mutate: func(value *SourceContext) { value.RootMode = "READ_ONLY" }, reason: BlockReadOnly, code: apperror.CodeForbidden},
		{name: "disabled root", mutate: func(value *SourceContext) { value.RootEnabled = false }, reason: BlockRootDisabled, code: apperror.CodeInvalidStateTransition},
		{name: "root not ready", mutate: func(value *SourceContext) { value.RootStatus = "ERROR" }, reason: BlockRootNotReady, code: apperror.CodeInvalidStateTransition},
		{name: "source not ready", mutate: func(value *SourceContext) { value.SourceStatus = "PROCESSING" }, reason: BlockSourceNotReady, code: apperror.CodeInvalidStateTransition},
		{name: "unsupported format", mutate: func(value *SourceContext) { value.SourcePath = "album/song.wav" }, reason: BlockUnsupportedFormat, code: apperror.CodeValidationError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := writable
			if test.mutate != nil {
				test.mutate(&input)
			}
			decision := Evaluate(input)
			if test.reason == BlockNone {
				if !decision.CanWriteBack || decision.MessagePointer() != nil || decision.Error("track") != nil {
					t.Fatalf("writable decision = %#v", decision)
				}
				return
			}
			if decision.CanWriteBack || decision.BlockReason != test.reason || decision.MessagePointer() == nil {
				t.Fatalf("blocked decision = %#v", decision)
			}
			if err := decision.Error("track"); !apperror.IsCode(err, test.code) {
				t.Fatalf("error = %v, want code %s", err, test.code)
			}
		})
	}
}
