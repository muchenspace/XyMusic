package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsolatedDatabaseURLsPreserveConnectionOptions(t *testing.T) {
	source := "postgres://user:secret@db.example:5432/xymusic?sslmode=require&application_name=test"
	isolated, admin, err := isolatedDatabaseURLs(source, "xymusic_it_abc")
	if err != nil {
		t.Fatal(err)
	}
	for candidate, expectedDatabase := range map[string]string{isolated: "xymusic_it_abc", admin: "postgres"} {
		parsed, err := url.Parse(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimPrefix(parsed.Path, "/") != expectedDatabase || parsed.Query().Get("sslmode") != "require" {
			t.Fatalf("url=%s", candidate)
		}
	}
}

func TestCreateDirectoryRequiresIsolatedPrefix(t *testing.T) {
	if err := ensureCreateDirectory(t.TempDir()); err == nil {
		t.Fatal("unsafe directory name was accepted")
	}
}

func TestLoadIsolatedEnvironmentRejectsOrdinaryDirectory(t *testing.T) {
	if _, _, err := loadIsolatedEnvironment(filepath.Join(t.TempDir(), ".env")); err == nil {
		t.Fatal("ordinary environment directory was accepted")
	}
}

func TestResetRateLimitsRequiresEnvironmentPath(t *testing.T) {
	if err := runResetRateLimits(nil); err == nil {
		t.Fatal("missing isolated environment path was accepted")
	}
}

func TestWriteTestCredentialsCreatesExclusiveJSONDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test-credentials.json")
	want := testCredentials{
		BaseURL: "http://127.0.0.1:3102",
		Admin:   accountCredentials{Username: "isolated_admin", Password: "admin-secret", UserID: "admin-id"},
		User:    accountCredentials{Username: "isolated_user", Password: "user-secret", UserID: "user-id"},
		Windows: accountCredentials{Username: "isolated_windows", Password: "windows-secret", UserID: "windows-id"},
		Android: accountCredentials{Username: "isolated_android", Password: "android-secret", UserID: "android-id"},
	}
	if err := writeTestCredentials(path, want); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got testCredentials
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("credentials = %#v, want %#v", got, want)
	}
	if err := writeTestCredentials(path, want); err == nil {
		t.Fatal("existing credentials file was overwritten")
	}
}
