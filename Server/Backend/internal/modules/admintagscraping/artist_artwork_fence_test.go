package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"xymusic/server/internal/shared/apperror"
)

func TestArtistArtworkCompletionFenceLocksOwnershipAndAuditsAtomically(t *testing.T) {
	auditWritten := false
	mutation := &artistMutationFenceStub{commit: func(candidate ArtistCandidate) error {
		if !auditWritten {
			t.Fatal("batch success was recorded before the scrape audit")
		}
		if candidate.Source != SourceQMusic || candidate.ID != "qq-artist" {
			t.Fatalf("committed candidate = %#v", candidate)
		}
		return nil
	}}
	tx := &artistFenceTxStub{
		batchFenceTxStub: &batchFenceTxStub{t: t, rows: []batchFenceRowStub{{
			queryContains: "FROM artists",
			arguments:     []any{"artist-1"},
			scan: func(destinations ...any) error {
				if !mutation.called {
					t.Fatal("artist row was locked before batch ownership")
				}
				*(destinations[0].(*int)) = 3
				*(destinations[1].(**string)) = nil
				return nil
			},
		}}},
		t: t,
		exec: func(query string, arguments ...any) {
			if !strings.Contains(query, "ARTIST_ARTWORK_SCRAPED") {
				t.Fatalf("audit query = %q", compactBatchFenceSQL(query))
			}
			if arguments[0] != "admin-1" || arguments[1] != "artist-1" || arguments[2] != "trace-1" {
				t.Fatalf("audit arguments = %#v", arguments)
			}
			var details map[string]any
			if err := json.Unmarshal(arguments[3].([]byte), &details); err != nil {
				t.Fatal(err)
			}
			expected := map[string]any{
				"provider": string(SourceQMusic), "externalId": "qq-artist",
				"reason": "operator scrape", "overwrite": false,
			}
			if !reflect.DeepEqual(details, expected) {
				t.Fatalf("audit details = %#v", details)
			}
			auditWritten = true
		},
	}
	fence := &artistArtworkCompletionFence{
		mutationFence: mutation, actorID: "admin-1", traceID: "trace-1", artistID: "artist-1",
		expectedVersion: 3, reason: "operator scrape",
		candidate: ArtistCandidate{Source: SourceQMusic, ID: "qq-artist"},
	}
	if err := fence.Lock(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if tx.execCalls != 1 {
		t.Fatalf("audit calls = %d", tx.execCalls)
	}
	if !mutation.successCalled {
		t.Fatal("artist batch success was not recorded in the completion transaction")
	}
	tx.assertComplete()
}

func TestArtistArtworkCompletionFenceRejectsStaleVersionAndMissingOnlyOverwrite(t *testing.T) {
	assetID := "asset-1"
	tests := []struct {
		name      string
		version   int
		artwork   *string
		overwrite bool
		expected  apperror.Code
	}{
		{name: "stale version", version: 4, expected: apperror.CodeVersionConflict},
		{name: "existing artwork", version: 3, artwork: &assetID, expected: apperror.CodeResourceConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &artistFenceTxStub{
				batchFenceTxStub: &batchFenceTxStub{t: t, rows: []batchFenceRowStub{{
					queryContains: "FROM artists", arguments: []any{"artist-1"},
					scan: func(destinations ...any) error {
						*(destinations[0].(*int)) = test.version
						*(destinations[1].(**string)) = test.artwork
						return nil
					},
				}}},
				t: t,
			}
			fence := &artistArtworkCompletionFence{
				actorID: "admin-1", traceID: "trace-1", artistID: "artist-1",
				expectedVersion: 3, overwrite: test.overwrite, reason: "operator scrape",
				candidate: ArtistCandidate{Source: SourceQMusic, ID: "qq-artist"},
			}
			err := fence.Lock(context.Background(), tx)
			if !apperror.IsCode(err, test.expected) || tx.execCalls != 0 {
				t.Fatalf("error/audits = %v / %d", err, tx.execCalls)
			}
			tx.assertComplete()
		})
	}
}

func TestArtistArtworkCompletionFenceStopsBeforeArtistLockWhenOwnershipIsLost(t *testing.T) {
	tx := &artistFenceTxStub{batchFenceTxStub: &batchFenceTxStub{t: t}, t: t}
	fence := &artistArtworkCompletionFence{
		mutationFence: &artistMutationFenceStub{err: ErrBatchLeaseLost},
		artistID:      "artist-1", expectedVersion: 1,
	}
	if err := fence.Lock(context.Background(), tx); !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("error = %v", err)
	}
	tx.assertComplete()
}

type artistMutationFenceStub struct {
	called        bool
	successCalled bool
	err           error
	commit        func(ArtistCandidate) error
}

func (fence *artistMutationFenceStub) Lock(context.Context, pgx.Tx) error {
	fence.called = true
	return fence.err
}

func (fence *artistMutationFenceStub) CommitSuccess(
	_ context.Context,
	_ pgx.Tx,
	candidate ArtistCandidate,
) error {
	fence.successCalled = true
	if fence.commit != nil {
		return fence.commit(candidate)
	}
	return nil
}

type artistFenceTxStub struct {
	*batchFenceTxStub
	t         *testing.T
	exec      func(string, ...any)
	execCalls int
}

func (tx *artistFenceTxStub) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	tx.t.Helper()
	tx.execCalls++
	if tx.exec == nil {
		tx.t.Fatalf("unexpected Exec(%q, %#v)", compactBatchFenceSQL(query), arguments)
	}
	tx.exec(query, arguments...)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
