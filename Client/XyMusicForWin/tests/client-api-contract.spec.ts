import { describe, expect, it } from "vitest";
import type { ApiRequestInit } from "../src/infrastructure/http/ApiClient";
import { ApiClient, ApiError } from "../src/infrastructure/http/ApiClient";
import type { Playlist, PlaylistDetail } from "../src/domain/music";
import { HttpCatalogRepository } from "../src/infrastructure/repositories/HttpCatalogRepository";
import { HttpLibraryRepository } from "../src/infrastructure/repositories/HttpLibraryRepository";
import { HttpPlaybackRepository } from "../src/infrastructure/repositories/HttpPlaybackRepository";
import { HttpPlaylistRepository } from "../src/infrastructure/repositories/HttpPlaylistRepository";
import type {
  AlbumDto,
  ArtistDto,
  PlaylistDetailDto,
  PlaylistDto,
  TrackDetailDto,
  TrackDto,
} from "../src/infrastructure/http/musicDtos";

describe("desktop catalog API contract", () => {
  it("uses the catalog list, search, detail, and album routes", async () => {
    const api = new RecordingApi([
      { items: [trackDto()], nextCursor: "track-next" },
      {
        tracks: { items: [trackDto()], nextCursor: null },
        artists: { items: [artistDto()], nextCursor: "artist-next" },
        albums: { items: [albumDto()], nextCursor: null },
      },
      trackDetailDto({ lyricTotal: 2, lyricTotalPages: 2 }),
      trackDetailDto({
        lyricPage: 2,
        lyricTotal: 2,
        lyricTotalPages: 2,
        lyrics: [lyricDto({ id: "lyric-2", language: "en", content: "[00:01.00]Translation", isDefault: false })],
      }),
      { items: [albumDto()], nextCursor: null },
    ]);
    const repository = new HttpCatalogRepository(api.client);

    const tracks = await repository.listPublishedTracks("TITLE_ASC", "next/value", 25);
    const search = await repository.search("rock & roll");
    const lyrics = await repository.getLyrics("track/1");
    const albums = await repository.listAlbums("TITLE_DESC", "album/page", 30);

    expect(api.paths()).toEqual([
      "api/v1/tracks?sort=TITLE_ASC&limit=25&cursor=next%2Fvalue",
      "api/v1/search?q=rock+%26+roll&scope=ALL&limit=20",
      "api/v1/tracks/track%2F1?lyricPage=1&lyricPageSize=100",
      "api/v1/tracks/track%2F1?lyricPage=2&lyricPageSize=100",
      "api/v1/albums?sort=TITLE_DESC&limit=30&cursor=album%2Fpage",
    ]);
    expect(tracks).toMatchObject({ nextCursor: "track-next", items: [{ id: "track-1", duration: 181 }] });
    expect(search).toMatchObject({
      tracks: [{ id: "track-1" }],
      artists: [{ id: "artist-1" }],
      albums: [{ id: "album-1" }],
      nextCursors: { tracks: null, artists: "artist-next", albums: null },
    });
    expect(lyrics).toMatchObject({ trackId: "track/1", synchronized: true, lines: [{ time: 1, text: "Line" }] });
    expect(albums.items[0]).toMatchObject({ id: "album-1", year: 2025 });
  });

  it("rejects lyric pages that do not advance, change versions, or repeat lyric IDs", async () => {
    const stalled = new RecordingApi([
      trackDetailDto({ lyricTotal: 2, lyricTotalPages: 2 }),
      trackDetailDto({ lyricPage: 1, lyricTotal: 2, lyricTotalPages: 2 }),
    ]);
    await expect(new HttpCatalogRepository(stalled.client).getLyrics("track/1"))
      .rejects.toMatchObject<ApiError>({ code: "INVALID_PAGINATION" });

    const changedVersion = new RecordingApi([
      trackDetailDto({ lyricTotal: 2, lyricTotalPages: 2 }),
      trackDetailDto({
        lyricPage: 2,
        lyricTotal: 2,
        lyricTotalPages: 2,
        lyrics: [lyricDto({ id: "lyric-2", trackVersion: 8 })],
      }),
    ]);
    await expect(new HttpCatalogRepository(changedVersion.client).getLyrics("track/1"))
      .rejects.toMatchObject<ApiError>({ code: "INVALID_PAGINATION" });

    const repeated = new RecordingApi([
      trackDetailDto({ lyricTotal: 2, lyricTotalPages: 2 }),
      trackDetailDto({ lyricPage: 2, lyricTotal: 2, lyricTotalPages: 2 }),
    ]);
    await expect(new HttpCatalogRepository(repeated.client).getLyrics("track/1"))
      .rejects.toMatchObject<ApiError>({ code: "INVALID_PAGINATION" });

    expect(stalled.paths()).toEqual([
      "api/v1/tracks/track%2F1?lyricPage=1&lyricPageSize=100",
      "api/v1/tracks/track%2F1?lyricPage=2&lyricPageSize=100",
    ]);
  });

  it("uses every scoped search route and validates scoped responses", async () => {
    const api = new RecordingApi([
      { tracks: { items: [trackDto()], nextCursor: "tracks-next" } },
      { artists: { items: [artistDto()], nextCursor: "artists-next" } },
      { albums: { items: [albumDto()], nextCursor: "albums-next" } },
      { tracks: null },
    ]);
    const repository = new HttpCatalogRepository(api.client);

    await expect(repository.searchTracks("needle", "cursor/1", 200)).resolves.toMatchObject({ nextCursor: "tracks-next" });
    await expect(repository.searchArtists("needle", "cursor/2", 0)).resolves.toMatchObject({ nextCursor: "artists-next" });
    await expect(repository.searchAlbums("needle", "cursor/3", 20)).resolves.toMatchObject({ nextCursor: "albums-next" });
    await expect(repository.searchTracks("needle", "cursor/4", 20)).rejects.toMatchObject({ code: "INVALID_RESPONSE" });

    expect(api.paths()).toEqual([
      "api/v1/search?q=needle&scope=TRACKS&limit=100&cursor=cursor%2F1",
      "api/v1/search?q=needle&scope=ARTISTS&limit=1&cursor=cursor%2F2",
      "api/v1/search?q=needle&scope=ALBUMS&limit=20&cursor=cursor%2F3",
      "api/v1/search?q=needle&scope=TRACKS&limit=20&cursor=cursor%2F4",
    ]);
  });

  it("uses filtered track pagination and both random-content routes", async () => {
    const api = new RecordingApi([
      { items: [trackDto("track-a")], nextCursor: "next/page" },
      { items: [trackDto("track-b")], nextCursor: null },
      { items: [trackDto("track-c")], nextCursor: null },
      { items: [trackDto("track-d")], nextCursor: "album-next" },
      { items: [trackDto("track-e")], nextCursor: "artist-next" },
      { items: [albumDto()] },
      { items: [trackDto()] },
    ]);
    const repository = new HttpCatalogRepository(api.client);

    await expect(repository.getAlbumTracks("album/1")).resolves.toHaveLength(2);
    await expect(repository.getArtistTracks("artist/1")).resolves.toHaveLength(1);
    await expect(repository.getAlbumTracksPage("album/2", "cursor/a", 500)).resolves.toMatchObject({ nextCursor: "album-next" });
    await expect(repository.getArtistTracksPage("artist/2", "cursor/b", -5)).resolves.toMatchObject({ nextCursor: "artist-next" });
    await repository.getRandomAlbums(6);
    await repository.getRandomTracks(7);

    expect(api.paths()).toEqual([
      "api/v1/tracks?sort=ALBUM_ORDER_ASC&limit=100&albumId=album%2F1",
      "api/v1/tracks?sort=ALBUM_ORDER_ASC&limit=100&albumId=album%2F1&cursor=next%2Fpage",
      "api/v1/tracks?sort=PUBLISHED_DESC&limit=100&artistId=artist%2F1",
      "api/v1/tracks?sort=ALBUM_ORDER_ASC&limit=100&albumId=album%2F2&cursor=cursor%2Fa",
      "api/v1/tracks?sort=PUBLISHED_DESC&limit=1&artistId=artist%2F2&cursor=cursor%2Fb",
      "api/v1/albums/random",
      "api/v1/tracks/random",
    ]);
    expect(api.calls[5]?.init).toMatchObject({ method: "POST" });
    expect(parseBody(api.calls[5])).toEqual({ limit: 6 });
    expect(parseBody(api.calls[6])).toEqual({ limit: 7 });
    await expect(repository.getRandomTracks(0)).rejects.toThrow("1-50");
    expect(api.calls).toHaveLength(7);
  });
});

