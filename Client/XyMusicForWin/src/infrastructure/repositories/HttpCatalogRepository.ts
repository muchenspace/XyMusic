import type { CatalogRepository } from "../../application/ports/CatalogRepository";
import type { Album, Artist, Lyrics, SearchResults, Track } from "../../domain/music";
import type { AlbumSort, CursorPage, TrackSort } from "../../domain/pagination";
import { ApiClient, ApiError } from "../http/ApiClient";
import { collectPages, paginationError } from "../http/cursorPagination";
import { mapAlbum, mapArtist, mapTrack } from "../http/musicMappers";
import type { AlbumDto, PageDto, RandomResponseDto, SearchResponseDto, TrackDetailDto, TrackDto } from "../http/musicDtos";
import { buildLyrics } from "../lyrics/LyricsParser";

export class HttpCatalogRepository implements CatalogRepository {
  constructor(private readonly api: ApiClient) {}

  async listPublishedTracks(sort: TrackSort, cursor?: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Track>> {
    const page = await this.api.request<PageDto<TrackDto>>(pagePath("api/v1/tracks", { sort, cursor, limit }), { signal });
    return mapPage(page, mapTrack);
  }

  async search(query: string, signal?: AbortSignal): Promise<SearchResults> {
    const params = new URLSearchParams({ q: query, scope: "ALL", limit: "20" });
    const response = await this.api.request<SearchResponseDto>(`api/v1/search?${params}`, { signal });
    const tracks = mapOptionalPage(response.tracks, mapTrack);
    const artists = mapOptionalPage(response.artists, mapArtist);
    const albums = mapOptionalPage(response.albums, mapAlbum);
    return {
      tracks: tracks.items,
      artists: artists.items,
      albums: albums.items,
      nextCursors: { tracks: tracks.nextCursor, artists: artists.nextCursor, albums: albums.nextCursor },
    };
  }

  searchTracks(query: string, cursor: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Track>> {
    return this.searchPage(query, "TRACKS", cursor, limit, (response) => response.tracks, mapTrack, signal);
  }

  searchArtists(query: string, cursor: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Artist>> {
    return this.searchPage(query, "ARTISTS", cursor, limit, (response) => response.artists, mapArtist, signal);
  }

  searchAlbums(query: string, cursor: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Album>> {
    return this.searchPage(query, "ALBUMS", cursor, limit, (response) => response.albums, mapAlbum, signal);
  }

  async getLyrics(trackId: string, signal?: AbortSignal): Promise<Lyrics | null> {
    const resources: TrackDetailDto["lyrics"] = [];
    const seenLyricIds = new Set<string>();
    let requestedPage = 1;
    let expected: LyricPageMetadata | undefined;
    while (true) {
      const params = new URLSearchParams({ lyricPage: String(requestedPage), lyricPageSize: "100" });
      const detail = await this.api.request<TrackDetailDto>(
        `api/v1/tracks/${encodeURIComponent(trackId)}?${params}`,
        { signal },
      );
      const metadata = validateLyricPage(detail, trackId, requestedPage, seenLyricIds, expected);
      expected ??= metadata;
      resources.push(...detail.lyrics);
      if (requestedPage >= metadata.totalPages) return buildLyrics(trackId, resources);
      requestedPage += 1;
    }
  }

  getAlbumTracks(albumId: string, signal?: AbortSignal): Promise<Track[]> {
    return this.trackPages(`api/v1/tracks?sort=ALBUM_ORDER_ASC&limit=100&albumId=${encodeURIComponent(albumId)}`, signal);
  }

  getArtistTracks(artistId: string, signal?: AbortSignal): Promise<Track[]> {
    return this.trackPages(`api/v1/tracks?sort=PUBLISHED_DESC&limit=100&artistId=${encodeURIComponent(artistId)}`, signal);
  }

  async getAlbumTracksPage(albumId: string, cursor?: string, limit = 100, signal?: AbortSignal): Promise<CursorPage<Track>> {
    return this.trackPage(trackFilterPath("ALBUM_ORDER_ASC", "albumId", albumId, cursor, limit), signal);
  }

  async getArtistTracksPage(artistId: string, cursor?: string, limit = 100, signal?: AbortSignal): Promise<CursorPage<Track>> {
    return this.trackPage(trackFilterPath("PUBLISHED_DESC", "artistId", artistId, cursor, limit), signal);
  }

  async getRandomAlbums(limit: number, signal?: AbortSignal): Promise<Album[]> {
    const response = await this.api.request<RandomResponseDto<AlbumDto>>("api/v1/albums/random", {
      method: "POST", body: JSON.stringify({ limit: randomLimit(limit) }), signal,
    });
    if (!response || !Array.isArray(response.items)) throw new ApiError("随机专辑响应格式无效", 0, "INVALID_RESPONSE");
    return response.items.map(mapAlbum);
  }

  async getRandomTracks(limit: number, signal?: AbortSignal): Promise<Track[]> {
    const response = await this.api.request<RandomResponseDto<TrackDto>>("api/v1/tracks/random", {
      method: "POST", body: JSON.stringify({ limit: randomLimit(limit) }), signal,
    });
    if (!response || !Array.isArray(response.items)) throw new ApiError("随机歌曲响应格式无效", 0, "INVALID_RESPONSE");
    return response.items.map(mapTrack);
  }

  async listAlbums(sort: AlbumSort, cursor?: string, limit = 50, signal?: AbortSignal): Promise<CursorPage<Album>> {
    const page = await this.api.request<PageDto<AlbumDto>>(pagePath("api/v1/albums", { sort, cursor, limit }), { signal });
    return mapPage(page, mapAlbum);
  }

  private async trackPages(path: string, signal?: AbortSignal): Promise<Track[]> {
    return (await collectPages<TrackDto>(path, (nextPath) => this.api.request<PageDto<TrackDto>>(nextPath, { signal }), signal)).map(mapTrack);
  }

  private async trackPage(path: string, signal?: AbortSignal): Promise<CursorPage<Track>> {
    const page = await this.api.request<PageDto<TrackDto>>(path, { signal });
    return mapPage(page, mapTrack);
  }

  private async searchPage<TDto, TDomain>(
    query: string,
    scope: "TRACKS" | "ARTISTS" | "ALBUMS",
    cursor: string,
    limit: number,
    select: (response: SearchResponseDto) => PageDto<TDto> | null | undefined,
    mapper: (value: TDto, index: number) => TDomain,
    signal?: AbortSignal,
  ): Promise<CursorPage<TDomain>> {
    const params = new URLSearchParams({ q: query, scope, limit: String(Math.max(1, Math.min(100, Math.round(limit)))), cursor });
    const response = await this.api.request<SearchResponseDto>(`api/v1/search?${params}`, { signal });
    const page = select(response);
    if (!page || !Array.isArray(page.items)) throw new ApiError("搜索分页响应格式无效", 0, "INVALID_RESPONSE");
    return mapPage(page, mapper);
  }
}

interface LyricPageMetadata {
  pageSize: number;
  total: number;
  totalPages: number;
  trackVersion: number | null;
}

function validateLyricPage(
  detail: TrackDetailDto | null | undefined,
  trackId: string,
  requestedPage: number,
  seenLyricIds: Set<string>,
  expected?: LyricPageMetadata,
): LyricPageMetadata {
  if (!detail || detail.id !== trackId || !Array.isArray(detail.lyrics)) {
    throw paginationError("歌词分页响应格式无效");
  }
  const { lyricPage, lyricPageSize, lyricTotal, lyricTotalPages } = detail;
  if (!Number.isInteger(lyricPage) || lyricPage !== requestedPage ||
      !Number.isInteger(lyricPageSize) || lyricPageSize < 1 || lyricPageSize > 100 ||
      !Number.isInteger(lyricTotal) || lyricTotal < 0 ||
      !Number.isInteger(lyricTotalPages) || lyricTotalPages < 0 ||
      lyricTotalPages !== Math.ceil(lyricTotal / lyricPageSize) ||
      (lyricTotalPages > 0 && lyricPage > lyricTotalPages)) {
    throw paginationError("歌词分页页码或统计信息无效");
  }
  const remaining = Math.max(0, lyricTotal - (lyricPage - 1) * lyricPageSize);
  if (detail.lyrics.length !== Math.min(lyricPageSize, remaining)) {
    throw paginationError("歌词分页数据数量与统计信息不一致");
  }
  let trackVersion: number | null = null;
  for (const lyric of detail.lyrics) {
    if (!lyric || typeof lyric.id !== "string" || !lyric.id || lyric.trackId !== trackId ||
        !Number.isInteger(lyric.trackVersion) || lyric.trackVersion < 1 ||
        (trackVersion !== null && lyric.trackVersion !== trackVersion) || seenLyricIds.has(lyric.id)) {
      throw paginationError("歌词分页包含不一致或重复的数据");
    }
    trackVersion = lyric.trackVersion;
    seenLyricIds.add(lyric.id);
  }
  const metadata = { pageSize: lyricPageSize, total: lyricTotal, totalPages: lyricTotalPages, trackVersion };
  if (expected && (metadata.pageSize !== expected.pageSize || metadata.total !== expected.total ||
      metadata.totalPages !== expected.totalPages || metadata.trackVersion !== expected.trackVersion)) {
    throw paginationError("歌词分页统计信息在翻页过程中发生变化");
  }
  return metadata;
}

function randomLimit(value: number): number {
  if (!Number.isInteger(value) || value < 1 || value > 50) throw new Error("随机内容数量必须在 1-50 之间");
  return value;
}

function pagePath(base: string, values: { sort: string; cursor?: string; limit: number }): string {
  const params = new URLSearchParams({ sort: values.sort, limit: String(values.limit) });
  if (values.cursor) params.set("cursor", values.cursor);
  return `${base}?${params}`;
}

function trackFilterPath(sort: string, filter: "albumId" | "artistId", id: string, cursor: string | undefined, limit: number): string {
  const params = new URLSearchParams({ sort, limit: String(Math.max(1, Math.min(100, Math.round(limit)))), [filter]: id });
  if (cursor) params.set("cursor", cursor);
  return `api/v1/tracks?${params}`;
}

function mapPage<TDto, TDomain>(page: PageDto<TDto>, mapper: (value: TDto, index: number) => TDomain): CursorPage<TDomain> {
  if (!page || !Array.isArray(page.items)) throw new ApiError("分页响应缺少数据列表", 0, "INVALID_PAGINATION");
  return { items: page.items.map(mapper), nextCursor: page.nextCursor || null };
}

function mapOptionalPage<TDto, TDomain>(page: PageDto<TDto> | null | undefined, mapper: (value: TDto, index: number) => TDomain): CursorPage<TDomain> {
  return page ? mapPage(page, mapper) : { items: [], nextCursor: null };
}
