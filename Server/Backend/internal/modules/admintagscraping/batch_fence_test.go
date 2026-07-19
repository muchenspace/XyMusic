package admintagscraping

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBatchMutationFenceLockUsesJobThenItemOrder(t *testing.T) {
	tx := &batchFenceTxStub{
		t: t,
		rows: []batchFenceRowStub{
			{
				queryContains: "FROM tag_scraping_jobs",
				arguments:     []any{"job-1"},
				scan: func(destinations ...any) error {
					setBatchFenceJobRow(t, destinations, string(JobRunning), false)
					return nil
				},
			},
			{
				queryContains: "FROM tag_scraping_job_items",
				arguments:     []any{"item-1", "job-1"},
				scan: func(destinations ...any) error {
					setBatchFenceItemRow(t, destinations, string(ItemRunning), "attempt-1", "worker-1", true)
					return nil
				},
			},
		},
	}

	err := (&BatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}).Lock(context.Background(), tx)
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	tx.assertComplete()
}

func TestBatchMutationFenceLockRejectsCancellationBeforeItemLock(t *testing.T) {
	tx := &batchFenceTxStub{
		t: t,
		rows: []batchFenceRowStub{{
			queryContains: "FROM tag_scraping_jobs",
			arguments:     []any{"job-1"},
			scan: func(destinations ...any) error {
				setBatchFenceJobRow(t, destinations, string(JobRunning), true)
				return nil
			},
		}},
	}

	err := (&BatchMutationFence{
		JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
	}).Lock(context.Background(), tx)
	if !errors.Is(err, errBatchCancellationRequested) {
		t.Fatalf("Lock() error = %v, want cancellation", err)
	}
	tx.assertComplete()
}

func TestBatchMutationFenceLockRejectsLostLease(t *testing.T) {
	tests := []struct {
		name           string
		jobStatus      JobStatus
		itemStatus     ItemStatus
		attemptID      string
		workerID       string
		leaseActive    bool
		expectItemLock bool
	}{
		{name: "inactive job", jobStatus: JobCompleted, expectItemLock: false},
		{name: "stale attempt", jobStatus: JobRunning, itemStatus: ItemRunning, attemptID: "attempt-2", workerID: "worker-1", leaseActive: true, expectItemLock: true},
		{name: "worker mismatch", jobStatus: JobRunning, itemStatus: ItemRunning, attemptID: "attempt-1", workerID: "worker-2", leaseActive: true, expectItemLock: true},
		{name: "expired lease", jobStatus: JobRunning, itemStatus: ItemRunning, attemptID: "attempt-1", workerID: "worker-1", leaseActive: false, expectItemLock: true},
		{name: "inactive item", jobStatus: JobRunning, itemStatus: ItemPending, attemptID: "attempt-1", workerID: "worker-1", leaseActive: true, expectItemLock: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := []batchFenceRowStub{{
				queryContains: "FROM tag_scraping_jobs",
				arguments:     []any{"job-1"},
				scan: func(destinations ...any) error {
					setBatchFenceJobRow(t, destinations, string(test.jobStatus), false)
					return nil
				},
			}}
			if test.expectItemLock {
				rows = append(rows, batchFenceRowStub{
					queryContains: "FROM tag_scraping_job_items",
					arguments:     []any{"item-1", "job-1"},
					scan: func(destinations ...any) error {
						setBatchFenceItemRow(t, destinations, string(test.itemStatus), test.attemptID, test.workerID, test.leaseActive)
						return nil
					},
				})
			}
			tx := &batchFenceTxStub{t: t, rows: rows}

			err := (&BatchMutationFence{
				JobID: "job-1", ItemID: "item-1", AttemptID: "attempt-1", WorkerID: "worker-1",
			}).Lock(context.Background(), tx)
			if !errors.Is(err, ErrBatchLeaseLost) {
				t.Fatalf("Lock() error = %v, want ErrBatchLeaseLost", err)
			}
			tx.assertComplete()
		})
	}
}

func TestBatchMutationFenceLockRejectsMissingOwnershipFields(t *testing.T) {
	tx := &batchFenceTxStub{t: t}
	err := (&BatchMutationFence{JobID: "job-1"}).Lock(context.Background(), tx)
	if !errors.Is(err, ErrBatchLeaseLost) {
		t.Fatalf("Lock() error = %v, want ErrBatchLeaseLost", err)
	}
	tx.assertComplete()
}

type batchFenceTxStub struct {
	t     *testing.T
	rows  []batchFenceRowStub
	index int
}

type batchFenceRowStub struct {
	queryContains string
	arguments     []any
	scan          func(...any) error
}

func (tx *batchFenceTxStub) QueryRow(_ context.Context, query string, arguments ...any) pgx.Row {
	tx.t.Helper()
	if tx.index >= len(tx.rows) {
		tx.t.Fatalf("unexpected QueryRow(%q, %#v)", compactBatchFenceSQL(query), arguments)
	}
	expected := tx.rows[tx.index]
	tx.index++
	if !strings.Contains(query, expected.queryContains) {
		tx.t.Fatalf("QueryRow[%d] query = %q, want containing %q", tx.index-1, compactBatchFenceSQL(query), expected.queryContains)
	}
	if !reflect.DeepEqual(arguments, expected.arguments) {
		tx.t.Fatalf("QueryRow[%d] arguments = %#v, want %#v", tx.index-1, arguments, expected.arguments)
	}
	return expected
}

func (tx *batchFenceTxStub) assertComplete() {
	tx.t.Helper()
	if tx.index != len(tx.rows) {
		tx.t.Fatalf("consumed %d QueryRow calls, want %d", tx.index, len(tx.rows))
	}
}

func (row batchFenceRowStub) Scan(destinations ...any) error {
	return row.scan(destinations...)
}

func setBatchFenceJobRow(t *testing.T, destinations []any, status string, cancelled bool) {
	t.Helper()
	if len(destinations) != 2 {
		t.Fatalf("job Scan destinations = %d, want 2", len(destinations))
	}
	*(destinations[0].(*string)) = status
	*(destinations[1].(*bool)) = cancelled
}

func setBatchFenceItemRow(t *testing.T, destinations []any, status, attemptID, workerID string, leaseActive bool) {
	t.Helper()
	if len(destinations) != 4 {
		t.Fatalf("item Scan destinations = %d, want 4", len(destinations))
	}
	*(destinations[0].(*string)) = status
	*(destinations[1].(**string)) = &attemptID
	*(destinations[2].(**string)) = &workerID
	*(destinations[3].(*bool)) = leaseActive
}

func compactBatchFenceSQL(query string) string {
	return strings.Join(strings.Fields(query), " ")
}

func (*batchFenceTxStub) Begin(context.Context) (pgx.Tx, error) { panic("unexpected Begin") }
func (*batchFenceTxStub) Commit(context.Context) error          { panic("unexpected Commit") }
func (*batchFenceTxStub) Rollback(context.Context) error        { panic("unexpected Rollback") }
func (*batchFenceTxStub) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	panic("unexpected CopyFrom")
}
func (*batchFenceTxStub) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("unexpected SendBatch")
}
func (*batchFenceTxStub) LargeObjects() pgx.LargeObjects { panic("unexpected LargeObjects") }
func (*batchFenceTxStub) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	panic("unexpected Prepare")
}
func (*batchFenceTxStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}
func (*batchFenceTxStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}
func (*batchFenceTxStub) Conn() *pgx.Conn { return nil }