describe("desktop library and playback API contract", () => {
  it("covers favorite mutations, favorites paging, and history paging", async () => {
    const api = new RecordingApi([
      undefined,
      undefined,
      { items: [{ track: trackDto() }], nextCursor: "favorite-next" },
      { items: [{ track: trackDto() }], nextCursor: "history-next" },
    ]);
    const repository = new HttpLibraryRepository(api.client);
    const controller = new AbortController();

    await repository.setFavorite("track/1", true, controller.signal);
    await repository.setFavorite("track/1", false, controller.signal);
    const favorites = await repository.getFavoriteTracks("FAVORITED_DESC", "fav/page", 150, controller.signal);
    const history = await repository.getHistoryTracks("history/page", 0, controller.signal);

    expect(api.paths()).toEqual([
      "api/v1/library/favorites/track%2F1",
      "api/v1/library/favorites/track%2F1",
      "api/v1/library/favorites?sort=FAVORITED_DESC&limit=100&cursor=fav%2Fpage",
      "api/v1/library/history?limit=1&cursor=history%2Fpage",
    ]);
    expect(api.calls.map((call) => call.init.method)).toEqual(["PUT", "DELETE", undefined, undefined]);
    expect(api.calls.every((call) => call.init.signal === controller.signal)).toBe(true);
    expect(favorites).toMatchObject({ nextCursor: "favorite-next", items: [{ id: "track-1" }] });
    expect(history).toMatchObject({ nextCursor: "history-next", items: [{ id: "track-1" }] });
  });

  it("covers playback grants and idempotent history events", async () => {
    const grant = { url: "https://media.example/track", expiresAt: "2026-07-17T01:00:00Z", selectedQuality: "HIGH" };
    const api = new RecordingApi([grant, undefined]);
    const repository = new HttpPlaybackRepository(api.client);

    await expect(repository.getPlaybackGrant("track/1", "HIGH")).resolves.toEqual(grant);
    await repository.recordPlayback("track/1", "session-1", 1234.6, "PROGRESS");

    expect(api.paths()).toEqual([
      "api/v1/tracks/track%2F1/playback",
      "api/v1/library/history/track%2F1",
    ]);
    expect(api.calls[0]?.init.method).toBe("POST");
    expect(parseBody(api.calls[0])).toEqual({ preferredQuality: "HIGH", acceptedCodecs: ["aac", "mp3", "flac", "opus"] });
    expect(api.calls[1]?.init.method).toBe("PUT");
    expect(new Headers(api.calls[1]?.init.headers).get("Idempotency-Key")).toBeTruthy();
    expect(parseBody(api.calls[1])).toMatchObject({ playbackSessionId: "session-1", positionMs: 1235, event: "PROGRESS" });
    expect(Number.isNaN(Date.parse(String(parseBody(api.calls[1]).occurredAt)))).toBe(false);
  });
});

