package security

import (
	"encoding/base64"
	"testing"
)

func TestPayloadCipherRoundTrip(t *testing.T) {
	cipher, err := NewPayloadCipher("01234567890123456789012345678901")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"status": "ok", "count": float64(2)}
	encoded, err := cipher.Encrypt(want)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := cipher.Decrypt(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != want["status"] || got["count"] != want["count"] {
		t.Fatalf("unexpected decrypted payload: %#v", got)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 1
	tampered := base64.RawURLEncoding.EncodeToString(payload)
	if err := cipher.Decrypt(tampered, &got); err == nil {
		t.Fatal("expected tampered payload to fail authentication")
	}
}
