package adminsources

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (synchronizer *ProductionSynchronizer) ProcessPreparedFileBatch(
	ctx context.Context,
	rootID string,
	scanRunID string,
	batch []PreparedScanBatchFile,
	seenAt time.Time,
) []error {
	result := make([]error, len(batch))
	if len(batch) == 0 {
		return result
	}

	// Only the stable, non-reusable standard-file path is safe to place in a
	// shared transaction. Reusable files may need their own sidecar transaction
	// and a file that changed after preparation must fall back to the normal
	// path. Keeping this guard cheap preserves the per-file error contract.
	preparedFiles := make([]*preparedStandardFile, len(batch))
	for index, item := range batch {
		prepared, ok := item.Prepared.(*preparedStandardFile)
		if !ok || prepared == nil || prepared.UnchangedReady {
			return synchronizer.processPreparedFilesIndividually(ctx, rootID, scanRunID, batch, seenAt)
		}
		stable, err := synchronizer.preparedFileStillStable(item.File, prepared)
		if err != nil || !stable {
			return synchronizer.processPreparedFilesIndividually(ctx, rootID, scanRunID, batch, seenAt)
		}
		preparedFiles[index] = prepared
	}

	transaction, err := synchronizer.database.Begin(ctx)
	if err != nil {
		return synchronizer.processPreparedFilesIndividually(ctx, rootID, scanRunID, batch, seenAt)
	}
	defer transaction.Rollback(context.WithoutCancel(ctx))
	batchCatalog := newScanCatalogCache()
	batchContext := withPreparedStabilityChecked(withScanBatchCatalog(withScanTransaction(ctx, transaction), batchCatalog))

	for index, item := range batch {
		if err := ctx.Err(); err != nil {
			return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, err)
		}
		// A savepoint isolates one bad file while allowing the remaining files
		// to share the transaction and its catalog/index work.
		savepoint := fmt.Sprintf("xymusic_scan_file_%d", index)
		if _, err := transaction.Exec(batchContext, "SAVEPOINT "+savepoint); err != nil {
			batchErr := fmt.Errorf("create prepared local library scan savepoint: %w", err)
			return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
		}
		result[index] = synchronizer.ProcessPreparedFile(
			batchContext, rootID, scanRunID, item.File, seenAt, preparedFiles[index],
		)
		if result[index] != nil {
			if rollbackErr := rollbackScanSavepoint(batchContext, transaction, savepoint); rollbackErr != nil {
				batchErr := errors.Join(result[index], rollbackErr)
				return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
			}
			continue
		}
		if _, releaseErr := transaction.Exec(batchContext, "RELEASE SAVEPOINT "+savepoint); releaseErr != nil {
			result[index] = releaseErr
			if rollbackErr := rollbackScanSavepoint(batchContext, transaction, savepoint); rollbackErr != nil {
				batchErr := errors.Join(result[index], rollbackErr)
				return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
			}
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		batchErr := fmt.Errorf("commit prepared local library scan batch: %w", err)
		return synchronizer.abortPreparedFileBatch(ctx, rootID, batch, seenAt, transaction, result, batchErr)
	}
	if snapshot := sourceScanSnapshotFromContext(ctx); snapshot != nil {
		snapshot.rememberCatalog(batchCatalog)
	}
	for index, item := range batch {
		if result[index] != nil && !errors.Is(result[index], context.Canceled) && !errors.Is(result[index], ErrScanCancelled) {
			result[index] = synchronizer.finishPreparedFileError(ctx, rootID, item.File, seenAt, result[index])
		}
	}
	return result
}

func (synchronizer *ProductionSynchronizer) abortPreparedFileBatch(
	ctx context.Context,
	rootID string,
	batch []PreparedScanBatchFile,
	seenAt time.Time,
	transaction pgx.Tx,
	result []error,
	batchErr error,
) []error {
	fillUncommittedBatchErrors(result, batchErr)
	cleanupContext := context.WithoutCancel(ctx)
	_ = transaction.Rollback(cleanupContext)
	for index, item := range batch {
		if result[index] == nil || errors.Is(result[index], context.Canceled) || errors.Is(result[index], ErrScanCancelled) {
			continue
		}
		result[index] = synchronizer.finishPreparedFileError(cleanupContext, rootID, item.File, seenAt, result[index])
	}
	return result
}

func fillUncommittedBatchErrors(result []error, err error) {
	for index := range result {
		if result[index] == nil {
			result[index] = err
		}
	}
}

func rollbackScanSavepoint(ctx context.Context, transaction pgx.Tx, savepoint string) error {
	cleanupContext := context.WithoutCancel(ctx)
	if _, err := transaction.Exec(cleanupContext, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		return err
	}
	_, err := transaction.Exec(cleanupContext, "RELEASE SAVEPOINT "+savepoint)
	return err
}

func (synchronizer *ProductionSynchronizer) processPreparedFilesIndividually(
	ctx context.Context,
	rootID string,
	scanRunID string,
	batch []PreparedScanBatchFile,
	seenAt time.Time,
) []error {
	result := make([]error, len(batch))
	for index, item := range batch {
		result[index] = synchronizer.ProcessPreparedFile(ctx, rootID, scanRunID, item.File, seenAt, item.Prepared)
	}
	return result
}