describe("desktop playlist API contract", () => {
  it("covers list, detail continuation, and explicit detail pages", async () => {
    const api = new RecordingApi([
      { items: [playlistDto()], nextCursor: "playlist-next" },
      playlistDetailDto(["entry-1"], "entry-next"),
      playlistDetailDto(["entry-2"], null),
      playlistDetailDto(["entry-3"], "page-next"),
    ]);
    const repository = new HttpPlaylistRepository(api.client);

    await expect(repository.list("UPDATED_DESC", "list/page", 500)).resolves.toMatchObject({ nextCursor: "playlist-next" });
    const complete = await repository.get("playlist/1");
    const page = await repository.getPage("playlist/1", "entry/page", 0);

    expect(api.paths()).toEqual([
      "api/v1/playlists?sort=UPDATED_DESC&limit=100&cursor=list%2Fpage",
      "api/v1/playlists/playlist%2F1?limit=100",
      "api/v1/playlists/playlist%2F1?limit=100&cursor=entry-next",
      "api/v1/playlists/playlist%2F1?limit=1&cursor=entry%2Fpage",
    ]);
    expect(complete.entries.map((entry) => entry.id)).toEqual(["entry-1", "entry-2"]);
    expect(complete.nextCursor).toBeNull();
    expect(page).toMatchObject({ nextCursor: "page-next", entries: [{ id: "entry-3" }] });
  });

  it("covers every playlist mutation with versioning and idempotency", async () => {
    const api = new RecordingApi([
      playlistDto({ id: "created", version: 1 }),
      playlistDto({ version: 4, name: "Updated" }),
      undefined,
      { version: 5 },
      { version: 6 },
      { version: 7 },
    ]);
    const repository = new HttpPlaylistRepository(api.client);
    const playlist = playlistDomain();
    const detail = playlistDetailDomain();

    await repository.create("Created", "", "PRIVATE");
    await repository.update(playlist, { name: "Updated", visibility: "PUBLIC" });
    await repository.delete(playlist);
    await expect(repository.addTrack(playlist, "track/1")).resolves.toBe(5);
    await expect(repository.removeTrack(detail, "entry/1")).resolves.toBe(6);
    await expect(repository.reorder(detail, ["entry-2", "entry-1"])).resolves.toBe(7);

    expect(api.paths()).toEqual([
      "api/v1/playlists",
      "api/v1/playlists/playlist-1",
      "api/v1/playlists/playlist-1?expectedVersion=3",
      "api/v1/playlists/playlist-1/tracks",
      "api/v1/playlists/playlist-1/tracks/entry%2F1?expectedVersion=3",
      "api/v1/playlists/playlist-1/tracks/order",
    ]);
    expect(api.calls.map((call) => call.init.method)).toEqual(["POST", "PATCH", "DELETE", "POST", "DELETE", "PATCH"]);
    for (const call of api.calls) expect(new Headers(call.init.headers).get("Idempotency-Key")).toBeTruthy();
    expect(parseBody(api.calls[0])).toEqual({ name: "Created", description: null, visibility: "PRIVATE" });
    expect(parseBody(api.calls[1])).toEqual({ expectedVersion: 3, name: "Updated", visibility: "PUBLIC" });
    expect(parseBody(api.calls[3])).toEqual({ expectedVersion: 3, trackId: "track/1", insertAfterEntryId: null });
    expect(parseBody(api.calls[5])).toEqual({ expectedVersion: 3, orderedEntryIds: ["entry-2", "entry-1"] });
  });

  it("rejects a continuation page for a different playlist", async () => {
    const api = new RecordingApi([
      playlistDetailDto(["entry-1"], "next"),
      { ...playlistDetailDto(["entry-2"], null), id: "other-playlist" },
    ]);

    await expect(new HttpPlaylistRepository(api.client).get("playlist-1"))
      .rejects.toMatchObject<ApiError>({ code: "INVALID_PAGINATION" });
  });
});

