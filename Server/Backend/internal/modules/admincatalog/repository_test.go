package admincatalog

import (
	"strings"
	"testing"
)

func TestWritebackHasTerminalError(t *testing.T) {
	errorCode := "WRITE_FAILED"
	message := "Tag writeback failed"
	for _, test := range []struct {
		name      string
		status    string
		errorCode *string
		message   *string
		want      bool
	}{
		{name: "failed without details", status: "FAILED", want: true},
		{name: "cancelled with code", status: "CANCELLED", errorCode: &errorCode, want: true},
		{name: "cancelled with message", status: "CANCELLED", message: &message, want: true},
		{name: "ordinary cancellation", status: "CANCELLED", want: false},
		{name: "ready with historical error", status: "READY", errorCode: &errorCode, message: &message, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := writebackHasTerminalError(test.status, test.errorCode, test.message); got != test.want {
				t.Fatalf("terminal error=%v want=%v", got, test.want)
			}
		})
	}
}

func TestCatalogSeekConditionHandlesNullableReleaseDates(t *testing.T) {
	date := "2026-01-02"
	for _, test := range []struct {
		name      string
		order     SortOrder
		cursor    ListCursor
		wantParts []string
	}{
		{name: "release ascending before nulls", order: SortAscending, cursor: ListCursor{Value: date, ID: "album-1"}, wantParts: []string{"al.release_date > $2::date", "al.release_date IS NULL", "al.id > $1"}},
		{name: "release descending non-null", order: SortDescending, cursor: ListCursor{Value: date, ID: "album-1"}, wantParts: []string{"al.release_date < $2::date", "al.id < $1"}},
		{name: "null release group", order: SortAscending, cursor: ListCursor{Null: true, ID: "album-1"}, wantParts: []string{"al.release_date IS NULL", "al.id > $1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			arguments := make([]any, 0, 2)
			condition, err := catalogSeekCondition("al.release_date", "al.id", "releaseDate", test.order, &test.cursor, true, &arguments)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range test.wantParts {
				if !strings.Contains(condition, part) {
					t.Fatalf("condition %q does not contain %q", condition, part)
				}
			}
		})
	}
}
