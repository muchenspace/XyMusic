package admintagscraping

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

var (
	qrcKey = []byte("!@#)(*$%123ZXC!@!@#)(NHL")
	krcKey = []byte("@Gaw^2tGQ61-\xce\xd2ni")
)

func decryptKRC(encoded string) ([]byte, error) {
	encrypted, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode KRC payload: %w", err)
	}
	return decryptKRCBytes(encrypted)
}

func decryptKRCBytes(encrypted []byte) ([]byte, error) {
	if len(encrypted) < 4 || string(encrypted[:4]) != "krc1" {
		return nil, fmt.Errorf("KRC magic header is invalid")
	}
	for index := 4; index < len(encrypted); index++ {
		encrypted[index] ^= krcKey[(index-4)%len(krcKey)]
	}
	return decompressLyrics(encrypted[4:])
}

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

func encryptNeteaseEAPIParams(path string, params map[string]any, key []byte) ([]byte, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	pathBytes := []byte(path)
	signInput := make([]byte, 0, len(pathBytes)+len(encoded)+32)
	signInput = append(signInput, []byte("nobody")...)
	signInput = append(signInput, pathBytes...)
	signInput = append(signInput, []byte("use")...)
	signInput = append(signInput, encoded...)
	signInput = append(signInput, []byte("md5forencrypt")...)
	digest := md5.Sum(signInput)
	plain := make([]byte, 0, len(pathBytes)+len(encoded)+64)
	plain = append(plain, pathBytes...)
	plain = append(plain, []byte("-36cd479b6b5-")...)
	plain = append(plain, encoded...)
	plain = append(plain, []byte("-36cd479b6b5-")...)
	plain = append(plain, []byte(fmt.Sprintf("%x", digest))...)
	return encryptECB(key, plain)
}

func decryptNeteaseEAPIResponse(ciphertext, key []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(ciphertext)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return trimmed, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("Netease EAPI response is not aligned to an AES block")
	}
	decrypted := make([]byte, len(ciphertext))
	for offset := 0; offset < len(ciphertext); offset += block.BlockSize() {
		block.Decrypt(decrypted[offset:offset+block.BlockSize()], ciphertext[offset:offset+block.BlockSize()])
	}
	padding := int(decrypted[len(decrypted)-1])
	if padding < 1 || padding > block.BlockSize() || padding > len(decrypted) {
		return nil, fmt.Errorf("Netease EAPI response padding is invalid")
	}
	for _, value := range decrypted[len(decrypted)-padding:] {
		if int(value) != padding {
			return nil, fmt.Errorf("Netease EAPI response padding is invalid")
		}
	}
	return decrypted[:len(decrypted)-padding], nil
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