interface RecordedCall {
  path: string;
  init: ApiRequestInit;
}

class RecordingApi {
  readonly calls: RecordedCall[] = [];
  private readonly responses: unknown[];

  constructor(responses: unknown[]) {
    this.responses = [...responses];
  }

  get client(): ApiClient {
    return this as unknown as ApiClient;
  }

  request<T>(path: string, init: ApiRequestInit = {}): Promise<T> {
    this.calls.push({ path, init });
    if (!this.responses.length) return Promise.reject(new Error(`No response queued for ${path}`));
    const response = this.responses.shift();
    return response instanceof Error ? Promise.reject(response) : Promise.resolve(response as T);
  }

  paths(): string[] {
    return this.calls.map((call) => call.path);
  }
}

function parseBody(call: RecordedCall | undefined): Record<string, unknown> {
  return JSON.parse(String(call?.init.body)) as Record<string, unknown>;
}

function trackDto(id = "track-1"): TrackDto {
  return {
    id,
    title: "Track",
    artists: [{ id: "artist-1", name: "Artist" }],
    album: { id: "album-1", title: "Album" },
    artwork: { url: "https://media.example/cover", cacheKey: "cover-1" },
    durationMs: 180_500,
    isFavorite: false,
    publishedAt: "2026-07-17T00:00:00Z",
  };
}

function trackDetailDto(overrides: Partial<TrackDetailDto> = {}): TrackDetailDto {
  return {
    ...trackDto("track/1"),
    lyrics: [lyricDto()],
    lyricPage: 1,
    lyricPageSize: 1,
    lyricTotal: 1,
    lyricTotalPages: 1,
    ...overrides,
  };
}

function lyricDto(overrides: Partial<TrackDetailDto["lyrics"][number]> = {}): TrackDetailDto["lyrics"][number] {
  return {
    id: "lyric-1",
    trackId: "track/1",
    language: "zh",
    format: "LRC",
    content: "[00:01.00]Line",
    isDefault: true,
    trackVersion: 7,
    updatedAt: "2026-07-17T00:00:00Z",
    ...overrides,
  };
}

function albumDto(): AlbumDto {
  return {
    id: "album-1",
    title: "Album",
    artists: [{ id: "artist-1", name: "Artist" }],
    cover: { url: "https://media.example/album", cacheKey: "album-1" },
    releaseDate: "2025-04-03",
    trackCount: 1,
  };
}

function artistDto(): ArtistDto {
  return {
    id: "artist-1",
    name: "Artist",
    artwork: { url: "https://media.example/artist", cacheKey: "artist-1" },
    description: "Description",
  };
}

function playlistDto(overrides: Partial<PlaylistDto> = {}): PlaylistDto {
  return {
    id: "playlist-1",
    name: "Playlist",
    description: "Description",
    visibility: "PRIVATE",
    cover: null,
    trackCount: 2,
    version: 3,
    ...overrides,
  };
}

function playlistDetailDto(entryIds: string[], nextCursor: string | null): PlaylistDetailDto {
  return {
    ...playlistDto(),
    entries: entryIds.map((id, index) => ({ id, position: index, track: trackDto(`track-${index + 1}`) })),
    nextCursor,
  };
}

function playlistDomain(): Playlist {
  return {
    id: "playlist-1",
    title: "Playlist",
    description: "Description",
    coverUrl: "",
    trackCount: 2,
    accent: "#000000",
    version: 3,
    visibility: "PRIVATE",
  };
}

function playlistDetailDomain(): PlaylistDetail {
  return { ...playlistDomain(), entries: [], nextCursor: null };
}
