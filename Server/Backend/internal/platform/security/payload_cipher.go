package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

type PayloadCipher struct {
	gcm cipher.AEAD
}

func NewPayloadCipher(secret string) (*PayloadCipher, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create payload cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create payload GCM: %w", err)
	}
	return &PayloadCipher{gcm: gcm}, nil
}

func (c *PayloadCipher) Encrypt(value any) (string, error) {
	plaintext, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("serialize protected payload: %w", err)
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate payload nonce: %w", err)
	}
	sealed := c.gcm.Seal(nil, nonce, plaintext, nil)
	tagLength := c.gcm.Overhead()
	ciphertext := sealed[:len(sealed)-tagLength]
	tag := sealed[len(sealed)-tagLength:]
	payload := make([]byte, 0, len(nonce)+len(tag)+len(ciphertext))
	payload = append(payload, nonce...)
	payload = append(payload, tag...)
	payload = append(payload, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func (c *PayloadCipher) Decrypt(encoded string, destination any) error {
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return errors.New("encrypted payload is invalid")
	}
	nonceLength := c.gcm.NonceSize()
	tagLength := c.gcm.Overhead()
	if len(payload) < nonceLength+tagLength+1 {
		return errors.New("encrypted payload is invalid")
	}
	nonce := payload[:nonceLength]
	tag := payload[nonceLength : nonceLength+tagLength]
	ciphertext := payload[nonceLength+tagLength:]
	sealed := make([]byte, 0, len(ciphertext)+len(tag))
	sealed = append(sealed, ciphertext...)
	sealed = append(sealed, tag...)
	plaintext, err := c.gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return errors.New("encrypted payload is invalid")
	}
	if err := json.Unmarshal(plaintext, destination); err != nil {
		return fmt.Errorf("decode protected payload: %w", err)
	}
	return nil
}
