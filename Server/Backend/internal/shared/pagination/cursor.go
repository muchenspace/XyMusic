package pagination

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	"xymusic/server/internal/shared/apperror"
)

type CursorCodec struct {
	secret []byte
}

type cursorEnvelope[T any] struct {
	Version int    `json:"version"`
	Scope   string `json:"scope"`
	Value   T      `json:"value"`
}

func NewCursorCodec(secret string) *CursorCodec {
	return &CursorCodec{secret: []byte(secret)}
}

func EncodeCursor[T any](codec *CursorCodec, scope string, value T) (string, error) {
	contents, err := json.Marshal(cursorEnvelope[T]{Version: 1, Scope: scope, Value: value})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(contents)
	return payload + "." + codec.sign(payload), nil
}

func DecodeCursor[T any](codec *CursorCodec, scope, cursor string) (*T, error) {
	if cursor == "" {
		return nil, nil
	}
	parts := strings.Split(cursor, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !codec.validSignature(parts[0], parts[1]) {
		return nil, apperror.InvalidCursor("分页游标无效")
	}
	contents, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, apperror.InvalidCursor("分页游标无效")
	}
	var envelope cursorEnvelope[T]
	if err := json.Unmarshal(contents, &envelope); err != nil || envelope.Version != 1 || envelope.Scope != scope {
		return nil, apperror.InvalidCursor("分页游标无效")
	}
	return &envelope.Value, nil
}

func (c *CursorCodec) sign(payload string) string {
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *CursorCodec) validSignature(payload, signature string) bool {
	actual, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(c.sign(payload))
	return err == nil && hmac.Equal(actual, expected)
}
