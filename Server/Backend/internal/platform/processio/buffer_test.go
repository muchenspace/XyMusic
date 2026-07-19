package processio

import (
	"strings"
	"testing"
)

func TestHeadBufferDrainsAfterLimit(t *testing.T) {
	buffer := NewHeadBuffer(5)
	if written, err := buffer.Write([]byte("abc")); err != nil || written != 3 {
		t.Fatalf("first write = %d, %v", written, err)
	}
	if written, err := buffer.Write([]byte("defgh")); err != nil || written != 5 {
		t.Fatalf("second write = %d, %v", written, err)
	}
	if buffer.String() != "abcde" || !buffer.Truncated() {
		t.Fatalf("buffer = %q, truncated = %v", buffer.String(), buffer.Truncated())
	}
}

func TestTailBufferRetainsLatestBytes(t *testing.T) {
	buffer := NewTailBuffer(5)
	for _, value := range []string{"ab", "cde", "fgh"} {
		if written, err := buffer.Write([]byte(value)); err != nil || written != len(value) {
			t.Fatalf("write %q = %d, %v", value, written, err)
		}
	}
	if got := buffer.String(); got != "defgh" {
		t.Fatalf("tail = %q, want %q", got, "defgh")
	}

	large := strings.Repeat("x", 10) + "tail"
	if written, err := buffer.Write([]byte(large)); err != nil || written != len(large) {
		t.Fatalf("large write = %d, %v", written, err)
	}
	if got := buffer.String(); got != "xtail" {
		t.Fatalf("large tail = %q, want %q", got, "xtail")
	}
}
