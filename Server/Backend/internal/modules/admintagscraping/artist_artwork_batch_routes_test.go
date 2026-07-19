package admintagscraping

import (
	"context"
	"time"
)

type artistArtworkBatchAPIStub struct {
	create func(context.Context, string, CreateArtistArtworkBatchInput) (ArtistArtworkBatchCreateResult, error)
	job    func(context.Context, string, *time.Time) (ArtistArtworkBatchJobDTO, error)
	cancel func(context.Context, string) (ArtistArtworkBatchJobDTO, error)
	retry  func(context.Context, string) (ArtistArtworkBatchJobDTO, error)
	createCalls int
	jobCalls int
	cancelCalls int
	retryCalls int
	updatedAfter *time.Time
}

func (stub *artistArtworkBatchAPIStub) Create(ctx context.Context, actorID string, input CreateArtistArtworkBatchInput) (ArtistArtworkBatchCreateResult, error) {
	stub.createCalls++
	if stub.create != nil {
		return stub.create(ctx, actorID, input)
	}
	return ArtistArtworkBatchCreateResult{}, nil
}

func (stub *artistArtworkBatchAPIStub) Job(ctx context.Context, jobID string, updatedAfter *time.Time) (ArtistArtworkBatchJobDTO, error) {
	stub.jobCalls++
	stub.updatedAfter = updatedAfter
	if stub.job != nil {
		return stub.job(ctx, jobID, updatedAfter)
	}
	return ArtistArtworkBatchJobDTO{}, nil
}

func (stub *artistArtworkBatchAPIStub) Cancel(ctx context.Context, jobID string) (ArtistArtworkBatchJobDTO, error) {
	stub.cancelCalls++
	if stub.cancel != nil {
		return stub.cancel(ctx, jobID)
	}
	return ArtistArtworkBatchJobDTO{}, nil
}

func (stub *artistArtworkBatchAPIStub) Retry(ctx context.Context, jobID string) (ArtistArtworkBatchJobDTO, error) {
	stub.retryCalls++
	if stub.retry != nil {
		return stub.retry(ctx, jobID)
	}
	return ArtistArtworkBatchJobDTO{}, nil
}
