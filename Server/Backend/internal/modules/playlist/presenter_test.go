package playlist

import (
	"context"
	"testing"
	"time"
)

func TestProductionUserPresenterBatchesUsersAndSignsReadyArtwork(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	assetID := "asset-1"
	checksum := "checksum"
	repository := &userProjectionStoreStub{
		users: []userProjection{{
			ID: "user-1", Username: "alice", DisplayName: "Alice", AvatarAssetID: &assetID,
		}},
		artworks: []artworkProjection{{
			ID: assetID, ObjectKey: "avatars/user-1.webp", MimeType: "image/webp",
			ChecksumSHA256: &checksum, UpdatedAt: now,
		}},
	}
	signer := &artworkSignerStub{url: "https://objects.test/avatar"}
	presenter, err := newProductionUserPresenter(repository, signer, 5*time.Minute, presenterClock{now})
	if err != nil {
		t.Fatal(err)
	}

	result, err := presenter.UserSummaries(context.Background(), []string{"user-1", "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	user := result["user-1"]
	if user.Avatar == nil || user.Avatar.URL != signer.url || user.Avatar.CacheKey != "asset-1:checksum" {
		t.Fatalf("UserSummaries() = %#v", result)
	}
	if len(repository.requestedUsers) != 1 || len(repository.requestedArtworks) != 1 {
		t.Fatalf("requested users/artworks = %#v/%#v", repository.requestedUsers, repository.requestedArtworks)
	}
	if signer.ttl != 5*time.Minute {
		t.Fatalf("signed TTL = %s", signer.ttl)
	}
}

type userProjectionStoreStub struct {
	users             []userProjection
	artworks          []artworkProjection
	requestedUsers    []string
	requestedArtworks []string
}

func (store *userProjectionStoreStub) Users(_ context.Context, ids []string) ([]userProjection, error) {
	store.requestedUsers = append([]string(nil), ids...)
	return store.users, nil
}

func (store *userProjectionStoreStub) Artworks(_ context.Context, ids []string) ([]artworkProjection, error) {
	store.requestedArtworks = append([]string(nil), ids...)
	return store.artworks, nil
}

type artworkSignerStub struct {
	url string
	ttl time.Duration
}

func (signer *artworkSignerStub) PresignedGet(_ context.Context, _ string, ttl time.Duration) (string, error) {
	signer.ttl = ttl
	return signer.url, nil
}

type presenterClock struct{ now time.Time }

func (clock presenterClock) Now() time.Time { return clock.now }
