package database

import (
	"strings"
	"testing"
)

func TestLargeLibraryIndexDeclarationsAreConsistentAndDeduplicated(t *testing.T) {
	seen := make(map[string]struct{}, len(largeLibraryIndexes))
	for _, index := range largeLibraryIndexes {
		if index.name == "" {
			t.Fatal("managed index name cannot be empty")
		}
		if _, duplicate := seen[index.name]; duplicate {
			t.Fatalf("duplicate managed index name: %s", index.name)
		}
		seen[index.name] = struct{}{}

		expectedPrefix := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + index.name + " ON "
		if !strings.HasPrefix(index.statement, expectedPrefix) {
			t.Fatalf("statement for %s must start with %q, got %q", index.name, expectedPrefix, index.statement)
		}
	}
	for _, obsolete := range obsoleteDuplicateIndexes {
		if _, stillPresent := seen[obsolete]; stillPresent {
			t.Fatalf("obsolete duplicate index %s is still declared in largeLibraryIndexes", obsolete)
		}
	}
}
