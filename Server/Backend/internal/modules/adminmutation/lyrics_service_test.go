package adminmutation

import (
	"context"
	"errors"
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestUpsertLyricsRejectsMissingOrInconsistentTimingBeforeStore(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		timing  string
		content string
	}{
		{name: "missing timing", format: "LRC", content: "[00:01.00]line"},
		{name: "plain word timing", format: "PLAIN", timing: "WORD", content: "plain lyrics"},
		{name: "incomplete word timing", format: "LRC", timing: "WORD", content: "[00:01.00]<00:01.00>word\n[00:02.00]line"},
		{name: "word content declared as line", format: "LRC", timing: "LINE", content: "[00:01.00]<00:01.00>word"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &lyricsValidationStoreStub{}
			service := newBatchServiceForTest(t, store)
			_, err := service.UpsertLyrics(context.Background(), "admin", "trace", "track", LyricsInput{
				ExpectedVersion: 1,
				Language:        "und",
				Format:          test.format,
				Timing:          test.timing,
				Content:         OptionalString{Set: true, Value: test.content},
				IsDefault:       OptionalBool{Set: true, Value: true},
			})
			if !apperror.IsCode(err, apperror.CodeValidationError) || store.calls != 0 {
				t.Fatalf("UpsertLyrics() error/calls = %v/%d", err, store.calls)
			}
		})
	}
}

func TestUpsertLyricsRejectsNonCanonicalTimingBeforeStore(t *testing.T) {
	for _, timing := range []string{"word", " WORD "} {
		t.Run(timing, func(t *testing.T) {
			store := &lyricsValidationStoreStub{}
			service := newBatchServiceForTest(t, store)
			_, err := service.UpsertLyrics(context.Background(), "admin", "trace", "track", LyricsInput{
				ExpectedVersion: 1,
				Language:        "und",
				Format:          "LRC",
				Timing:          timing,
				Content:         OptionalString{Set: true, Value: "[00:01.00]<00:01.00>word"},
				IsDefault:       OptionalBool{Set: true, Value: true},
			})
			if !apperror.IsCode(err, apperror.CodeValidationError) || store.calls != 0 {
				t.Fatalf("UpsertLyrics() timing %q error/calls = %v/%d", timing, err, store.calls)
			}
		})
	}
}

type lyricsValidationStoreStub struct {
	Store
	calls int
}

func (store *lyricsValidationStoreStub) UpsertLyrics(context.Context, string, LyricsInput) (StoredLyric, error) {
	store.calls++
	return StoredLyric{}, errors.New("unexpected lyric store call")
}
