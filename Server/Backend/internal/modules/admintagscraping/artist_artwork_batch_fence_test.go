package admintagscraping

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestArtistArtworkBatchMutationFenceCommitsSuccessAtomically(t *testing.T) {
	tx := &artistArtworkBatchFenceTxStub{
		batchFenceTxStub: &batchFenceTxStub{
			t: t,
			rows: []batchFenceRowStub{
				{
					queryContains: "FROM artist_artwork_scraping_jobs",
					arguments:     []any{"job-1"},
					scan: func(destinations ...any) error {
						setBatchFenceJobRow(t, destinations, string(JobRunning), false)
						return nil
					},
				},
				{
					queryContains: "FROM artist_artwork_scraping_job_items",
					arguments:     []any{"item-1", "job-1"},
					scan: func(destinations ...any) error {
						setBatchFenceItemRow(t, destinations, string(ItemRunning), "attempt-1", "worker-1", true)
						return nil
					},
				},
			},
		},
		execs: []artistArtworkBatchFenceExecStub{
			{
				queryContains: "UPDATE artist_artwork_scraping_job_items",
				rowsAffected:  1,
				check: func(arguments ...any) {
					expectedPrefix := []any{"item-1", "job-1", "attempt-1", "worker-1"}
					if len(arguments) != 6 || !reflect.DeepEqual(arguments[:4], expectedPrefix) {
						t.Fatalf("item completion arguments = %#v", arguments)
					}
					var candidate ArtistCandidate
					if err := json.Unmarshal(arguments[4].([]byte), &candidate); err != nil {
						t.Fatal(err)
					}
					if candidate.Source != SourceQMusic || candidate.ID != "qq-artist" {
						t.Fatalf("stored candidate = %#v", candidate)
					}
					source, ok := arguments[5].(*string)
					if !ok || source == nil || *source != string(SourceQMusic) {
						t.Fatalf("stored source = %#v", arguments[5])
					}
				},
			},
			{
				queryContains: "UPDATE artist_artwork_scraping_jobs",
				rowsAffected:  1,
				check: func(arguments ...any) {
					if !reflect.DeepEqual(arguments, []any{"job-1"}) {
						t.Fatalf("job count arguments = %#v", arguments)
					}
				},
			},
		},
	}
	fence := &ArtistArtworkBatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}
	if err := fence.Lock(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := fence.CommitSuccess(context.Background(), tx, ArtistCandidate{
		Source: SourceQMusic, ID: "qq-artist", Name: "Artist",
		ImageURL: "https://y.qq.com/avatar.jpg", Aliases: []string{}, Score: 2,
	}); err != nil {
		t.Fatal(err)
	}
	tx.assertComplete()
}

func TestArtistArtworkBatchMutationFenceRejectsLostOwnershipWhileCommitting(t *testing.T) {
	tx := &artistArtworkBatchFenceTxStub{
		batchFenceTxStub: &batchFenceTxStub{t: t},
		execs: []artistArtworkBatchFenceExecStub{{
			queryContains: "UPDATE artist_artwork_scraping_job_items",
			rowsAffected:  0,
		}},
	}
	err := (&ArtistArtworkBatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}).CommitSuccess(context.Background(), tx, ArtistCandidate{
		Source: SourceQMusic, ID: "qq-artist", Name: "Artist",
		ImageURL: "https://y.qq.com/avatar.jpg", Aliases: []string{}, Score: 2,
	})
	if !errors.Is(err, ErrArtistArtworkBatchLeaseLost) {
		t.Fatalf("CommitSuccess() error = %v", err)
	}
	tx.assertComplete()
}

type artistArtworkBatchFenceExecStub struct {
	queryContains string
	rowsAffected  int64
	check         func(...any)
}

type artistArtworkBatchFenceTxStub struct {
	*batchFenceTxStub
	execs     []artistArtworkBatchFenceExecStub
	execIndex int
}

func (tx *artistArtworkBatchFenceTxStub) Exec(
	_ context.Context,
	query string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	tx.t.Helper()
	if tx.execIndex >= len(tx.execs) {
		tx.t.Fatalf("unexpected Exec(%q, %#v)", compactBatchFenceSQL(query), arguments)
	}
	expected := tx.execs[tx.execIndex]
	tx.execIndex++
	if !strings.Contains(query, expected.queryContains) {
		tx.t.Fatalf("Exec[%d] query = %q, want containing %q", tx.execIndex-1, compactBatchFenceSQL(query), expected.queryContains)
	}
	if expected.check != nil {
		expected.check(arguments...)
	}
	return pgconn.NewCommandTag("UPDATE " + string(rune('0'+expected.rowsAffected))), nil
}

func (tx *artistArtworkBatchFenceTxStub) assertComplete() {
	tx.batchFenceTxStub.assertComplete()
	if tx.execIndex != len(tx.execs) {
		tx.t.Fatalf("consumed %d Exec calls, want %d", tx.execIndex, len(tx.execs))
	}
}
