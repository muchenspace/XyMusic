package admintagscraping

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type artworkMutationFence interface {
	Lock(context.Context, pgx.Tx) error
}

// artistArtworkSuccessFence lets an artist batch persist its successful item
// inside the same transaction that makes the artwork visible.
type artistArtworkSuccessFence interface {
	CommitSuccess(context.Context, pgx.Tx, ArtistCandidate) error
}

type artworkMutationFenceContextKey struct{}
type artistArtworkDetailsContextKey struct{}

type artistArtworkDetails struct {
	reason    string
	candidate ArtistCandidate
}

func withArtworkMutationFence(ctx context.Context, fence artworkMutationFence) context.Context {
	if fence == nil {
		return ctx
	}
	return context.WithValue(ctx, artworkMutationFenceContextKey{}, fence)
}

func completionMutationFenceFromContext(ctx context.Context) artworkMutationFence {
	if ctx == nil {
		return nil
	}
	if fence, ok := ctx.Value(artworkMutationFenceContextKey{}).(artworkMutationFence); ok {
		return fence
	}
	return batchMutationFenceFromContext(ctx)
}

func withArtistArtworkDetails(
	ctx context.Context,
	reason string,
	candidate ArtistCandidate,
) context.Context {
	return context.WithValue(ctx, artistArtworkDetailsContextKey{}, artistArtworkDetails{
		reason: reason, candidate: candidate,
	})
}

func artistArtworkDetailsFromContext(ctx context.Context) artistArtworkDetails {
	if ctx == nil {
		return artistArtworkDetails{}
	}
	details, _ := ctx.Value(artistArtworkDetailsContextKey{}).(artistArtworkDetails)
	return details
}
