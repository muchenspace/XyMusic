package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
)

var qrcKey = []byte("!@#)(*$%123ZXC!@!@#)(NHL")

func decryptQRC(encoded string) ([]byte, error) {
	encrypted, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode QRC payload: %w", err)
	}
	if len(encrypted) == 0 || len(encrypted)%8 != 0 {
		return nil, fmt.Errorf("QRC payload is not aligned to a DES block")
	}
	schedule := qrcTripleKeySchedule(qrcKey, qrcCipherDecrypt)
	decrypted := make([]byte, 0, len(encrypted))
	for offset := 0; offset < len(encrypted); offset += 8 {
		decrypted = append(decrypted, qrcTripleCryptBlock(encrypted[offset:offset+8], schedule)...)
	}
	return decompressLyrics(decrypted)
}

func decompressLyrics(content []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("open compressed lyrics: %w", err)
	}
	decompressed, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read compressed lyrics: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close compressed lyrics: %w", closeErr)
	}
	return decompressed, nil
}
