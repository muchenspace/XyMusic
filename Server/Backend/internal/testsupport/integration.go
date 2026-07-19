package testsupport

import (
	"os"
	"testing"
)

const WriteIntegrationEnvironment = "XYMUSIC_ALLOW_WRITE_INTEGRATION"

// RequireWriteIntegration prevents a configured production dependency probe
// from silently becoming a destructive lifecycle test. Write-capable tests
// must run against an isolated database and object-storage bucket.
func RequireWriteIntegration(t testing.TB) {
	t.Helper()
	if os.Getenv(WriteIntegrationEnvironment) != "1" {
		t.Skip("set " + WriteIntegrationEnvironment + "=1 only for an isolated integration environment")
	}
}
