package lyrics

import "testing"

func TestValidTimingAcceptsOnlyLineAndWord(t *testing.T) {
	for _, value := range []string{"LINE", "WORD"} {
		if !ValidTiming(value) {
			t.Fatalf("ValidTiming(%q) = false", value)
		}
	}
	for _, value := range []string{"", "PLAIN", "LRC", "QRC"} {
		if ValidTiming(value) {
			t.Fatalf("ValidTiming(%q) = true", value)
		}
	}
}

func TestDetectTimingUsesExplicitWordMarkersOnlyForLRC(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		content string
		want    Timing
	}{
		{
			name:    "enhanced lrc",
			format:  "LRC",
			content: "[ar:Artist]\n[00:01.00]<00:01.00>first<00:01.50> line\n[00:03.00]<00:03.00>second",
			want:    TimingWord,
		},
		{name: "ordinary lrc", format: "LRC", content: "[00:01.00]line", want: TimingLine},
		{
			name:    "mixed enhanced and ordinary lrc",
			format:  "LRC",
			content: "[00:01.00]<00:01.00>word\n[00:02.00]ordinary line",
			want:    TimingLine,
		},
		{
			name:    "enhanced lrc with untimed ordinary text",
			format:  "LRC",
			content: "[00:01]<00:01>word\nordinary lyric",
			want:    TimingLine,
		},
		{
			name:    "metadata-only lines with enhanced lrc",
			format:  "LRC",
			content: "[ar:Artist]\n[ti:Song]\n[offset:0]\n[00:01]<00:01>word",
			want:    TimingWord,
		},
		{
			name:    "metadata tag with untimed text",
			format:  "LRC",
			content: "[00:01]<00:01>word\n[ar:Artist]ordinary lyric",
			want:    TimingLine,
		},
		{
			name:    "invalid timestamp tag with untimed text",
			format:  "LRC",
			content: "[00:01]<00:01>word\n[00:60]ordinary lyric",
			want:    TimingLine,
		},
		{
			name:    "section tag with untimed text",
			format:  "LRC",
			content: "[00:01]<00:01>word\n[Verse]ordinary lyric",
			want:    TimingLine,
		},
		{
			name:    "decreasing word timestamps within one line",
			format:  "LRC",
			content: "[00:10]<00:11>late<00:10>early",
			want:    TimingLine,
		},
		{
			name:    "equal word timestamps within one line",
			format:  "LRC",
			content: "[00:10]<00:10>first<00:10>second",
			want:    TimingWord,
		},
		{
			name:    "equivalent fractional word timestamps",
			format:  "LRC",
			content: "[00:10]<00:10.1>first<00:10:100>second",
			want:    TimingWord,
		},
		{
			name:    "word timestamps restart on each line",
			format:  "LRC",
			content: "[00:10]<00:10>first<00:11>later\n[00:02]<00:02>second<00:03>later",
			want:    TimingWord,
		},
		{name: "invalid word seconds", format: "LRC", content: "[00:01.00]<00:60.00>word", want: TimingLine},
		{name: "invalid line seconds", format: "LRC", content: "[00:60.00]<00:01.00>word", want: TimingLine},
		{name: "plain text", format: "PLAIN", content: "<00:01.00>word", want: TimingLine},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DetectTiming(test.format, test.content); got != test.want {
				t.Fatalf("DetectTiming(%q, %q) = %q, want %q", test.format, test.content, got, test.want)
			}
		})
	}
}

func TestValidateDocumentRejectsInvalidWordTiming(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		timing  Timing
		content string
	}{
		{
			name: "plain lyrics", format: "PLAIN", timing: TimingWord,
			content: "plain lyrics",
		},
		{
			name: "ordinary line in enhanced document", format: "LRC", timing: TimingWord,
			content: "[00:01.00]<00:01.00>word\n[00:02.00]ordinary line",
		},
		{
			name: "untimed ordinary line in enhanced document", format: "LRC", timing: TimingWord,
			content: "[00:01]<00:01>word\nordinary lyric",
		},
		{
			name: "decreasing word timestamps within one line", format: "LRC", timing: TimingWord,
			content: "[00:10]<00:11>late<00:10>early",
		},
		{
			name: "text before first word marker", format: "LRC", timing: TimingWord,
			content: "[00:01.00]prefix<00:01.00>word",
		},
		{
			name: "no timed lyric lines", format: "LRC", timing: TimingWord,
			content: "[ar:Artist]",
		},
		{
			name: "invalid word timestamp seconds", format: "LRC", timing: TimingWord,
			content: "[00:01.00]<00:60.00>word",
		},
		{
			name: "invalid later word timestamp seconds", format: "LRC", timing: TimingWord,
			content: "[00:01.00]<00:01.00>valid<00:60.00>invalid",
		},
		{
			name: "unterminated later word timestamp", format: "LRC", timing: TimingWord,
			content: "[00:01.00]<00:01.00>valid<00:60.00invalid",
		},
		{
			name: "complete word timing declared as line", format: "LRC", timing: TimingLine,
			content: "[00:01.00]<00:01.00>word",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateDocument(test.format, test.timing, test.content); err == nil {
				t.Fatalf("ValidateDocument(%q, %q, %q) succeeded", test.format, test.timing, test.content)
			}
		})
	}
}

func TestValidateDocumentAcceptsCompleteWordTiming(t *testing.T) {
	contents := []string{
		"[ar:Artist]\n[ti:Song]\n[offset:0]\n[00:01.00]<00:01.00>first<00:01.50> line\n[00:03.00]<00:03.00>second",
		"[00:10]<00:10>first<00:10>second",
		"[00:10]<00:10.1>first<00:10:100>second",
		"[00:10]<00:10>first<00:11>later\n[00:02]<00:02>second<00:03>later",
	}
	for _, content := range contents {
		if err := ValidateDocument("LRC", TimingWord, content); err != nil {
			t.Fatalf("ValidateDocument(%q) error = %v", content, err)
		}
	}
}
