package adminweb

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/platform/httpserver"
)

func TestAssetsServeFilesAndSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>admin</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("console.log('ok')"), 0o600); err != nil {
		t.Fatal(err)
	}
	assets, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: assets.Register})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path, contains string
		status         int
	}{
		{"/admin/app.js", "console.log", http.StatusOK},
		{"/admin/albums/123", "<main>admin</main>", http.StatusOK},
		{"/admin/missing.js", "RESOURCE_NOT_FOUND", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.contains) {
			t.Fatalf("GET %s = %d %q", test.path, response.Code, response.Body.String())
		}
	}
}

func TestAssetsRejectTraversal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: assets.Register})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/%2e%2e/secret", nil))
	if response.Code != http.StatusBadRequest && response.Code != http.StatusNotFound {
		t.Fatalf("unexpected traversal response: %d %s", response.Code, response.Body.String())
	}
}

func TestAdminRedirectSupportsGetAndHead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := httpserver.New(httpserver.Options{RegisterRoutes: assets.Register})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(method, "/admin?from=test", nil))
		if response.Code != http.StatusPermanentRedirect {
			t.Fatalf("%s /admin = %d", method, response.Code)
		}
		if location := response.Header().Get("Location"); location != "/admin/?from=test" {
			t.Fatalf("%s /admin location = %q", method, location)
		}
	}
}
