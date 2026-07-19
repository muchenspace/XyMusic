package parity

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/security"
	"xymusic/server/internal/testsupport"
)

func TestLegacyAndGoAdminCatalogQueryParity(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	legacyBase := os.Getenv("XYMUSIC_LEGACY_BASE_URL")
	if environmentPath == "" || legacyBase == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV and XYMUSIC_LEGACY_BASE_URL to run admin catalog parity")
	}
	testsupport.RequireWriteIntegration(t)
	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	goRuntime, err := app.Bootstrap(ctx, cfg, app.Options{RootDirectory: filepath.Dir(absoluteEnvironmentPath)})
	if err != nil {
		t.Fatal(err)
	}
	defer goRuntime.Close()
	goServer := httptest.NewServer(goRuntime.Handler)
	defer goServer.Close()

	userID := uuid.NewString()
	sessionID := uuid.NewString()
	username := "catalog_parity_" + strings.ReplaceAll(userID[:12], "-", "")
	passwordHash, err := security.HashPassword("admin-catalog-parity-" + userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goRuntime.DB.Exec(ctx, `
		INSERT INTO users (id, username, normalized_username, password_hash, role, status)
		VALUES ($1, $2, $2, $3, 'ADMIN', 'ACTIVE')
	`, userID, username, passwordHash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = goRuntime.DB.Exec(cleanupContext, `DELETE FROM users WHERE id = $1`, userID)
	})
	if _, err := goRuntime.DB.Exec(ctx, `
		INSERT INTO user_profiles (user_id, display_name) VALUES ($1, 'Catalog Parity')
	`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := goRuntime.DB.Exec(ctx, `
		INSERT INTO auth_sessions (id, user_id, installation_id, device_name, platform, app_version)
		VALUES ($1, $2, $3, 'Catalog parity', 'WEB', 'parity/1')
	`, sessionID, userID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	tokens := security.NewAccessTokenService(
		goRuntime.Config.Security.AccessTokenSecret,
		time.Duration(goRuntime.Config.Security.AccessTokenTTLSeconds)*time.Second,
	)
	accessToken, _, err := tokens.Issue(security.Principal{
		UserID: userID, SessionID: sessionID, AuthVersion: 1, Role: "ADMIN",
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	paths := []string{
		"/api/v1/admin/artists?page=1&pageSize=5&sort=name&order=asc",
		"/api/v1/admin/albums?page=1&pageSize=5&sort=updatedAt&order=desc",
		"/api/v1/admin/albums/duplicates",
		"/api/v1/admin/tracks?page=1&pageSize=5&sort=updatedAt&order=desc",
		"/api/v1/admin/tracks?page=1&pageSize=2&metadataStatus=ORIGINAL",
	}
	for index := 0; index < len(paths); index++ {
		path := paths[index]
		legacy := fetchAuthorized(t, client, strings.TrimRight(legacyBase, "/")+path, accessToken)
		modern := fetchAuthorized(t, client, goServer.URL+path, accessToken)
		if legacy.status != modern.status || !semanticAdminJSONEqual(legacy.body, modern.body) {
			t.Fatalf("admin catalog %s differs:\nlegacy=%d %s\ngo=%d %s", path, legacy.status, legacy.body, modern.status, modern.body)
		}
		if index == 0 {
			if id := firstPageID(t, legacy.body); id != "" {
				paths = append(paths, "/api/v1/admin/artists/"+id)
			}
		}
		if index == 1 {
			if id := firstPageID(t, legacy.body); id != "" {
				paths = append(paths, "/api/v1/admin/albums/"+id)
			}
		}
		if index == 3 {
			if id := firstPageID(t, legacy.body); id != "" {
				paths = append(paths, "/api/v1/admin/tracks/"+id)
			}
		}
	}
}

func fetchAuthorized(t *testing.T, client *http.Client, target, accessToken string) response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("X-Trace-Id", "admin-catalog-parity")
	result, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(result.Body); err != nil {
		t.Fatal(err)
	}
	return response{status: result.StatusCode, body: body.Bytes()}
}

func firstPageID(t *testing.T, body []byte) string {
	t.Helper()
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		return ""
	}
	return page.Items[0].ID
}

func semanticAdminJSONEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	normalizeSignedArtwork(leftValue)
	normalizeSignedArtwork(rightValue)
	return deepJSONEqual(leftValue, rightValue)
}

func normalizeSignedArtwork(value any) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			normalizeSignedArtwork(item)
		}
	case map[string]any:
		if _, hasAsset := typed["assetId"]; hasAsset {
			if _, hasCacheKey := typed["cacheKey"]; hasCacheKey {
				delete(typed, "url")
				delete(typed, "expiresAt")
			}
		}
		for _, item := range typed {
			normalizeSignedArtwork(item)
		}
	}
}
