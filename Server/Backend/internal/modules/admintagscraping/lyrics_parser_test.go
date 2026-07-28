package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestParseYRCBuildsWordTimings(t *testing.T) {
	document, err := parseYRC("[270,3700](270,500,0)二(770,190,0)人")
	if err != nil {
		t.Fatal(err)
	}
	if !document.HasWordTiming || len(document.Lines) != 1 || len(document.Lines[0].Words) != 2 {
		t.Fatalf("parsed YRC = %#v", document)
	}
	if got := document.Lines[0].Words; got[0].StartMS != 270 || got[0].EndMS != 770 || got[0].Text != "二" ||
		got[1].StartMS != 770 || got[1].EndMS != 960 || got[1].Text != "人" {
		t.Fatalf("YRC words = %#v", got)
	}
}

func TestParseQRCBuildsWordTimings(t *testing.T) {
	document, err := parseQRC(`<Lyric_1 LyricType="1" LyricContent="[270,3700]二(270,500)人(770,190)"/>`)
	if err != nil {
		t.Fatal(err)
	}
	if !document.HasWordTiming || len(document.Lines) != 1 || len(document.Lines[0].Words) != 2 {
		t.Fatalf("parsed QRC = %#v", document)
	}
	if got := document.Lines[0].Words; got[0].StartMS != 270 || got[0].EndMS != 770 || got[0].Text != "二" ||
		got[1].StartMS != 770 || got[1].EndMS != 960 || got[1].Text != "人" {
		t.Fatalf("QRC words = %#v", got)
	}
}

func TestParseKRCBuildsWordTimingsRelativeToLine(t *testing.T) {
	document, err := parseKRC("[270,3700]<0,500,0>二<500,190,0>人")
	if err != nil {
		t.Fatal(err)
	}
	if !document.HasWordTiming || len(document.Lines) != 1 || len(document.Lines[0].Words) != 2 {
		t.Fatalf("parsed KRC = %#v", document)
	}
	if got := document.Lines[0].Words; got[0].StartMS != 270 || got[0].EndMS != 770 || got[0].Text != "二" ||
		got[1].StartMS != 770 || got[1].EndMS != 960 || got[1].Text != "人" {
		t.Fatalf("KRC words = %#v", got)
	}
}

func TestRenderEnhancedLRCPreservesWordStartTimes(t *testing.T) {
	document, err := parseYRC("[270,3700](270,500,0)二(770,190,0)人")
	if err != nil {
		t.Fatal(err)
	}
	if got := renderEnhancedLRC(document); got != "[00:00.27]<00:00.27>二<00:00.77>人" {
		t.Fatalf("enhanced LRC = %q", got)
	}
}

func TestParseYRCRejectsInputWithoutWordTimings(t *testing.T) {
	document, err := parseYRC("[270,3700]普通歌词")
	if err != nil {
		t.Fatal(err)
	}
	if document.HasWordTiming {
		t.Fatalf("plain YRC was classified as word-timed: %#v", document)
	}
}

func TestDecryptKRCDecodesCompressedWordLyrics(t *testing.T) {
	plain := []byte("[270,3700]<0,500,0>二<500,190,0>人")
	encrypted := compressAndXORKRC(t, plain)
	got, err := decryptKRC(base64.StdEncoding.EncodeToString(encrypted))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("KRC plaintext = %q", got)
	}
}

func TestDecryptQRCDecodesCompressedWordLyrics(t *testing.T) {
	plain := []byte(`<Lyric_1 LyricType="1" LyricContent="[270,3700]二(270,500)人(770,190)"/>`)
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

func compressAndXORKRC(t *testing.T, plain []byte) []byte {
	t.Helper()
	compressed := zlibBytes(t, plain)
	encrypted := append([]byte("krc1"), compressed...)
	for index := 4; index < len(encrypted); index++ {
		encrypted[index] ^= krcKey[(index-4)%len(krcKey)]
	}
	return encrypted
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
