package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"testing"
)

func TestParseQRCBuildsWordTimings(t *testing.T) {
	document, err := parseQRC(`<Lyric_1 LyricType="1" LyricContent="[270,3700]first(270,500)second(770,190)"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if !document.HasWordTiming || len(document.Lines) != 1 || len(document.Lines[0].Words) != 2 {
		t.Fatalf("parsed QRC = %#v", document)
	}
	if got := document.Lines[0].Words; got[0].StartMS != 270 || got[0].EndMS != 770 || got[0].Text != "first" ||
		got[1].StartMS != 770 || got[1].EndMS != 960 || got[1].Text != "second" {
		t.Fatalf("QRC words = %#v", got)
	}
}

func TestRenderEnhancedLRCIncludesTheFinalWordEndTime(t *testing.T) {
	document, err := parseQRC(`<Lyric_1 LyricType="1" LyricContent="[270,3700]first(270,500)second(770,190)"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if got := renderEnhancedLRC(document); got != "[00:00.270]<00:00.270>first<00:00.770>second<00:00.960>" {
		t.Fatalf("enhanced LRC = %q", got)
	}
}

func TestFormatLyricTimestampPreservesMillisecondBoundary(t *testing.T) {
	if got := formatLyricTimestamp(59_999, '[', ']'); got != "[00:59.999]" {
		t.Fatalf("formatLyricTimestamp(59999) = %q", got)
	}
	if got := formatLyricTimestamp(60_000, '<', '>'); got != "<01:00.000>" {
		t.Fatalf("formatLyricTimestamp(60000) = %q", got)
	}
}

func TestRenderEnhancedLRCAddsWordMarkerToUntimedLinesInWordDocument(t *testing.T) {
	document, err := parseQRC(`<Lyric_1 LyricType="1" LyricContent="[0,1000]plain line&#10;[1000,1000]你(1000,500)好(1500,500)"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if !document.HasWordTiming {
		t.Fatal("expected a word-timed lyric document")
	}
	want := "[00:00.000]<00:00.000>plain line<00:01.000>\n[00:01.000]<00:01.000>你<00:01.500>好<00:02.000>"
	if got := renderEnhancedLRC(document); got != want {
		t.Fatalf("enhanced LRC = %q, want %q", got, want)
	}
}

func TestDecryptQRCDecodesCompressedWordLyrics(t *testing.T) {
	plain := []byte(`<Lyric_1 LyricType="1" LyricContent="[270,3700]first(270,500)second(770,190)"/>`)
	compressed := zlibBytes(t, plain)
	if remainder := len(compressed) % 8; remainder != 0 {
		compressed = append(compressed, bytes.Repeat([]byte{0}, 8-remainder)...)
	}
	schedule := qrcTripleKeySchedule(qrcKey, qrcCipherEncrypt)
	encrypted := make([]byte, 0, len(compressed))
	for offset := 0; offset < len(compressed); offset += 8 {
		encrypted = append(encrypted, qrcTripleCryptBlock(compressed[offset:offset+8], schedule)...)
	}
	got, err := decryptQRC(hex.EncodeToString(encrypted))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, plain) {
		t.Fatalf("QRC plaintext = %q", got)
	}
}

func TestDecompressLyricsRejectsOversizedExpansion(t *testing.T) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(bytes.Repeat([]byte{'x'}, maximumDecompressedLyricsBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := decompressLyrics(compressed.Bytes()); err == nil {
		t.Fatal("oversized decompressed lyrics were accepted")
	}
}

func zlibBytes(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
