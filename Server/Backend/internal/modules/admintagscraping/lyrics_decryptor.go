package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

var (
	qrcKey           = []byte("!@#)(*$%123ZXC!@!@#)(NHL")
	krcKey           = []byte("@Gaw^2tGQ61-\xce\xd2ni")
	kugouLyricsSalt  = "LnT6xpN3khm36zse0QzvmgTZ3waWdRSA"
	neteaseEapiKey   = []byte("e82ckenh8dichen8")
)

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

func decryptKRC(encrypted []byte) ([]byte, error) {
	if len(encrypted) <= 4 {
		return nil, errors.New("invalid KRC payload length")
	}
	if len(encrypted) > maximumQRCEncodedBytes {
		return nil, fmt.Errorf("KRC payload exceeds %d bytes", maximumQRCEncodedBytes)
	}
	data := make([]byte, len(encrypted)-4)
	copy(data, encrypted[4:])
	keyLen := len(krcKey)
	for i := 0; i < len(data); i++ {
		data[i] ^= krcKey[i%keyLen]
	}
	return decompressLyrics(data)
}

func calcKugouLyricsSignature(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	builder := strings.Builder{}
	builder.WriteString(kugouLyricsSalt)
	for _, k := range keys {
		builder.WriteString(k)
		builder.WriteString("=")
		builder.WriteString(params[k])
	}
	builder.WriteString(kugouLyricsSalt)
	digest := md5.Sum([]byte(builder.String()))
	return strings.ToLower(hex.EncodeToString(digest[:]))
}

func decryptECB(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	if len(ciphertext) == 0 || len(ciphertext)%blockSize != 0 {
		return nil, errors.New("ciphertext is not aligned to block size")
	}
	decrypted := make([]byte, len(ciphertext))
	for offset := 0; offset < len(ciphertext); offset += blockSize {
		block.Decrypt(decrypted[offset:offset+blockSize], ciphertext[offset:offset+blockSize])
	}
	if len(decrypted) == 0 {
		return nil, errors.New("decrypted content is empty")
	}
	padLen := int(decrypted[len(decrypted)-1])
	if padLen < 1 || padLen > blockSize || padLen > len(decrypted) {
		return nil, errors.New("invalid PKCS7 padding")
	}
	for i := len(decrypted) - padLen; i < len(decrypted); i++ {
		if decrypted[i] != byte(padLen) {
			return nil, errors.New("invalid PKCS7 padding byte")
		}
	}
	return decrypted[:len(decrypted)-padLen], nil
}

func encryptNetEaseEAPI(path string, params any) ([]byte, error) {
	cleanPath := strings.ReplaceAll(path, "eapi", "api")
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal EAPI params: %w", err)
	}
	signSrc := fmt.Sprintf("nobody%suse%smd5forencrypt", cleanPath, string(paramsBytes))
	signDigest := md5.Sum([]byte(signSrc))
	sign := hex.EncodeToString(signDigest[:])
	aesSrc := fmt.Sprintf("%s-36cd479b6b5-%s-36cd479b6b5-%s", cleanPath, string(paramsBytes), sign)
	encrypted, err := encryptECB(neteaseEapiKey, []byte(aesSrc))
	if err != nil {
		return nil, fmt.Errorf("encrypt EAPI payload: %w", err)
	}
	return []byte("params=" + strings.ToUpper(hex.EncodeToString(encrypted))), nil
}

func decryptNetEaseEAPI(responseBytes []byte) ([]byte, error) {
	return decryptECB(neteaseEapiKey, responseBytes)
}

