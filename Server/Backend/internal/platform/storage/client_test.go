package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/config"
)

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		input    string
		endpoint string
		secure   bool
		valid    bool
	}{
		{"", "s3.amazonaws.com", true, true},
		{"http://127.0.0.1:9000", "127.0.0.1:9000", false, true},
		{"http://[2001:db8::10]:9000", "[2001:db8::10]:9000", false, true},
		{"https://minio.example.com/", "minio.example.com", true, true},
		{"minio.example.com", "", false, false},
		{"https://minio.example.com/prefix", "", false, false},
	}
	for _, test := range tests {
		endpoint, secure, err := normalizeEndpoint(test.input)
		if (err == nil) != test.valid || endpoint != test.endpoint || secure != test.secure {
			t.Fatalf("normalizeEndpoint(%q) = %q, %v, %v", test.input, endpoint, secure, err)
		}
	}
}

func TestPresignedGetProxiesConfiguredPublicBaseURL(t *testing.T) {
	client, err := Open(config.Storage{
		Endpoint:        "http://objects.example.test:9000",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Bucket:          "music",
		PublicBaseURL:   "https://cdn.example.test/assets",
	})
	if err != nil {
		t.Fatal(err)
	}
	clientURL, err := client.PresignedGet(context.Background(), "covers/folder name/cover.jpg", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(clientURL, "/api/v1/oss/") || !strings.Contains(clientURL, "/assets/covers/folder%20name/cover.jpg") {
		t.Fatalf("client URL = %q", clientURL)
	}
}
