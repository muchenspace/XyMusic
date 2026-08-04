package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"strconv"
	"testing"
)

func BenchmarkDecryptQRC(b *testing.B) {
	for _, plainSize := range []int{1 << 10, 64 << 10} {
		b.Run(strconv.Itoa(plainSize)+"-bytes", func(b *testing.B) {
			encoded := benchmarkQRCEncodedPayload(b, plainSize)
			b.ReportAllocs()
			b.SetBytes(int64(plainSize))
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				decoded, err := decryptQRC(encoded)
				if err != nil {
					b.Fatal(err)
				}
				if len(decoded) == 0 {
					b.Fatal("QRC payload decoded to an empty document")
				}
			}
		})
	}
}

func benchmarkQRCEncodedPayload(b *testing.B, plainSize int) string {
	b.Helper()
	plain := bytes.Repeat([]byte("[00:01.00]benchmark lyric line\n"), plainSize/31+1)
	plain = plain[:plainSize]
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(plain); err != nil {
		b.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		b.Fatal(err)
	}
	data := compressed.Bytes()
	if remainder := len(data) % 8; remainder != 0 {
		data = append(data, make([]byte, 8-remainder)...)
	}
	encrypted := make([]byte, 0, len(data))
	schedule := qrcTripleKeySchedule(qrcKey, qrcCipherEncrypt)
	for offset := 0; offset < len(data); offset += 8 {
		encrypted = append(encrypted, qrcTripleCryptBlock(data[offset:offset+8], schedule)...)
	}
	return hex.EncodeToString(encrypted)
}
