package storage

import "testing"

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		input    string
		endpoint string
		secure   bool
		valid    bool
	}{
		{"", "s3.amazonaws.com", true, true},
		{"http://127.0.0.1:9000", "127.0.0.1:9000", false, true},
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
