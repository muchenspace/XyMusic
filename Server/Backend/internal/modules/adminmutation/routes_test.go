package adminmutation

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xymusic/server/internal/modules/identity"
	"xymusic/server/internal/platform/httpserver"
	"xymusic/server/internal/shared/apperror"
	sharedidempotency "xymusic/server/internal/shared/idempotency"
)

func TestAdminMutationRoutesExecuteReplayAndRejectPayloadConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		artistID       = "00000000-0000-4000-8000-000000000001"
		albumID        = "00000000-0000-4000-8000-000000000002"
		sourceAlbumID  = "00000000-0000-4000-8000-000000000003"
		trackID        = "00000000-0000-4000-8000-000000000004"
		userID         = "00000000-0000-4000-8000-000000000005"
		creditArtistID = "00000000-0000-4000-8000-000000000006"
		secondTrackID  = "00000000-0000-4000-8000-000000000007"
	)
	tests := []struct {
		name             string
		method           string
		path             string
		scope            string
		status           int
		initialBody      string
		replayBody       string
		conflictBody     string
		responseContains string
		assertCall       func(*testing.T, mutationCall)
	}{
		{
			name: "create artist", method: http.MethodPost, path: "/api/v1/admin/artists",
			scope: "admin.artist.create", status: http.StatusCreated,
			initialBody:      `{"name":"Artist","description":"Biography","unknown":"first"}`,
			replayBody:       `{"name":"Artist","description":"Biography","unknown":"second","another":true}`,
			conflictBody:     `{"name":"Other Artist","description":"Biography","unknown":"second"}`,
			responseContains: `"id":"artist-created"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(CreateArtistInput)
				if call.operation != "createArtist" || input.Name != "Artist" || !input.Description.Set || input.Description.Value == nil || *input.Description.Value != "Biography" {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "update artist", method: http.MethodPatch, path: "/api/v1/admin/artists/" + artistID,
			scope: "admin.artist.update:" + artistID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"name":"Renamed Artist","description":null,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"name":"Renamed Artist","description":null,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":2,"name":"Different Artist","description":null,"unknown":"second"}`,
			responseContains: `"id":"` + artistID + `"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(UpdateArtistInput)
				if call.operation != "updateArtist" || call.id != artistID || input.ExpectedVersion != 2 || !input.Name.Set || input.Name.Value != "Renamed Artist" || !input.Description.Set || input.Description.Value != nil {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "create album", method: http.MethodPost, path: "/api/v1/admin/albums",
			scope: "admin.album.create", status: http.StatusCreated,
			initialBody:      `{"title":"Album","artistCredits":[{"artistId":"` + creditArtistID + `","role":"PRIMARY","sortOrder":0}],"releaseDate":"2026-07-16","description":null,"unknown":"first"}`,
			replayBody:       `{"title":"Album","artistCredits":[{"artistId":"` + creditArtistID + `","role":"PRIMARY","sortOrder":0}],"releaseDate":"2026-07-16","description":null,"unknown":"second","another":true}`,
			conflictBody:     `{"title":"Other Album","artistCredits":[{"artistId":"` + creditArtistID + `","role":"PRIMARY","sortOrder":0}],"releaseDate":"2026-07-16","description":null,"unknown":"second"}`,
			responseContains: `"id":"album-created"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(CreateAlbumInput)
				if call.operation != "createAlbum" || input.Title != "Album" || len(input.ArtistCredits) != 1 || input.ArtistCredits[0].ArtistID != creditArtistID || input.ArtistCredits[0].Role != CreditPrimary || input.ArtistCredits[0].SortOrder != 0 || !input.ArtistCredits[0].SortOrderSet || !input.ReleaseDate.Set || input.ReleaseDate.Value == nil || *input.ReleaseDate.Value != "2026-07-16" || !input.Description.Set || input.Description.Value != nil {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "update album", method: http.MethodPatch, path: "/api/v1/admin/albums/" + albumID,
			scope: "admin.album.update:" + albumID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"title":"Updated Album","releaseDate":null,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"title":"Updated Album","releaseDate":null,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":2,"title":"Different Album","releaseDate":null,"unknown":"second"}`,
			responseContains: `"id":"` + albumID + `"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(UpdateAlbumInput)
				if call.operation != "updateAlbum" || call.id != albumID || input.ExpectedVersion != 2 || !input.Title.Set || input.Title.Value != "Updated Album" || !input.ReleaseDate.Set || input.ReleaseDate.Value != nil {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "merge albums", method: http.MethodPost, path: "/api/v1/admin/albums/merge",
			scope: "admin.album.merge:" + albumID, status: http.StatusOK,
			initialBody:      `{"target":{"albumId":"` + albumID + `","expectedVersion":2},"sources":[{"albumId":"` + sourceAlbumID + `","expectedVersion":3}],"fieldSources":{"title":"` + albumID + `","cover":null,"artistCredits":"` + sourceAlbumID + `","releaseDate":null,"description":null},"unknown":"first"}`,
			replayBody:       `{"target":{"albumId":"` + albumID + `","expectedVersion":2},"sources":[{"albumId":"` + sourceAlbumID + `","expectedVersion":3}],"fieldSources":{"title":"` + albumID + `","cover":null,"artistCredits":"` + sourceAlbumID + `","releaseDate":null,"description":null},"unknown":"second","another":true}`,
			conflictBody:     `{"target":{"albumId":"` + albumID + `","expectedVersion":2},"sources":[{"albumId":"` + sourceAlbumID + `","expectedVersion":4}],"fieldSources":{"title":"` + albumID + `","cover":null,"artistCredits":"` + sourceAlbumID + `","releaseDate":null,"description":null},"unknown":"second"}`,
			responseContains: `"movedTracks":4`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(MergeAlbumsInput)
				if call.operation != "mergeAlbums" || input.Target.AlbumID != albumID || input.Target.ExpectedVersion != 2 || len(input.Sources) != 1 || input.Sources[0].AlbumID != sourceAlbumID || input.Sources[0].ExpectedVersion != 3 || input.FieldSources.Title != albumID || input.FieldSources.ArtistCredits != sourceAlbumID || !input.FieldSources.Cover.Set || input.FieldSources.Cover.Value != nil {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "create track", method: http.MethodPost, path: "/api/v1/admin/tracks",
			scope: "admin.track.create", status: http.StatusCreated,
			initialBody:      `{"title":"Track","albumId":"` + albumID + `","artistCredits":[{"artistId":"` + creditArtistID + `","role":"PRIMARY","sortOrder":0}],"trackNumber":7,"unknown":"first"}`,
			replayBody:       `{"title":"Track","albumId":"` + albumID + `","artistCredits":[{"artistId":"` + creditArtistID + `","role":"PRIMARY","sortOrder":0}],"trackNumber":7,"unknown":"second","another":true}`,
			conflictBody:     `{"title":"Other Track","albumId":"` + albumID + `","artistCredits":[{"artistId":"` + creditArtistID + `","role":"PRIMARY","sortOrder":0}],"trackNumber":7,"unknown":"second"}`,
			responseContains: `"id":"track-created"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(CreateTrackInput)
				if call.operation != "createTrack" || input.Title != "Track" || !input.AlbumID.Set || input.AlbumID.Value == nil || *input.AlbumID.Value != albumID || len(input.ArtistCredits) != 1 || !input.TrackNumber.Set || input.TrackNumber.Value == nil || *input.TrackNumber.Value != 7 || !input.DiscNumber.Set || input.DiscNumber.Value != 1 {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "update track", method: http.MethodPatch, path: "/api/v1/admin/tracks/" + trackID,
			scope: "admin.track.update:" + trackID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"title":"Updated Track","albumId":null,"trackNumber":null,"discNumber":2,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"title":"Updated Track","albumId":null,"trackNumber":null,"discNumber":2,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":2,"title":"Different Track","albumId":null,"trackNumber":null,"discNumber":2,"unknown":"second"}`,
			responseContains: `"id":"` + trackID + `"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(UpdateTrackInput)
				if call.operation != "updateTrack" || call.id != trackID || input.ExpectedVersion != 2 || !input.Title.Set || input.Title.Value != "Updated Track" || !input.AlbumID.Set || input.AlbumID.Value != nil || !input.TrackNumber.Set || input.TrackNumber.Value != nil || !input.DiscNumber.Set || input.DiscNumber.Value != 2 {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "publish track", method: http.MethodPost, path: "/api/v1/admin/tracks/" + trackID + "/publish",
			scope: "admin.track.publish:" + trackID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":3,"unknown":"second"}`,
			responseContains: `"status":"READY"`,
			assertCall: func(t *testing.T, call mutationCall) {
				if call.operation != "publishTrack" || call.id != trackID || call.version != 2 {
					t.Fatalf("call=%+v", call)
				}
			},
		},
		{
			name: "archive track", method: http.MethodPost, path: "/api/v1/admin/tracks/" + trackID + "/archive",
			scope: "admin.track.archive:" + trackID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":3,"unknown":"second"}`,
			responseContains: `"status":"ARCHIVED"`,
			assertCall: func(t *testing.T, call mutationCall) {
				if call.operation != "archiveTrack" || call.id != trackID || call.version != 2 {
					t.Fatalf("call=%+v", call)
				}
			},
		},
		{
			name: "restore track", method: http.MethodPost, path: "/api/v1/admin/tracks/" + trackID + "/restore",
			scope: "admin.track.restore:" + trackID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":3,"unknown":"second"}`,
			responseContains: `"status":"READY"`,
			assertCall: func(t *testing.T, call mutationCall) {
				if call.operation != "restoreTrack" || call.id != trackID || call.version != 2 {
					t.Fatalf("call=%+v", call)
				}
			},
		},
		{
			name: "delete track", method: http.MethodDelete, path: "/api/v1/admin/tracks/" + trackID,
			scope: "admin.track.delete-permanently:" + trackID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":3,"unknown":"second"}`,
			responseContains: `"deleted":true`,
			assertCall: func(t *testing.T, call mutationCall) {
				if call.operation != "deleteTrack" || call.id != trackID || call.version != 2 {
					t.Fatalf("call=%+v", call)
				}
			},
		},
		{
			name: "restore tracks batch", method: http.MethodPost, path: "/api/v1/admin/tracks/batch/restore",
			scope: "admin.track.restore.batch", status: http.StatusOK,
			initialBody:      `{"items":[{"trackId":"` + trackID + `","expectedVersion":2},{"trackId":"` + secondTrackID + `","expectedVersion":3}],"unknown":"first"}`,
			replayBody:       `{"items":[{"trackId":"` + trackID + `","expectedVersion":2},{"trackId":"` + secondTrackID + `","expectedVersion":3}],"unknown":"second","another":true}`,
			conflictBody:     `{"items":[{"trackId":"` + trackID + `","expectedVersion":4},{"trackId":"` + secondTrackID + `","expectedVersion":3}],"unknown":"second"}`,
			responseContains: `"restored":2`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(BatchTrackMutationInput)
				if call.operation != "restoreTracksBatch" || len(input.Items) != 2 || input.Items[0].TrackID != trackID || input.Items[1].TrackID != secondTrackID {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "delete tracks batch", method: http.MethodPost, path: "/api/v1/admin/tracks/batch/delete-permanently",
			scope: "admin.track.delete-permanently.batch", status: http.StatusAccepted,
			initialBody:      `{"items":[{"trackId":"` + trackID + `","expectedVersion":2},{"trackId":"` + secondTrackID + `","expectedVersion":3}],"unknown":"first"}`,
			replayBody:       `{"items":[{"trackId":"` + trackID + `","expectedVersion":2},{"trackId":"` + secondTrackID + `","expectedVersion":3}],"unknown":"second","another":true}`,
			conflictBody:     `{"items":[{"trackId":"` + trackID + `","expectedVersion":2},{"trackId":"` + secondTrackID + `","expectedVersion":4}],"unknown":"second"}`,
			responseContains: `"status":"PENDING"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(BatchTrackMutationInput)
				if call.operation != "createPermanentDeleteBatch" || len(input.Items) != 2 || input.Items[0].ExpectedVersion != 2 || input.Items[1].ExpectedVersion != 3 {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "upsert lyrics", method: http.MethodPut, path: "/api/v1/admin/tracks/" + trackID + "/lyrics",
			scope: "admin.track.lyrics:" + trackID + ":zh-CN", status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"language":"zh-CN","format":"LRC","content":"[00:00]Line","isDefault":true,"unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"language":"zh-CN","format":"LRC","content":"[00:00]Line","isDefault":true,"unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":2,"language":"zh-CN","format":"LRC","content":"[00:01]Different","isDefault":true,"unknown":"second"}`,
			responseContains: `"language":"zh-CN"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(LyricsInput)
				if call.operation != "upsertLyrics" || call.id != trackID || input.ExpectedVersion != 2 || input.Language != "zh-CN" || input.Format != "LRC" || !input.Content.Set || input.Content.Value != "[00:00]Line" || !input.IsDefault.Set || !input.IsDefault.Value {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
		{
			name: "update user status", method: http.MethodPatch, path: "/api/v1/admin/users/" + userID + "/status",
			scope: "admin.user.status:" + userID, status: http.StatusOK,
			initialBody:      `{"expectedVersion":2,"status":"SUSPENDED","reason":"maintenance","unknown":"first"}`,
			replayBody:       `{"expectedVersion":2,"status":"SUSPENDED","reason":"maintenance","unknown":"second","another":true}`,
			conflictBody:     `{"expectedVersion":2,"status":"SUSPENDED","reason":"different reason","unknown":"second"}`,
			responseContains: `"status":"SUSPENDED"`,
			assertCall: func(t *testing.T, call mutationCall) {
				input := call.input.(UserStatusInput)
				if call.operation != "updateUserStatus" || call.id != userID || input.ExpectedVersion != 2 || input.Status != UserSuspended || input.Reason != "maintenance" {
					t.Fatalf("call=%+v input=%+v", call, input)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &mutationAPIStub{}
			auth := &mutationIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
			idempotency := newMutationIdempotencyStub()
			routes, err := NewRoutes(api, auth, idempotency)
			if err != nil {
				t.Fatal(err)
			}
			engine, err := httpserver.New(httpserver.Options{RegisterRoutes: func(engine *gin.Engine) {
				routes.Register(engine)
			}})
			if err != nil {
				t.Fatal(err)
			}
			key := "mutation-key-123"

			first := performMutationRequest(engine, test.method, test.path, test.initialBody, key)
			if first.Code != test.status || first.Header().Get("X-Idempotent-Replay") != "false" || !strings.Contains(first.Body.String(), test.responseContains) {
				t.Fatalf("first status/replay/body=%d/%q/%s", first.Code, first.Header().Get("X-Idempotent-Replay"), first.Body.String())
			}
			if len(api.calls) != 1 {
				t.Fatalf("service calls=%+v", api.calls)
			}
			if api.calls[0].actor != "admin" || api.calls[0].trace == "" {
				t.Fatalf("service actor/trace=%q/%q", api.calls[0].actor, api.calls[0].trace)
			}
			test.assertCall(t, api.calls[0])
			assertMutationBoundary(t, auth, idempotency, 1, test.scope, key)

			replay := performMutationRequest(engine, test.method, test.path, test.replayBody, key)
			if replay.Code != test.status || replay.Header().Get("X-Idempotent-Replay") != "true" || replay.Body.String() != first.Body.String() {
				t.Fatalf("replay status/header/body=%d/%q/%s first=%s", replay.Code, replay.Header().Get("X-Idempotent-Replay"), replay.Body.String(), first.Body.String())
			}
			if len(api.calls) != 1 {
				t.Fatalf("service executed during replay: %+v", api.calls)
			}
			assertMutationBoundary(t, auth, idempotency, 2, test.scope, key)
			if !bytes.Equal(idempotency.payloads[0], idempotency.payloads[1]) {
				t.Fatalf("unknown fields changed payload: first=%s replay=%s", idempotency.payloads[0], idempotency.payloads[1])
			}

			conflict := performMutationRequest(engine, test.method, test.path, test.conflictBody, key)
			if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), string(apperror.CodeIdempotencyKeyReused)) {
				t.Fatalf("conflict status/body=%d/%s", conflict.Code, conflict.Body.String())
			}
			if len(api.calls) != 1 {
				t.Fatalf("service executed during conflict: %+v", api.calls)
			}
			assertMutationBoundary(t, auth, idempotency, 3, test.scope, key)
			if bytes.Equal(idempotency.payloads[0], idempotency.payloads[2]) {
				t.Fatalf("known payload change was not detected: %s", idempotency.payloads[0])
			}
		})
	}
}

func TestMutationSchemaValidationPrecedesAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &mutationAPIStub{}
	auth := &mutationIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	routes, _ := NewRoutes(api, auth, newMutationIdempotencyStub())
	engine := gin.New()
	routes.Register(engine)
	id := "00000000-0000-4000-8000-000000000001"
	requests := []struct{ method, path, body string }{{http.MethodPost, "/api/v1/admin/artists", `{}`}, {http.MethodPatch, "/api/v1/admin/artists/" + id, `{"expectedVersion":1}`}, {http.MethodPost, "/api/v1/admin/albums", `{"title":"Album","artistCredits":[]}`}, {http.MethodPost, "/api/v1/admin/tracks", `{"title":"Track","artistCredits":[],"discNumber":1}`}, {http.MethodPost, "/api/v1/admin/tracks/" + id + "/restore", `{}`}, {http.MethodPost, "/api/v1/admin/tracks/batch/restore", `{"items":[{"trackId":"not-a-uuid","expectedVersion":1}]}`}, {http.MethodPost, "/api/v1/admin/tracks/batch/delete-permanently", `{"items":[{"trackId":"` + id + `","expectedVersion":1},{"trackId":"` + strings.ToUpper(id) + `","expectedVersion":1}]}`}, {http.MethodPut, "/api/v1/admin/tracks/" + id + "/lyrics", `{"expectedVersion":1,"language":"zh","format":"LRC","content":""}`}, {http.MethodPatch, "/api/v1/admin/users/" + id + "/status", `{"expectedVersion":1,"status":"DELETED","reason":"x"}`}}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s %s=%d body=%s", item.method, item.path, response.Code, response.Body.String())
		}
	}
	if auth.calls != 0 {
		t.Fatalf("auth calls=%d", auth.calls)
	}
}

func TestPermanentDeleteBatchStatusUsesAdminAuthenticationWithoutIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const jobID = "00000000-0000-4000-8000-000000000099"
	api := &mutationAPIStub{}
	auth := &mutationIdentityStub{actor: identity.AuthenticatedActor{UserID: "admin", Role: identity.RoleAdmin}}
	idempotency := newMutationIdempotencyStub()
	routes, err := NewRoutes(api, auth, idempotency)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	routes.Register(engine)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tracks/batch/delete-permanently/"+jobID, nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"`+jobID+`"`) {
		t.Fatalf("status/body=%d/%s", response.Code, response.Body.String())
	}
	if auth.calls != 1 || len(idempotency.inputs) != 0 || len(api.calls) != 1 || api.calls[0].operation != "permanentDeleteBatch" {
		t.Fatalf("auth/idempotency/calls=%d/%d/%+v", auth.calls, len(idempotency.inputs), api.calls)
	}
}

func performMutationRequest(engine http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func assertMutationBoundary(t *testing.T, auth *mutationIdentityStub, idempotency *mutationIdempotencyStub, calls int, scope, key string) {
	t.Helper()
	if auth.calls != calls || len(auth.authorizations) != calls || auth.authorizations[calls-1] != "Bearer admin-token" {
		t.Fatalf("auth calls/values=%d/%v", auth.calls, auth.authorizations)
	}
	if len(idempotency.inputs) != calls || len(idempotency.payloads) != calls {
		t.Fatalf("idempotency calls=%d payloads=%d", len(idempotency.inputs), len(idempotency.payloads))
	}
	input := idempotency.inputs[calls-1]
	if input.ActorID != "admin" || input.Scope != scope || input.Key != key {
		t.Fatalf("idempotency input=%+v", input)
	}
	if strings.Contains(string(idempotency.payloads[calls-1]), "unknown") || strings.Contains(string(idempotency.payloads[calls-1]), "another") {
		t.Fatalf("unknown field reached payload: %s", idempotency.payloads[calls-1])
	}
}

type mutationCall struct {
	operation string
	actor     string
	trace     string
	id        string
	version   int
	input     any
}

type mutationAPIStub struct{ calls []mutationCall }

func (stub *mutationAPIStub) append(operation, actor, trace, id string, version int, input any) {
	stub.calls = append(stub.calls, mutationCall{operation: operation, actor: actor, trace: trace, id: id, version: version, input: input})
}

func (stub *mutationAPIStub) CreateArtist(_ context.Context, actor, trace string, input CreateArtistInput) (ArtistDTO, error) {
	stub.append("createArtist", actor, trace, "", 0, input)
	return ArtistDTO{ID: "artist-created", Name: input.Name, Version: 1}, nil
}
func (stub *mutationAPIStub) UpdateArtist(_ context.Context, actor, trace, id string, input UpdateArtistInput) (ArtistDTO, error) {
	stub.append("updateArtist", actor, trace, id, 0, input)
	return ArtistDTO{ID: id, Name: input.Name.Value, Version: input.ExpectedVersion + 1}, nil
}
func (stub *mutationAPIStub) CreateAlbum(_ context.Context, actor, trace string, input CreateAlbumInput) (AlbumDTO, error) {
	stub.append("createAlbum", actor, trace, "", 0, input)
	return AlbumDTO{ID: "album-created", Title: input.Title, Version: 1}, nil
}
func (stub *mutationAPIStub) UpdateAlbum(_ context.Context, actor, trace, id string, input UpdateAlbumInput) (AlbumDTO, error) {
	stub.append("updateAlbum", actor, trace, id, 0, input)
	return AlbumDTO{ID: id, Title: input.Title.Value, Version: input.ExpectedVersion + 1}, nil
}
func (stub *mutationAPIStub) MergeAlbums(_ context.Context, actor, trace string, input MergeAlbumsInput) (MergeResultDTO, error) {
	stub.append("mergeAlbums", actor, trace, "", 0, input)
	return MergeResultDTO{TargetAlbumID: input.Target.AlbumID, MergedAlbums: len(input.Sources), MovedTracks: 4, TargetVersion: input.Target.ExpectedVersion + 1}, nil
}
func (stub *mutationAPIStub) CreateTrack(_ context.Context, actor, trace string, input CreateTrackInput) (TrackDTO, error) {
	stub.append("createTrack", actor, trace, "", 0, input)
	return TrackDTO{ID: "track-created", Title: input.Title, DiscNumber: input.DiscNumber.Value, Status: "DRAFT", Version: 1}, nil
}
func (stub *mutationAPIStub) UpdateTrack(_ context.Context, actor, trace, id string, input UpdateTrackInput) (TrackDTO, error) {
	stub.append("updateTrack", actor, trace, id, 0, input)
	return TrackDTO{ID: id, Title: input.Title.Value, DiscNumber: input.DiscNumber.Value, Status: "DRAFT", Version: input.ExpectedVersion + 1}, nil
}
func (stub *mutationAPIStub) PublishTrack(_ context.Context, actor, trace, id string, version int) (TrackDTO, error) {
	stub.append("publishTrack", actor, trace, id, version, nil)
	return TrackDTO{ID: id, Status: "READY", Version: version + 1}, nil
}
func (stub *mutationAPIStub) ArchiveTrack(_ context.Context, actor, trace, id string, version int) (TrackDTO, error) {
	stub.append("archiveTrack", actor, trace, id, version, nil)
	return TrackDTO{ID: id, Status: "ARCHIVED", Version: version + 1}, nil
}
func (stub *mutationAPIStub) RestoreTrack(_ context.Context, actor, trace, id string, version int) (TrackDTO, error) {
	stub.append("restoreTrack", actor, trace, id, version, nil)
	return TrackDTO{ID: id, Status: "READY", Version: version + 1}, nil
}
func (stub *mutationAPIStub) RestoreTracksBatch(_ context.Context, actor, trace string, input BatchTrackMutationInput) (BatchRestoreDTO, error) {
	stub.append("restoreTracksBatch", actor, trace, "", 0, input)
	items := make([]BatchRestoreItemDTO, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, BatchRestoreItemDTO{TrackID: item.TrackID, Status: "READY", Version: item.ExpectedVersion + 1})
	}
	return BatchRestoreDTO{Restored: len(items), Items: items}, nil
}
func (stub *mutationAPIStub) DeleteTrackPermanently(_ context.Context, actor, trace, id string, version int) (DeleteTrackDTO, error) {
	stub.append("deleteTrack", actor, trace, id, version, nil)
	return DeleteTrackDTO{Deleted: true, DeletedFiles: 2, QuarantinedFiles: 1, ScheduledObjects: 3}, nil
}
func (stub *mutationAPIStub) CreatePermanentDeleteBatch(_ context.Context, actor, trace string, input BatchTrackMutationInput) (PermanentDeleteBatchDTO, error) {
	stub.append("createPermanentDeleteBatch", actor, trace, "", 0, input)
	return PermanentDeleteBatchDTO{ID: "00000000-0000-4000-8000-000000000099", Status: DeleteBatchPending, Total: len(input.Items), Items: []PermanentDeleteBatchItemDTO{}}, nil
}
func (stub *mutationAPIStub) PermanentDeleteBatch(_ context.Context, id string) (PermanentDeleteBatchDTO, error) {
	stub.append("permanentDeleteBatch", "", "", id, 0, nil)
	return PermanentDeleteBatchDTO{ID: id, Status: DeleteBatchPending, Total: 2, Items: []PermanentDeleteBatchItemDTO{}}, nil
}
func (stub *mutationAPIStub) UpsertLyrics(_ context.Context, actor, trace, id string, input LyricsInput) (LyricDTO, error) {
	stub.append("upsertLyrics", actor, trace, id, 0, input)
	return LyricDTO{ID: "lyric-1", TrackID: id, Language: input.Language, Format: input.Format, Content: input.Content.Value, IsDefault: input.IsDefault.Value, TrackVersion: input.ExpectedVersion + 1}, nil
}
func (stub *mutationAPIStub) UpdateUserStatus(_ context.Context, actor, trace, id string, input UserStatusInput) (UserStatusDTO, error) {
	stub.append("updateUserStatus", actor, trace, id, 0, input)
	return UserStatusDTO{ID: id, Username: "listener", Status: input.Status, Version: input.ExpectedVersion + 1}, nil
}

type mutationIdentityStub struct {
	actor          identity.AuthenticatedActor
	err            error
	calls          int
	authorizations []string
}

func (stub *mutationIdentityStub) Authenticate(_ context.Context, authorization string) (identity.AuthenticatedActor, error) {
	stub.calls++
	stub.authorizations = append(stub.authorizations, authorization)
	return stub.actor, stub.err
}
func (*mutationIdentityStub) Login(context.Context, identity.LoginInput) (identity.AuthSessionDTO, error) {
	return identity.AuthSessionDTO{}, errors.New("unexpected")
}
func (*mutationIdentityStub) Refresh(context.Context, string, string) (identity.RefreshResult, error) {
	return identity.RefreshResult{}, errors.New("unexpected")
}
func (*mutationIdentityStub) Logout(context.Context, identity.AuthenticatedActor) error {
	return errors.New("unexpected")
}
func (*mutationIdentityStub) GetAuthenticatedUser(context.Context, identity.AuthenticatedActor) (identity.CurrentUserDTO, error) {
	return identity.CurrentUserDTO{}, errors.New("unexpected")
}

type mutationIdempotencyRecord struct {
	payload  []byte
	response IdempotencyResponse
}

type mutationIdempotencyStub struct {
	inputs   []IdempotencyInput
	payloads [][]byte
	records  map[string]mutationIdempotencyRecord
}

func newMutationIdempotencyStub() *mutationIdempotencyStub {
	return &mutationIdempotencyStub{records: make(map[string]mutationIdempotencyRecord)}
}

func (stub *mutationIdempotencyStub) Execute(_ context.Context, input IdempotencyInput, operation func() (IdempotencyResponse, error)) (IdempotencyResult, error) {
	payload, err := sharedidempotency.CanonicalJSON(input.Payload)
	if err != nil {
		return IdempotencyResult{}, err
	}
	stub.inputs = append(stub.inputs, input)
	stub.payloads = append(stub.payloads, append([]byte(nil), payload...))
	key := input.ActorID + "\x00" + input.Scope + "\x00" + input.Key
	if record, exists := stub.records[key]; exists {
		if !bytes.Equal(record.payload, payload) {
			return IdempotencyResult{}, apperror.Conflict(apperror.CodeIdempotencyKeyReused, "idempotency key payload changed", nil)
		}
		return IdempotencyResult{Status: record.response.Status, Body: append([]byte(nil), record.response.Body...), Replayed: true}, nil
	}
	response, err := operation()
	if err != nil {
		return IdempotencyResult{}, err
	}
	response.Body = append([]byte(nil), response.Body...)
	stub.records[key] = mutationIdempotencyRecord{payload: append([]byte(nil), payload...), response: response}
	return IdempotencyResult{Status: response.Status, Body: append([]byte(nil), response.Body...)}, nil
}
