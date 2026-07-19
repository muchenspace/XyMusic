package admincatalog

import "testing"

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
