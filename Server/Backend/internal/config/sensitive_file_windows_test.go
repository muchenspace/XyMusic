//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProtectSensitiveFileUsesRestrictedWindowsACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.env")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := protectSensitiveFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
