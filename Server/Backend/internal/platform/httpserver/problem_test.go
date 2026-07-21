package httpserver

import (
	"net/http"
	"strings"
	"testing"

	"xymusic/server/internal/shared/apperror"
)

func TestProblemFromErrorPublishesOnlySafeMetadata(t *testing.T) {
	err := apperror.Conflict(
		apperror.CodeVersionConflict,
		"数据版本已变化",
		map[string]any{
			"expectedVersion":      4,
			"currentVersion":       int64(5),
			"conflictResourceType": "playlist",
			"conflictResourceId":   "playlist-123",
			"unsafe":               "must-not-be-public",
			"albumId":              "contains spaces",
			"fieldErrors": map[string][]string{
				"name":      {"名称不能为空"},
				"__proto__": {"must-not-be-public"},
			},
		},
	)
	problem := ProblemFromError(err, generatedTestTraceID, "/playlists/1")

	if problem.Status != http.StatusConflict || problem.Code != string(apperror.CodeVersionConflict) {
		t.Fatalf("unexpected problem: %#v", problem)
	}
	if problem.Extensions["expectedVersion"] != int64(4) || problem.Extensions["currentVersion"] != int64(5) {
		t.Fatalf("version metadata was not normalized: %#v", problem.Extensions)
	}
	if problem.Extensions["conflictResourceType"] != "playlist" || problem.Extensions["conflictResourceId"] != "playlist-123" {
		t.Fatalf("conflict metadata was not published: %#v", problem.Extensions)
	}
	if _, exists := problem.Extensions["unsafe"]; exists {
		t.Fatalf("unsafe extension was published: %#v", problem.Extensions)
	}
	if _, exists := problem.Extensions["albumId"]; exists {
		t.Fatalf("invalid identifier was published: %#v", problem.Extensions)
	}
	fieldErrors, ok := problem.Extensions["fieldErrors"].(map[string][]string)
	if !ok || len(fieldErrors) != 1 || fieldErrors["name"][0] != "名称不能为空" {
		t.Fatalf("unexpected field error extensions: %#v", problem.Extensions["fieldErrors"])
	}
}

func TestUnknownErrorDoesNotExposeDiagnostics(t *testing.T) {
	problem := ProblemFromError(
		assertionError("database dsn with password"),
		generatedTestTraceID,
		"/api/private",
	)
	if problem.Status != http.StatusInternalServerError || problem.Code != string(apperror.CodeInternalError) {
		t.Fatalf("unexpected problem: %#v", problem)
	}
	if strings.Contains(problem.Detail, "password") || strings.Contains(problem.Detail, "database") {
		t.Fatalf("internal diagnostics leaked: %q", problem.Detail)
	}
}

func TestExplicitInternalErrorDoesNotExposeMetadata(t *testing.T) {
	err := apperror.New(
		apperror.CodeInternalError,
		"database password must not be public",
		apperror.WithMetadata(map[string]any{"trackId": "secret-track"}),
	)
	problem := ProblemFromError(err, generatedTestTraceID, "/api/private")
	if problem.Detail == err.Detail {
		t.Fatalf("internal detail leaked: %q", problem.Detail)
	}
	if len(problem.Extensions) != 0 {
		t.Fatalf("internal metadata leaked: %#v", problem.Extensions)
	}
}

func TestSetupProblemPublishesActionableSafeContext(t *testing.T) {
	err := apperror.New(
		apperror.CodeSetupFailed,
		"初始化在数据库清除阶段失败。",
		apperror.WithMetadata(map[string]any{
			"setupStage":              "database_clear",
			"rollbackIncomplete":      true,
			"destructiveStageStarted": true,
			"reusable":                []string{"administrator", "catalog"},
			"missing":                 []string{"librarySource"},
			"unsafe":                  "postgresql://secret",
		}),
	)
	problem := ProblemFromError(err, generatedTestTraceID, "/api/setup/complete")
	if problem.Code != string(apperror.CodeSetupFailed) || problem.Suggestion == "" {
		t.Fatalf("setup problem is not actionable: %#v", problem)
	}
	if problem.Extensions["setupStage"] != "database_clear" || problem.Extensions["rollbackIncomplete"] != true ||
		problem.Extensions["destructiveStageStarted"] != true {
		t.Fatalf("setup context was not published: %#v", problem.Extensions)
	}
	if _, exists := problem.Extensions["unsafe"]; exists {
		t.Fatalf("unsafe setup metadata was published: %#v", problem.Extensions)
	}
}

