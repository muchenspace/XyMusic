package admintagscraping

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"xymusic/server/internal/shared/apperror"
)

func TestArtistArtworkCompletionFenceLocksOwnershipAtomically(t *testing.T) {
	mutation := &artistMutationFenceStub{commit: func(candidate ArtistCandidate) error {
		if candidate.Source != SourceQMusic || candidate.ID != "qq-artist" {
			t.Fatalf("committed candidate = %#v", candidate)
		}
		return nil
	}}
	tx := &batchFenceTxStub{t: t, rows: []batchFenceRowStub{{
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
	}}}
	fence := &artistArtworkCompletionFence{
		mutationFence: mutation, artistID: "artist-1",
		expectedVersion: 3, reason: "operator scrape",
		candidate: ArtistCandidate{Source: SourceQMusic, ID: "qq-artist"},
	}
	if err := fence.Lock(context.Background(), tx); err != nil {
		t.Fatal(err)
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
			tx := &batchFenceTxStub{t: t, rows: []batchFenceRowStub{{
				queryContains: "FROM artists", arguments: []any{"artist-1"},
				scan: func(destinations ...any) error {
					*(destinations[0].(*int)) = test.version
					*(destinations[1].(**string)) = test.artwork
					return nil
				},
			}}}
			fence := &artistArtworkCompletionFence{
				artistID: "artist-1", expectedVersion: 3, overwrite: test.overwrite,
				reason: "operator scrape", candidate: ArtistCandidate{Source: SourceQMusic, ID: "qq-artist"},
			}
			err := fence.Lock(context.Background(), tx)
			if !apperror.IsCode(err, test.expected) {
				t.Fatalf("error = %v", err)
			}
			tx.assertComplete()
		})
	}
}

func TestArtistArtworkCompletionFenceStopsBeforeArtistLockWhenOwnershipIsLost(t *testing.T) {
	tx := &batchFenceTxStub{t: t}
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
