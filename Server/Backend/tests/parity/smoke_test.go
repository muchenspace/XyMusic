package parity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
)

func TestLegacyAndGoPublicSmokeParity(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	legacyBase := os.Getenv("XYMUSIC_LEGACY_BASE_URL")
	if environmentPath == "" || legacyBase == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV and XYMUSIC_LEGACY_BASE_URL to run differential smoke tests")
	}
	if _, err := url.ParseRequestURI(legacyBase); err != nil {
		t.Fatalf("invalid legacy base URL: %v", err)
	}
	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	goRuntime, err := app.Bootstrap(ctx, cfg, app.Options{RootDirectory: filepath.Dir(absoluteEnvironmentPath)})
	if err != nil {
		t.Fatal(err)
	}
	defer goRuntime.Close()

	goServer := httptest.NewServer(goRuntime.Handler)
	defer goServer.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	for _, path := range []string{"/health/live", "/api/v1/not-a-real-route", "/admin/missing-parity.js"} {
		legacy := fetch(t, client, strings.TrimRight(legacyBase, "/")+path)
		modern := fetch(t, client, goServer.URL+path)
		if legacy.status != modern.status {
			t.Fatalf("%s status differs: legacy=%d go=%d", path, legacy.status, modern.status)
		}
		var legacyJSON any
		var modernJSON any
		if err := json.Unmarshal(legacy.body, &legacyJSON); err != nil {
			t.Fatalf("legacy %s JSON: %v", path, err)
		}
		if err := json.Unmarshal(modern.body, &modernJSON); err != nil {
			t.Fatalf("Go %s JSON: %v", path, err)
		}
		if !deepJSONEqual(legacyJSON, modernJSON) {
			t.Fatalf("%s body differs:\nlegacy=%s\ngo=%s", path, legacy.body, modern.body)
		}
	}

	legacyAdmin := fetch(t, client, strings.TrimRight(legacyBase, "/")+"/admin/")
	goAdmin := fetch(t, client, goServer.URL+"/admin/")
	if legacyAdmin.status != goAdmin.status || !bytes.Equal(legacyAdmin.body, goAdmin.body) {
		t.Fatalf("admin index differs: legacy=%d/%d bytes go=%d/%d bytes", legacyAdmin.status, len(legacyAdmin.body), goAdmin.status, len(goAdmin.body))
	}
	redirectClient := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, path := range []string{"/", "/admin"} {
		legacy := fetch(t, redirectClient, strings.TrimRight(legacyBase, "/")+path)
		modern := fetch(t, redirectClient, goServer.URL+path)
		if legacy.status != modern.status || legacy.location != modern.location || string(legacy.body) != string(modern.body) {
			t.Fatalf("%s redirect response differs: legacy=%d %q %q go=%d %q %q", path, legacy.status, legacy.location, legacy.body, modern.status, modern.location, modern.body)
		}
	}
	for _, path := range []string{"/admin", "/admin/app.js"} {
		legacy := fetchRequest(t, redirectClient, http.MethodPost, strings.TrimRight(legacyBase, "/")+path, nil)
		modern := fetchRequest(t, redirectClient, http.MethodPost, goServer.URL+path, nil)
		if legacy.status != modern.status || legacy.allow != modern.allow || string(legacy.body) != string(modern.body) {
			t.Fatalf("%s method response differs: legacy=%d %q %q go=%d %q %q", path, legacy.status, legacy.allow, legacy.body, modern.status, modern.allow, modern.body)
		}
	}
	invalidRequests := []struct {
		path string
		body []byte
	}{
		{"/api/v1/auth/login", []byte(`{}`)},
		{"/api/v1/auth/login", []byte(`{`)},
		{"/api/v1/auth/register", []byte(`{"username":"!","password":"short"}`)},
		{"/api/v1/auth/refresh", []byte(`{"refreshToken":"short"}`)},
	}
	for _, invalid := range invalidRequests {
		legacyInvalid := fetchRequest(t, client, http.MethodPost, strings.TrimRight(legacyBase, "/")+invalid.path, invalid.body)
		goInvalid := fetchRequest(t, client, http.MethodPost, goServer.URL+invalid.path, invalid.body)
		if legacyInvalid.status != goInvalid.status || !semanticJSONEqual(legacyInvalid.body, goInvalid.body) {
			t.Fatalf("invalid request %s contract differs:\nlegacy=%d %s\ngo=%d %s", invalid.path, legacyInvalid.status, legacyInvalid.body, goInvalid.status, goInvalid.body)
		}
	}
	for _, path := range []string{"/api/v1/auth/logout", "/api/v1/auth/logout-all"} {
		legacyUnauthorized := fetchRequest(t, client, http.MethodPost, strings.TrimRight(legacyBase, "/")+path, nil)
		goUnauthorized := fetchRequest(t, client, http.MethodPost, goServer.URL+path, nil)
		if legacyUnauthorized.status != goUnauthorized.status || !semanticJSONEqual(legacyUnauthorized.body, goUnauthorized.body) {
			t.Fatalf("unauthorized request %s differs:\nlegacy=%d %s\ngo=%d %s", path, legacyUnauthorized.status, legacyUnauthorized.body, goUnauthorized.status, goUnauthorized.body)
		}
	}
	for _, path := range []string{
		"/api/v1/tracks",
		"/api/v1/artists",
		"/api/v1/albums",
		"/api/v1/search?q=test&scope=ALL",
	} {
		legacyUnauthorized := fetch(t, client, strings.TrimRight(legacyBase, "/")+path)
		goUnauthorized := fetch(t, client, goServer.URL+path)
		if legacyUnauthorized.status != goUnauthorized.status || !semanticJSONEqual(legacyUnauthorized.body, goUnauthorized.body) {
			t.Fatalf("unauthorized catalog request %s differs:\nlegacy=%d %s\ngo=%d %s", path, legacyUnauthorized.status, legacyUnauthorized.body, goUnauthorized.status, goUnauthorized.body)
		}
	}
	playbackPath := "/api/v1/tracks/00000000-0000-4000-8000-000000000001/playback"
	playbackBody := []byte(`{"preferredQuality":"AUTO"}`)
	legacyPlayback := fetchRequest(t, client, http.MethodPost, strings.TrimRight(legacyBase, "/")+playbackPath, playbackBody)
	goPlayback := fetchRequest(t, client, http.MethodPost, goServer.URL+playbackPath, playbackBody)
	if legacyPlayback.status != goPlayback.status || !semanticJSONEqual(legacyPlayback.body, goPlayback.body) {
		t.Fatalf("unauthorized playback differs:\nlegacy=%d %s\ngo=%d %s", legacyPlayback.status, legacyPlayback.body, goPlayback.status, goPlayback.body)
	}
	invalidCatalogRequests := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/v1/tracks?sort=INVALID", nil},
		{http.MethodGet, "/api/v1/tracks/not-a-uuid", nil},
		{http.MethodGet, "/api/v1/artists?limit=0", nil},
		{http.MethodPost, "/api/v1/tracks/random", []byte(`{"limit":0}`)},
		{http.MethodPost, playbackPath, []byte(`{"preferredQuality":"INVALID"}`)},
	}
	for _, invalid := range invalidCatalogRequests {
		legacyInvalid := fetchRequest(t, client, invalid.method, strings.TrimRight(legacyBase, "/")+invalid.path, invalid.body)
		goInvalid := fetchRequest(t, client, invalid.method, goServer.URL+invalid.path, invalid.body)
		if legacyInvalid.status != goInvalid.status || !semanticJSONEqual(legacyInvalid.body, goInvalid.body) {
			t.Fatalf("invalid catalog request %s %s differs:\nlegacy=%d %s\ngo=%d %s", invalid.method, invalid.path, legacyInvalid.status, legacyInvalid.body, goInvalid.status, goInvalid.body)
		}
	}
	profileRequests := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/v1/users/me", nil},
		{http.MethodPatch, "/api/v1/users/me", []byte(`{"expectedVersion":1,"displayName":"Listener","unknown":true}`)},
		{http.MethodPost, "/api/v1/users/me/avatar/uploads", []byte(`{"fileName":"avatar.png","contentType":"image/png","sizeBytes":128,"checksumSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)},
		{http.MethodPost, "/api/v1/users/me/avatar/uploads/00000000-0000-4000-8000-000000000001/complete", []byte(`{}`)},
	}
	for _, profileRequest := range profileRequests {
		legacyResponse := fetchRequest(t, client, profileRequest.method, strings.TrimRight(legacyBase, "/")+profileRequest.path, profileRequest.body)
		goResponse := fetchRequest(t, client, profileRequest.method, goServer.URL+profileRequest.path, profileRequest.body)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("profile request %s %s differs:\nlegacy=%d %s\ngo=%d %s", profileRequest.method, profileRequest.path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
	adminAuthRequests := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/v1/admin/auth/session", nil},
		{http.MethodPost, "/api/v1/admin/auth/logout", nil},
		{http.MethodPost, "/api/v1/admin/auth/login", []byte(`{}`)},
	}
	for _, adminRequest := range adminAuthRequests {
		legacyResponse := fetchRequest(t, client, adminRequest.method, strings.TrimRight(legacyBase, "/")+adminRequest.path, adminRequest.body)
		goResponse := fetchRequest(t, client, adminRequest.method, goServer.URL+adminRequest.path, adminRequest.body)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("admin auth request %s %s differs:\nlegacy=%d %s\ngo=%d %s", adminRequest.method, adminRequest.path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
	adminUserID := "00000000-0000-4000-8000-000000000001"
	adminSessionID := "00000000-0000-4000-8000-000000000002"
	adminManagementRequests := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/v1/admin/dashboard", nil},
		{http.MethodGet, "/api/v1/admin/users?page=1&pageSize=25&role=ADMIN&status=ACTIVE", nil},
		{http.MethodPost, "/api/v1/admin/users", []byte(`{"username":"Parity_1","password":"secret1","displayName":"Parity","role":"USER"}`)},
		{http.MethodGet, "/api/v1/admin/users/" + adminUserID, nil},
		{http.MethodPatch, "/api/v1/admin/users/" + adminUserID, []byte(`{"expectedVersion":1,"displayName":"Parity","reason":"test"}`)},
		{http.MethodPost, "/api/v1/admin/users/" + adminUserID + "/password", []byte(`{"expectedVersion":1,"password":"secret2","reason":"test"}`)},
		{http.MethodPost, "/api/v1/admin/users/" + adminUserID + "/sessions/" + adminSessionID + "/revoke", []byte(`{"reason":"test"}`)},
		{http.MethodDelete, "/api/v1/admin/users/" + adminUserID, []byte(`{"expectedVersion":1,"reason":"test"}`)},
		{http.MethodPost, "/api/v1/admin/users/" + adminUserID + "/restore", []byte(`{"expectedVersion":1,"reason":"test"}`)},
	}
	for _, adminRequest := range adminManagementRequests {
		legacyResponse := fetchRequest(t, client, adminRequest.method, strings.TrimRight(legacyBase, "/")+adminRequest.path, adminRequest.body)
		goResponse := fetchRequest(t, client, adminRequest.method, goServer.URL+adminRequest.path, adminRequest.body)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("admin management request %s %s differs:\nlegacy=%d %s\ngo=%d %s", adminRequest.method, adminRequest.path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
	invalidAdminManagementRequests := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodGet, "/api/v1/admin/users?page=0", nil},
		{http.MethodGet, "/api/v1/admin/users/not-a-uuid", nil},
		{http.MethodPost, "/api/v1/admin/users", []byte(`{}`)},
		{http.MethodPatch, "/api/v1/admin/users/" + adminUserID, []byte(`{"expectedVersion":0,"reason":"test"}`)},
	}
	for _, adminRequest := range invalidAdminManagementRequests {
		legacyResponse := fetchRequest(t, client, adminRequest.method, strings.TrimRight(legacyBase, "/")+adminRequest.path, adminRequest.body)
		goResponse := fetchRequest(t, client, adminRequest.method, goServer.URL+adminRequest.path, adminRequest.body)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("invalid admin management request %s %s differs:\nlegacy=%d %s\ngo=%d %s", adminRequest.method, adminRequest.path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
	adminCatalogRequests := []string{
		"/api/v1/admin/artists?page=1&pageSize=5&sort=name&order=asc",
		"/api/v1/admin/artists/" + adminUserID,
		"/api/v1/admin/albums?sort=updatedAt&order=desc",
		"/api/v1/admin/albums/duplicates",
		"/api/v1/admin/albums/" + adminUserID,
		"/api/v1/admin/tracks?status=READY&metadataStatus=ORIGINAL",
		"/api/v1/admin/tracks/" + adminUserID,
	}
	for _, path := range adminCatalogRequests {
		legacyResponse := fetch(t, client, strings.TrimRight(legacyBase, "/")+path)
		goResponse := fetch(t, client, goServer.URL+path)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("admin catalog request %s differs:\nlegacy=%d %s\ngo=%d %s", path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
	for _, path := range []string{
		"/api/v1/admin/artists?page=0",
		"/api/v1/admin/albums?sort=invalid",
		"/api/v1/admin/tracks?metadataStatus=INVALID",
		"/api/v1/admin/tracks?sourceId=invalid",
	} {
		legacyResponse := fetch(t, client, strings.TrimRight(legacyBase, "/")+path)
		goResponse := fetch(t, client, goServer.URL+path)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("invalid admin catalog request %s differs:\nlegacy=%d %s\ngo=%d %s", path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
	mutationOtherID := "00000000-0000-4000-8000-000000000002"
	adminMutationRequests := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/v1/admin/artists", []byte(`{"name":"Parity artist"}`)},
		{http.MethodPatch, "/api/v1/admin/artists/" + adminUserID, []byte(`{"expectedVersion":1,"description":null}`)},
		{http.MethodPost, "/api/v1/admin/albums", []byte(`{"title":"Parity album","artistCredits":[{"artistId":"` + adminUserID + `","role":"PRIMARY","sortOrder":0}]}`)},
		{http.MethodPatch, "/api/v1/admin/albums/" + adminUserID, []byte(`{"expectedVersion":1,"releaseDate":null}`)},
		{http.MethodPost, "/api/v1/admin/albums/merge", []byte(`{"target":{"albumId":"` + adminUserID + `","expectedVersion":1},"sources":[{"albumId":"` + mutationOtherID + `","expectedVersion":1}],"fieldSources":{"title":"` + adminUserID + `","cover":null,"artistCredits":"` + adminUserID + `","releaseDate":null,"description":null}}`)},
		{http.MethodPost, "/api/v1/admin/tracks", []byte(`{"title":"Parity track","artistCredits":[{"artistId":"` + adminUserID + `","role":"PRIMARY","sortOrder":0}],"discNumber":1}`)},
		{http.MethodPatch, "/api/v1/admin/tracks/" + adminUserID, []byte(`{"expectedVersion":1,"trackNumber":null}`)},
		{http.MethodPost, "/api/v1/admin/tracks/" + adminUserID + "/publish", []byte(`{"expectedVersion":1}`)},
		{http.MethodPost, "/api/v1/admin/tracks/" + adminUserID + "/archive", []byte(`{"expectedVersion":1}`)},
		{http.MethodPost, "/api/v1/admin/tracks/" + adminUserID + "/restore", []byte(`{"expectedVersion":1}`)},
		{http.MethodDelete, "/api/v1/admin/tracks/" + adminUserID, []byte(`{"expectedVersion":1}`)},
		{http.MethodPut, "/api/v1/admin/tracks/" + adminUserID + "/lyrics", []byte(`{"expectedVersion":1,"language":"zh","format":"LRC","content":"","isDefault":true}`)},
		{http.MethodPatch, "/api/v1/admin/users/" + adminUserID + "/status", []byte(`{"expectedVersion":1,"status":"SUSPENDED","reason":"parity"}`)},
	}
	for _, mutation := range adminMutationRequests {
		legacyResponse := fetchRequest(t, client, mutation.method, strings.TrimRight(legacyBase, "/")+mutation.path, mutation.body)
		goResponse := fetchRequest(t, client, mutation.method, goServer.URL+mutation.path, mutation.body)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("admin mutation request %s %s differs:\nlegacy=%d %s\ngo=%d %s", mutation.method, mutation.path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
	for _, mutation := range []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/v1/admin/artists", []byte(`{}`)},
		{http.MethodPost, "/api/v1/admin/albums", []byte(`{"title":"Album","artistCredits":[]}`)},
		{http.MethodPost, "/api/v1/admin/tracks", []byte(`{"title":"Track","artistCredits":[],"discNumber":0}`)},
		{http.MethodPut, "/api/v1/admin/tracks/" + adminUserID + "/lyrics", []byte(`{"expectedVersion":1,"language":"z","format":"BAD","content":""}`)},
	} {
		legacyResponse := fetchRequest(t, client, mutation.method, strings.TrimRight(legacyBase, "/")+mutation.path, mutation.body)
		goResponse := fetchRequest(t, client, mutation.method, goServer.URL+mutation.path, mutation.body)
		if legacyResponse.status != goResponse.status || !semanticJSONEqual(legacyResponse.body, goResponse.body) {
			t.Fatalf("invalid admin mutation %s %s differs:\nlegacy=%d %s\ngo=%d %s", mutation.method, mutation.path, legacyResponse.status, legacyResponse.body, goResponse.status, goResponse.body)
		}
	}
}

type response struct {
	status   int
	body     []byte
	location string
	allow    string
}

func fetch(t *testing.T, client *http.Client, target string) response {
	return fetchRequest(t, client, http.MethodGet, target, nil)
}

func fetchRequest(t *testing.T, client *http.Client, method, target string, body []byte) response {
	t.Helper()
	request, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Trace-Id", "parity-trace-0001")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	result, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer result.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(result.Body, 8*1024*1024))
	if err != nil {
		t.Fatal(err)
	}
	return response{status: result.StatusCode, body: responseBody, location: result.Header.Get("Location"), allow: result.Header.Get("Allow")}
}

func deepJSONEqual(left, right any) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func semanticJSONEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil && deepJSONEqual(leftValue, rightValue)
}