func TestDatabaseProblemCodesHaveStableHTTPContracts(t *testing.T) {
	tests := []struct {
		code   apperror.Code
		status int
	}{
		{apperror.CodeDatabaseHostUnresolved, http.StatusUnprocessableEntity},
		{apperror.CodeDatabaseEndpointUnreachable, http.StatusServiceUnavailable},
		{apperror.CodeDatabaseConnectionTimeout, http.StatusServiceUnavailable},
		{apperror.CodeDatabaseNotFound, http.StatusUnprocessableEntity},
		{apperror.CodeDatabaseAuthenticationFailed, http.StatusUnprocessableEntity},
		{apperror.CodeDatabaseTLSFailed, http.StatusUnprocessableEntity},
		{apperror.CodeDatabasePermissionDenied, http.StatusUnprocessableEntity},
		{apperror.CodeDatabaseConnectionLimit, http.StatusServiceUnavailable},
		{apperror.CodeDatabaseConnectionFailed, http.StatusServiceUnavailable},
		{apperror.CodeDatabaseMigrationFailed, http.StatusInternalServerError},
		{apperror.CodeDatabaseMigrationIncompatible, http.StatusConflict},
	}
	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			problem := ProblemFromError(
				apperror.New(test.code, "数据库配置测试失败。"),
				generatedTestTraceID,
				"/api/setup/database/test",
			)
			if problem.Code != string(test.code) || problem.Status != test.status {
				t.Fatalf("unexpected database problem contract: %#v", problem)
			}
			if problem.Detail != "数据库配置测试失败。" || problem.Title == "" || problem.Suggestion == "" {
				t.Fatalf("database problem is not actionable: %#v", problem)
			}
		})
	}
}

func TestCORSConfigurationValidation(t *testing.T) {
	tests := []struct {
		name   string
		config CORSConfig
	}{
		{
			name:   "origin contains a path",
			config: CORSConfig{AllowedOrigins: []string{"https://client.example/path"}},
		},
		{
			name:   "invalid header",
			config: CORSConfig{AllowedHeaders: []string{"Bad Header"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CORS(test.config); err == nil {
				t.Fatal("expected invalid CORS configuration to fail")
			}
		})
	}
}

func TestMediaUploadMatcherIsStrict(t *testing.T) {
	valid := request(t, http.MethodPut, "/api/v1/admin/media/uploads/01234567-89AB-CDEF-0123-456789ABCDEF/content", nil)
	if !IsMediaContentUpload(valid) {
		t.Fatal("expected UUID media upload path to match")
	}
	objectProxy := request(t, http.MethodPut, "/api/v1/oss/b2JqZWN0cy5leGFtcGxlLnRlc3Q/music/song.flac", nil)
	if !IsMediaContentUpload(objectProxy) {
		t.Fatal("expected object storage proxy upload path to match")
	}

	invalidRequests := []*http.Request{
		request(t, http.MethodPost, valid.URL.Path, nil),
		request(t, http.MethodPost, objectProxy.URL.Path, nil),
		request(t, http.MethodPut, "/api/v1/admin/media/uploads/not-a-uuid/content", nil),
		request(t, http.MethodPut, valid.URL.Path+"/extra", nil),
		request(t, http.MethodPut, "/api/v1/admin/media/uploads/01234567-89ab-cdef-0123-456789abcdef", nil),
		request(t, http.MethodPut, "/api/v1/oss/not+base64/music/song.flac", nil),
		request(t, http.MethodPut, "/api/v1/oss/b2JqZWN0cy5leGFtcGxlLnRlc3Q", nil),
	}
	for _, invalid := range invalidRequests {
		if IsMediaContentUpload(invalid) {
			t.Fatalf("unexpected media upload match: %s %s", invalid.Method, invalid.URL.Path)
		}
	}
}

func TestRoutePatternMatcherHandlesParametersAndWildcards(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		{pattern: "/items/:itemID", path: "/items/42", match: true},
		{pattern: "/items/:itemID", path: "/items/", match: false},
		{pattern: "/assets/*path", path: "/assets/css/app.css", match: true},
		{pattern: "/assets/*path", path: "/asset/css/app.css", match: false},
		{pattern: "/health/live", path: "/health/live", match: true},
		{pattern: "/health/live", path: "/health/live/", match: false},
	}
	for _, test := range tests {
		if got := routePatternMatches(test.pattern, test.path); got != test.match {
			t.Fatalf("routePatternMatches(%q, %q) = %v, want %v", test.pattern, test.path, got, test.match)
		}
	}
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
