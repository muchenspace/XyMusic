import type { PlaylistVisibility } from "../../domain/music";

export interface ArtworkDto { url: string; cacheKey: string }
export interface ArtistRefDto { id: string; name: string }

export interface TrackDto {
  id: string;
  title: string;
  artists: ArtistRefDto[];
  album: { id: string; title: string } | null;
  artwork: ArtworkDto | null;
  durationMs: number;
  isFavorite: boolean;
  publishedAt: string;
}

export interface LyricDto {
  id: string;
  trackId: string;
  language: string;
  format: string;
  content: string;
  isDefault: boolean;
  trackVersion: number;
  updatedAt: string;
}

export interface TrackDetailDto extends TrackDto {
  lyrics: LyricDto[];
  lyricPage: number;
  lyricPageSize: number;
  lyricTotal: number;
  lyricTotalPages: number;
}

export interface AlbumDto {
  id: string;
  title: string;
  artists: ArtistRefDto[];
  cover: ArtworkDto | null;
  releaseDate: string | null;
  trackCount: number;
  description?: string | null;
}

export interface ArtistDto {
  id: string;
  name: string;
  artwork: ArtworkDto | null;
  description?: string | null;
}

export interface PlaylistDto {
  id: string;
  name: string;
  description: string | null;
  visibility: PlaylistVisibility;
  cover: ArtworkDto | null;
  trackCount: number;
  version: number;
}

export interface PlaylistEntryDto { id: string; position: number; track: TrackDto }
export interface PlaylistDetailDto extends PlaylistDto { entries: PlaylistEntryDto[]; nextCursor?: string | null }
export interface PageDto<T> { items: T[]; nextCursor?: string | null }
export interface RandomResponseDto<T> { items: T[] }

export interface SearchResponseDto {
  tracks?: PageDto<TrackDto> | null;
  artists?: PageDto<ArtistDto> | null;
  albums?: PageDto<AlbumDto> | null;
}
