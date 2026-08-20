package admintagscraping

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"xymusic/server/internal/config"
	"xymusic/server/internal/platform/database"
	"xymusic/server/internal/testsupport"
)

func TestArtistArtworkBatchClaimAgainstConfiguredPostgres(t *testing.T) {
	environmentPath := os.Getenv("XYMUSIC_INTEGRATION_ENV")
	if environmentPath == "" {
		t.Skip("set XYMUSIC_INTEGRATION_ENV to run artist artwork batch claim checks")
	}
	testsupport.RequireWriteIntegration(t)
	absolutePath, err := filepath.Abs(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewStore(absolutePath).Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.ResolveRuntime(cfg, filepath.Dir(absolutePath))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	jobIDs := make([]string, 0, 2)
	artistIDs := make([]string, 0, 2)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, jobID := range jobIDs {
			_, _ = pool.Exec(cleanupCtx, "DELETE FROM artist_artwork_scraping_jobs WHERE id = $1", jobID)
		}
		for _, artistID := range artistIDs {
			_, _ = pool.Exec(cleanupCtx, "DELETE FROM artists WHERE id = $1", artistID)
		}
	})

	optionsJSON, err := json.Marshal(ArtistArtworkBatchOptions{
		Sources: []Source{SourceQMusic},
		Reason:  "artist artwork claim integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool.Pool)
	tests := []struct {
		name       string
		workerID   string
		batchClaim bool
		claim      func(context.Context, string, time.Time) (ArtistArtworkBatchClaimResult, error)
	}{
		{
			name:       "batch",
			workerID:   "artist-artwork-batch-integration-worker",
			batchClaim: true,
			claim: func(ctx context.Context, workerID string, now time.Time) (ArtistArtworkBatchClaimResult, error) {
				return repository.ClaimArtistArtworkBatchItems(ctx, workerID, now, time.Minute, 1)
			},
		},
		{
			name:     "single delegates to batch",
			workerID: "artist-artwork-single-integration-worker",
			claim: func(ctx context.Context, workerID string, now time.Time) (ArtistArtworkBatchClaimResult, error) {
				return repository.ClaimArtistArtworkBatchItem(ctx, workerID, now, time.Minute)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artistID := uuid.NewString()
			jobID := uuid.NewString()
			itemID := uuid.NewString()
			artistIDs = append(artistIDs, artistID)
			jobIDs = append(jobIDs, jobID)
			claimAt := time.Now().UTC()
			artistName := "Artist Artwork Claim " + artistID[:8]

			if _, err := pool.Exec(ctx, `
				INSERT INTO artists (id, name, normalized_name)
				VALUES ($1, $2, $3)`, artistID, artistName, normalizeLookup(artistName)); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO artist_artwork_scraping_jobs (
					id, options, total, created_at, updated_at
				) VALUES ($1, $2::jsonb, 1, $3, $4)`,
				jobID, optionsJSON, time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC), claimAt,
			); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO artist_artwork_scraping_job_items (
					id, job_id, artist_id, expected_version, position, next_attempt_at
				) VALUES ($1, $2, $3, 1, 0, $4)`, itemID, jobID, artistID, claimAt); err != nil {
				t.Fatal(err)
			}

			var pendingStatus string
			var pendingAttempts int
			if err := pool.QueryRow(ctx, `
				SELECT status::text, attempts
				FROM artist_artwork_scraping_job_items
				WHERE id = $1`, itemID).Scan(&pendingStatus, &pendingAttempts); err != nil {
				t.Fatal(err)
			}
			if pendingStatus != string(ItemPending) || pendingAttempts != 0 {
				t.Fatalf("initial item state = %s/%d, want PENDING/0", pendingStatus, pendingAttempts)
			}

			result, err := test.claim(ctx, test.workerID, claimAt)
			if err != nil {
				t.Fatal(err)
			}
			if result.FinishJobID != "" {
				t.Fatalf("finish job ID = %q, want an item claim", result.FinishJobID)
			}
			var claimed *ClaimedArtistArtworkBatchItem
			if test.batchClaim {
				if len(result.Items) != 1 {
					t.Fatalf("claimed items = %d, want 1", len(result.Items))
				}
				claimed = &result.Items[0]
			} else {
				if result.Item == nil {
					t.Fatal("single-item claim returned no item")
				}
				claimed = result.Item
			}
			if claimed.Item.ID != itemID || claimed.AttemptID == "" {
				t.Fatalf("claim item/attempt = %q/%q, want %q/non-empty", claimed.Item.ID, claimed.AttemptID, itemID)
			}
			if claimed.Item.AttemptID == nil || *claimed.Item.AttemptID != claimed.AttemptID {
				t.Fatalf("claim attempt fence = %#v/%q", claimed.Item.AttemptID, claimed.AttemptID)
			}

			var status string
			var attempts int
			var lockedBy string
			var attemptID string
			if err := pool.QueryRow(ctx, `
				SELECT status::text, attempts, locked_by, attempt_id::text
				FROM artist_artwork_scraping_job_items
				WHERE id = $1`, itemID).Scan(&status, &attempts, &lockedBy, &attemptID); err != nil {
				t.Fatal(err)
			}
			if status != string(ItemRunning) || attempts != 1 || lockedBy != test.workerID || attemptID != claimed.AttemptID {
				t.Fatalf(
					"claimed database state = %s/%d/%q/%q, want RUNNING/1/%q/%q",
					status, attempts, lockedBy, attemptID, test.workerID, claimed.AttemptID,
				)
			}
		})
	}
}
