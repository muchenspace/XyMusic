package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xymusic/server/internal/config"
)

func TestMinIOUploadUsesPutChecksumWithoutStat(t *testing.T) {
	content := []byte("media upload checksum fast path")
	digest := sha256.Sum256(content)
	checksum := hex.EncodeToString(digest[:])
	var headCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			response.Header().Set("Content-Type", "application/xml")
			_, _ = response.Write([]byte(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`))
		case http.MethodPut:
			_, _ = io.Copy(io.Discard, request.Body)
			response.Header().Set("ETag", "\"upload-etag\"")
			response.Header().Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(digest[:]))
			response.WriteHeader(http.StatusOK)
		case http.MethodHead:
			if strings.Contains(request.URL.Path, "/objects/source") {
				headCalls.Add(1)
			}
			response.WriteHeader(http.StatusOK)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	storage, err := NewMinIOObjectStorage(config.Storage{
		Endpoint: server.URL, Bucket: "bucket", AccessKeyID: "access", SecretAccessKey: "secret",
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	size, err := storage.UploadFile(t.Context(), "objects/source", path, "application/octet-stream", checksum)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(content)) {
		t.Fatalf("size=%d, want %d", size, len(content))
	}
	if headCalls.Load() != 0 {
		t.Fatalf("HEAD requests=%d, want 0", headCalls.Load())
	}
}

func TestDecodeSHA256Hex(t *testing.T) {
	digest := sha256.Sum256([]byte("checksum"))
	if decoded, err := decodeSHA256Hex(hex.EncodeToString(digest[:])); err != nil || string(decoded) != string(digest[:]) {
		t.Fatalf("decode valid checksum: bytes=%x err=%v", decoded, err)
	}
	if _, err := decodeSHA256Hex("invalid"); err == nil {
		t.Fatal("expected invalid checksum error")
	}
}

func BenchmarkMinIOUploadAgainstHTTPStub(b *testing.B) {
	content := bytes.Repeat([]byte("media upload benchmark"), 16*1024)
	digest := sha256.Sum256(content)
	checksum := hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			_, _ = io.Copy(io.Discard, request.Body)
			response.Header().Set("ETag", "\"upload-etag\"")
			response.Header().Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(digest[:]))
			response.WriteHeader(http.StatusOK)
		case http.MethodGet:
			response.Header().Set("Content-Type", "application/xml")
			_, _ = response.Write([]byte(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`))
		case http.MethodHead:
			response.WriteHeader(http.StatusOK)
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	storage, err := NewMinIOObjectStorage(config.Storage{
		Endpoint: server.URL, Bucket: "bucket", AccessKeyID: "access", SecretAccessKey: "secret",
		ForcePathStyle: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(b.TempDir(), "source.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	b.ResetTimer()
	started := time.Now()
	for index := 0; index < b.N; index++ {
		if _, err := storage.UploadFile(
			context.Background(), "objects/"+strconv.Itoa(index), path,
			"application/octet-stream", checksum,
		); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/time.Since(started).Seconds(), "uploads/sec")
}
