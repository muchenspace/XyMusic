package adminaudit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
)

func TestAuditQueriesConfiguredDatabase(t *testing.T) {
	path := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if path == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolute).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absolute))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service, _ := NewService(pool.Pool)
	for _, input := range []ListInput{{}, {Search: "admin", Sort: "action", Order: "asc"}, {Action: "admin.", Result: "SUCCESS", From: "2020-01-01", To: "2030-01-01"}} {
		if _, err := service.List(ctx, input); err != nil {
			t.Fatalf("List(%#v): %v", input, err)
		}
	}
}
