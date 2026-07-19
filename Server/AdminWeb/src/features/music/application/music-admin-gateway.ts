import type {
  AlbumDetail,
  AlbumDuplicateQuery,
  AlbumDuplicateSummary,
  AlbumMergeResult,
  AlbumMergeFieldSources,
  AlbumSummary,
  ArtistSummary,
  BatchRestoreTracksResult,
  CreditRole,
  MusicListQuery,
  MusicPage,
  PermanentDeleteTracksJob,
  TrackListQuery,
  TrackMetadataRecord,
  TrackMetadataUpdateTarget,
  TrackMutationTarget,
  TrackSummary,
  TrackTagPatch,
  TrackTagRevision,
} from "@/features/music/domain/models";

export interface UpdateTrackMetadataCommand {
  expectedVersion: number;
  patch: TrackTagPatch;
  resetFields?: string[];
  reason: string;
}

export interface BatchUpdateTrackMetadataCommand {
  items: TrackMetadataUpdateTarget[];
  patch: TrackTagPatch;
  reason: string;
}

export interface BatchUpdateTrackMetadataResult {
  items: Array<{ trackId: string; version: number; changedFields: string[] }>;
}

export interface UpdateAlbumCommand {
  expectedVersion: number;
  title: string;
  artistCredits: Array<{ artistId: string; role: CreditRole; sortOrder: number }>;
  releaseDate: string | null;
  description: string | null;
}

export interface MergeAlbumsCommand {
  target: { albumId: string; expectedVersion: number };
  sources: Array<{ albumId: string; expectedVersion: number }>;
  fieldSources: AlbumMergeFieldSources;
}

export interface UpdateArtistCommand {
  expectedVersion: number;
  name: string;
  description: string | null;
}

export interface MusicAdminGateway {
  listTracks(query: TrackListQuery, signal?: AbortSignal): Promise<MusicPage<TrackSummary>>;
  getTrackMetadata(trackId: string, signal?: AbortSignal): Promise<TrackMetadataRecord>;
  listTagHistory(trackId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<MusicPage<TrackTagRevision>>;
  updateTrackMetadata(trackId: string, command: UpdateTrackMetadataCommand): Promise<TrackMetadataRecord>;
  setTrackState(trackId: string, expectedVersion: number, action: "publish" | "archive" | "restore"): Promise<void>;
  batchRestoreTracks(items: TrackMutationTarget[]): Promise<BatchRestoreTracksResult>;
  createPermanentDeleteTracksJob(items: TrackMutationTarget[]): Promise<PermanentDeleteTracksJob>;
  getPermanentDeleteTracksJob(jobId: string, signal?: AbortSignal): Promise<PermanentDeleteTracksJob>;
  deleteTrackPermanently(trackId: string, expectedVersion: number): Promise<{
    deleted: boolean;
    deletedFiles: number;
    quarantinedFiles: number;
    scheduledObjects: number;
  }>;
  writeTrackMetadata(trackId: string, expectedVersion: number, reason: string): Promise<void>;
  batchUpdateTrackMetadata(command: BatchUpdateTrackMetadataCommand): Promise<BatchUpdateTrackMetadataResult>;
  restoreTagRevision(trackId: string, revisionId: string, expectedVersion: number, reason: string): Promise<TrackMetadataRecord>;
  listAlbums(query: MusicListQuery, signal?: AbortSignal): Promise<MusicPage<AlbumSummary>>;
  getAlbum(albumId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<AlbumDetail>;
  getAlbumDuplicates(query: AlbumDuplicateQuery, signal?: AbortSignal): Promise<AlbumDuplicateSummary>;
  updateAlbum(albumId: string, command: UpdateAlbumCommand): Promise<void>;
  mergeAlbums(command: MergeAlbumsCommand): Promise<AlbumMergeResult>;
  listArtists(query: MusicListQuery, signal?: AbortSignal): Promise<MusicPage<ArtistSummary>>;
  updateArtist(artistId: string, command: UpdateArtistCommand): Promise<void>;
}
