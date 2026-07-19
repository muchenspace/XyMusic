package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/app"
	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	platformsecurity "xymusic/server/internal/platform/security"
	"xymusic/server/internal/platform/storage"
)

func TestProductionDependenciesAreCompatible(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production dependency probes")
	}
	absoluteEnvironmentPath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absoluteEnvironmentPath).Load()
	if err != nil {
		t.Fatalf("load production configuration: %v", err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absoluteEnvironmentPath))
	if err != nil {
		t.Fatalf("resolve production configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatalf("PostgreSQL probe failed: %v", err)
	}
	defer pool.Close()

	migrations := filepath.Join(projectRoot(t), "migrations")
	if err := database.CheckMigrationCompatibility(ctx, pool.Pool, migrations); err != nil {
		t.Fatalf("PostgreSQL migration compatibility failed: %v", err)
	}
	rows, err := pool.Query(ctx, "select password_hash from users limit 10")
	if err != nil {
		t.Fatalf("read password hash formats: %v", err)
	}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			rows.Close()
			t.Fatalf("scan password hash format: %v", err)
		}
		if _, err := platformsecurity.VerifyPassword("xymusic-integration-invalid-password", encoded); err != nil {
			rows.Close()
			parts := strings.Split(encoded, "$")
			format := parts[0]
			if len(parts) > 1 {
				format = parts[1]
			}
			t.Fatalf("Go cannot parse an existing Bun password hash (format=%q, segments=%d, length=%d): %v", format, len(parts), len(encoded), err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("read password hash formats: %v", err)
	}
	rows.Close()
	payloadCipher, err := platformsecurity.NewPayloadCipher(cfg.Security.IdempotencyEncryptionSecret)
	if err != nil {
		t.Fatalf("initialize idempotency compatibility cipher: %v", err)
	}
	var encryptedResponse *string
	err = pool.QueryRow(ctx, `
		select encrypted_response
		from idempotency_records
		where encrypted_response is not null
		order by created_at desc
		limit 1`).Scan(&encryptedResponse)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("read idempotency response format: %v", err)
	}
	if err == nil && encryptedResponse != nil {
		var protectedPayload any
		if err := payloadCipher.Decrypt(*encryptedResponse, &protectedPayload); err != nil {
			t.Fatalf("Go cannot decrypt an existing Bun idempotency response: %v", err)
		}
	}
	objects, err := storage.Open(cfg.Storage)
	if err != nil {
		t.Fatalf("MinIO/S3 client initialization failed: %v", err)
	}
	if err := objects.Ping(ctx); err != nil {
		t.Fatalf("MinIO/S3 read-only probe failed: %v", err)
	}
}

func TestGoRuntimeHealthWithProductionDependencies(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run production runtime probes")
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
	runtime, err := app.Bootstrap(ctx, cfg, app.Options{RootDirectory: filepath.Dir(absoluteEnvironmentPath)})
	if err != nil {
		t.Fatalf("bootstrap Go runtime: %v", err)
	}
	defer runtime.Close()
	if runtime.Metrics == nil {
		t.Fatal("production runtime did not compose request metrics")
	}
	metricsBefore := runtime.Metrics.Snapshot().Requests.Total
	registered := make(map[string]bool)
	for _, route := range runtime.Handler.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"POST /api/v1/auth/register",
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/refresh",
		"POST /api/v1/auth/logout",
		"POST /api/v1/auth/logout-all",
	} {
		if !registered[route] {
			t.Fatalf("Go runtime did not register %s", route)
		}
	}

	for _, endpoint := range []string{"/health/live", "/health/ready", "/admin/"} {
		request := httptest.NewRequest(http.MethodGet, endpoint, nil)
		response := httptest.NewRecorder()
		runtime.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", endpoint, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Trace-Id") == "" {
			t.Fatalf("%s did not return a trace id", endpoint)
		}
	}
	metricsAfter := runtime.Metrics.Snapshot().Requests
	if metricsAfter.Total != metricsBefore+3 || metricsAfter.InFlight != 0 {
		t.Fatalf("production runtime metrics after probes = %+v, before total=%d", metricsAfter, metricsBefore)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
