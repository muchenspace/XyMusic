/**
 * Central query-key factories for the music feature. Values must stay
 * identical to the keys previously inlined in pages, because TanStack Query
 * matches caches structurally.
 */

export interface TrackListQueryKeyParams {
  page: number;
  pageSize: number;
  search: string;
  status: string;
  metadataStatus: string;
  cursor: string;
}

export interface AlbumListQueryKeyParams {
  page: number;
  pageSize: number;
  search: string;
  cursor: string;
}

export interface AlbumDuplicatesQueryKeyParams {
  page: number;
  pageSize: number;
  cursor: string;
}

export interface AlbumDetailQueryKeyParams {
  page: number;
  pageSize: number;
  cursor: string;
}

export interface ArtistListQueryKeyParams {
  page: number;
  pageSize: number;
  search: string;
  cursor: string;
}

export const musicQueryKeys = {
  tracks: (params: TrackListQueryKeyParams) => ["admin", "tracks", params] as const,
  tracksPrefix: ["admin", "tracks"] as const,
  permanentDeleteJob: (jobId: string) => ["admin", "tracks", "permanent-delete", jobId] as const,
  trackMetadata: (trackId: string | undefined) => ["admin", "track", trackId, "metadata"] as const,
  trackPrefix: ["admin", "track"] as const,
  albums: (params: AlbumListQueryKeyParams) => ["admin", "albums", params] as const,
  albumsPrefix: ["admin", "albums"] as const,
  albumDuplicates: (params: AlbumDuplicatesQueryKeyParams) => ["admin", "albums", "duplicates", params] as const,
  albumDuplicateGroup: (albumId: string) => ["admin", "albums", "duplicates", { albumId }] as const,
  album: (albumId: string, params: AlbumDetailQueryKeyParams) => ["admin", "album", albumId, params] as const,
  albumPrefix: ["admin", "album"] as const,
  artists: (params: ArtistListQueryKeyParams) => ["admin", "artists", params] as const,
  artistsPrefix: ["admin", "artists"] as const,
};
