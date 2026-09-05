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

func TestParseKRCBuildsWordTimings(t *testing.T) {
	krc := "[ti:晴天]\n[ar:周杰伦]\n[0,2250]<0,160,0>晴<160,160,0>天<320,160,0>空\n[2250,2250]<0,450,0>词<450,450,0>："
	document, err := parseKRC(krc)
	if err != nil {
		t.Fatal(err)
	}
	if !document.HasWordTiming || len(document.Lines) != 2 {
		t.Fatalf("parsed KRC = %#v", document)
	}
	line1 := document.Lines[0]
	if line1.StartMS != 0 || line1.EndMS != 2250 || len(line1.Words) != 3 {
		t.Fatalf("line 1 = %#v", line1)
	}
	if line1.Words[0].StartMS != 0 || line1.Words[0].EndMS != 160 || line1.Words[0].Text != "晴" {
		t.Fatalf("word 1 = %#v", line1.Words[0])
	}
	if line1.Words[1].StartMS != 160 || line1.Words[1].EndMS != 320 || line1.Words[1].Text != "天" {
		t.Fatalf("word 2 = %#v", line1.Words[1])
	}
	if line1.Words[2].StartMS != 320 || line1.Words[2].EndMS != 480 || line1.Words[2].Text != "空" {
		t.Fatalf("word 3 = %#v", line1.Words[2])
	}
	line2 := document.Lines[1]
	if line2.StartMS != 2250 || line2.EndMS != 4500 || len(line2.Words) != 2 {
		t.Fatalf("line 2 = %#v", line2)
	}
	if line2.Words[0].StartMS != 2250 || line2.Words[0].EndMS != 2700 || line2.Words[0].Text != "词" {
		t.Fatalf("line 2 word 1 = %#v", line2.Words[0])
	}
	enhanced := renderEnhancedLRC(document)
	want := "[00:00.000]<00:00.000>晴<00:00.160>天<00:00.320>空<00:00.480>\n[00:02.250]<00:02.250>词<00:02.700>：<00:03.150>"
	if enhanced != want {
		t.Fatalf("renderEnhancedLRC(krc) = %q, want %q", enhanced, want)
	}
}


func TestParseYRCBuildsWordTimings(t *testing.T) {
	yrc := "{\"t\":0,\"c\":[{\"tx\":\"制作人\"}]}\n[27630,5520](27630,300,0)我(27930,290,0)的(28220,340,0)青\n[33890,4720](33890,460,0)是(34350,360,0)明"
	document, err := parseYRC(yrc)
	if err != nil {
		t.Fatal(err)
	}
	if !document.HasWordTiming || len(document.Lines) != 2 {
		t.Fatalf("parsed YRC = %#v", document)
	}
	line1 := document.Lines[0]
	if line1.StartMS != 27630 || line1.EndMS != 33150 || len(line1.Words) != 3 {
		t.Fatalf("line 1 = %#v", line1)
	}
	if line1.Words[0].StartMS != 27630 || line1.Words[0].EndMS != 27930 || line1.Words[0].Text != "我" {
		t.Fatalf("word 1 = %#v", line1.Words[0])
	}
	if line1.Words[1].StartMS != 27930 || line1.Words[1].EndMS != 28220 || line1.Words[1].Text != "的" {
		t.Fatalf("word 2 = %#v", line1.Words[1])
	}
	enhanced := renderEnhancedLRC(document)
	want := "[00:27.630]<00:27.630>我<00:27.930>的<00:28.220>青<00:28.560>\n[00:33.890]<00:33.890>是<00:34.350>明<00:34.710>"
	if enhanced != want {
		t.Fatalf("renderEnhancedLRC(yrc) = %q, want %q", enhanced, want)
	}
}

func TestDecryptKRCDecodesPayload(t *testing.T) {
	plain := []byte("[0,1000]<0,500,0>你<500,500,0>好")
	compressed := zlibBytes(t, plain)
	keyLen := len(krcKey)
	xorPayload := make([]byte, len(compressed))
	for i := 0; i < len(compressed); i++ {
		xorPayload[i] = compressed[i] ^ krcKey[i%keyLen]
	}
	krcRaw := append([]byte("krc1"), xorPayload...)
	decrypted, err := decryptKRC(krcRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("decrypted KRC = %q, want %q", string(decrypted), string(plain))
	}
}

func TestNetEaseEAPIEncryptDecryptRoundtrip(t *testing.T) {
	params := map[string]any{
		"id": 12345,
		"lv": "-1",
		"yv": "-1",
	}
	encryptedBody, err := encryptNetEaseEAPI("/eapi/song/lyric/v1", params)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encryptedBody, []byte("params=")) {
		t.Fatalf("encryptedBody = %q", string(encryptedBody))
	}
	hexPart := string(bytes.TrimPrefix(encryptedBody, []byte("params=")))
	cipherBytes, err := hex.DecodeString(hexPart)
	if err != nil {
		t.Fatal(err)
	}
	decryptedBytes, err := decryptNetEaseEAPI(cipherBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(decryptedBytes, []byte("/api/song/lyric/v1")) || !bytes.Contains(decryptedBytes, []byte(`"id":12345`)) {
		t.Fatalf("decrypted payload = %q", string(decryptedBytes))
	}
}

func TestParseTimedLinesPreservesOffset(t *testing.T) {
	qrcWithOffset := `<Lyric_1 LyricType="1" LyricContent="[offset:500]&#10;[270,3700]first(270,500)second(770,190)"/>`
	doc, err := parseQRC(qrcWithOffset)
	if err != nil {
		t.Fatal(err)
	}
	if doc.OffsetMS != 500 {
		t.Fatalf("doc.OffsetMS = %d, want 500", doc.OffsetMS)
	}
	rendered := renderEnhancedLRC(doc)
	want := "[offset:500]\n[00:00.270]<00:00.270>first<00:00.770>second<00:00.960>"
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}

	qrcZeroOffset := `<Lyric_1 LyricType="1" LyricContent="[offset:0]&#10;[270,3700]first(270,500)second(770,190)"/>`
	docZero, err := parseQRC(qrcZeroOffset)
	if err != nil {
		t.Fatal(err)
	}
	if docZero.OffsetMS != 0 {
		t.Fatalf("docZero.OffsetMS = %d, want 0", docZero.OffsetMS)
	}
	renderedZero := renderEnhancedLRC(docZero)
	wantZero := "[00:00.270]<00:00.270>first<00:00.770>second<00:00.960>"
	if renderedZero != wantZero {
		t.Fatalf("renderedZero = %q, want %q", renderedZero, wantZero)
	}
}


