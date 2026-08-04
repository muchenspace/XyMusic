package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
)

var qrcKey = []byte("!@#)(*$%123ZXC!@!@#)(NHL")

const (
	maximumQRCEncodedBytes         = 2 * 1024 * 1024
	maximumDecompressedLyricsBytes = 2 * 1024 * 1024
)

var qrcDecryptSchedule = qrcTripleKeySchedule(qrcKey, qrcCipherDecrypt)

func decryptQRC(encoded string) ([]byte, error) {
	if len(encoded) > maximumQRCEncodedBytes {
		return nil, fmt.Errorf("QRC payload exceeds %d bytes", maximumQRCEncodedBytes)
	}
	encrypted, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode QRC payload: %w", err)
	}
	if len(encrypted) == 0 || len(encrypted)%8 != 0 {
		return nil, fmt.Errorf("QRC payload is not aligned to a DES block")
	}
	decrypted := make([]byte, len(encrypted))
	for offset := 0; offset < len(encrypted); offset += 8 {
		qrcTripleCryptBlockInto(
			encrypted[offset:offset+8], qrcDecryptSchedule, decrypted[offset:offset+8],
		)
	}
	return decompressLyrics(decrypted)
}

func decompressLyrics(content []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("open compressed lyrics: %w", err)
	}
	decompressed, readErr := io.ReadAll(io.LimitReader(reader, maximumDecompressedLyricsBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read compressed lyrics: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close compressed lyrics: %w", closeErr)
	}
	if len(decompressed) > maximumDecompressedLyricsBytes {
		return nil, fmt.Errorf("decompressed lyrics exceed %d bytes", maximumDecompressedLyricsBytes)
	}
	return decompressed, nil
}
