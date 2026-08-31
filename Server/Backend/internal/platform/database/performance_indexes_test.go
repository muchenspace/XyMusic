package database

import (
	"context"
	"strings"
	"testing"
)

func TestLargeLibraryIndexStatementsAreIdempotentAndNonBlocking(t *testing.T) {
	if len(largeLibraryIndexStatements) < 10 {
		t.Fatalf("large-library index plan is unexpectedly small: %d", len(largeLibraryIndexStatements))
	}
	seen := make(map[string]struct{}, len(largeLibraryIndexStatements))
	for _, statement := range largeLibraryIndexStatements {
		if !strings.Contains(statement, "CREATE INDEX CONCURRENTLY IF NOT EXISTS") {
			t.Fatalf("index is not idempotent/concurrent: %s", statement)
		}
		name := strings.TrimPrefix(strings.SplitN(statement, " ON ", 2)[0], "CREATE INDEX CONCURRENTLY IF NOT EXISTS ")
		if _, exists := seen[name]; exists {
			t.Fatalf("duplicate performance index name: %s", name)
		}
		seen[name] = struct{}{}
	}
	if err := EnsureLargeLibraryIndexes(context.Background(), nil); err == nil {
		t.Fatal("nil database pool should be rejected")
	}
}
