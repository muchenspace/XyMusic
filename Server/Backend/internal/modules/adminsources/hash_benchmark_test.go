package adminsources

import (
	"crypto/sha256"
	"io"
	"os"
	"testing"
)

func BenchmarkFileHashStrategies(b *testing.B) {
	path := b.TempDir() + string(os.PathSeparator) + "payload.bin"
	if err := os.WriteFile(path, make([]byte, 256*1024), 0o600); err != nil {
		b.Fatal(err)
	}
	for _, strategy := range []string{"copy", "copy-buffer", "production"} {
		b.Run(strategy, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(256 * 1024)
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				if strategy == "production" {
					if _, err := fileSHA256(path); err != nil {
						b.Fatal(err)
					}
					continue
				}
				file, err := os.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				hasher := sha256.New()
				if strategy == "copy" {
					_, err = io.Copy(hasher, file)
				} else {
					_, err = io.CopyBuffer(hasher, file, make([]byte, 64*1024))
				}
				_ = hasher.Sum(nil)
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					b.Fatalf("copy=%v close=%v", err, closeErr)
				}
			}
		})
	}
}
