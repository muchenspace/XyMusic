import { adminApi } from "@/api/admin";
import type {
  BatchUpdateTrackMetadataCommand,
  BatchUpdateTrackMetadataResult,
  MergeAlbumsCommand,
  MusicAdminGateway,
  UpdateAlbumCommand,
  UpdateArtistCommand,
  UpdateTrackMetadataCommand,
} from "@/features/music/application/music-admin-gateway";
import type {
  AlbumDetail,
  AlbumDuplicateQuery,
  AlbumDuplicateSummary,
  AlbumMergeResult,
  AlbumSummary,
  ArtistSummary,
  BatchRestoreTracksResult,
  MusicListQuery,
  MusicPage,
  PermanentDeleteTracksJob,
  TrackListQuery,
  TrackMetadataRecord,
  TrackMutationTarget,
  TrackSummary,
  TrackTagRevision,
} from "@/features/music/domain/models";

export class HttpMusicAdminGateway implements MusicAdminGateway {
  listTracks(query: TrackListQuery, signal?: AbortSignal): Promise<MusicPage<TrackSummary>> {
    return adminApi.tracks(query, signal);
  }

  getTrackMetadata(trackId: string, signal?: AbortSignal): Promise<TrackMetadataRecord> {
    return adminApi.trackMetadata(trackId, signal);
  }

  listTagHistory(trackId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<MusicPage<TrackTagRevision>> {
    return adminApi.tagHistory(trackId, { page, pageSize }, signal);
  }

  updateTrackMetadata(trackId: string, command: UpdateTrackMetadataCommand): Promise<TrackMetadataRecord> {
    return adminApi.updateTrackMetadata(trackId, command);
  }

  async setTrackState(trackId: string, expectedVersion: number, action: "publish" | "archive" | "restore"): Promise<void> {
    if (action === "publish") await adminApi.publishTrack(trackId, expectedVersion);
    else if (action === "restore") await adminApi.restoreTrack(trackId, expectedVersion);
    else await adminApi.archiveTrack(trackId, expectedVersion);
  }

  batchRestoreTracks(items: TrackMutationTarget[]): Promise<BatchRestoreTracksResult> {
    return adminApi.batchRestoreTracks(items);
  }

  createPermanentDeleteTracksJob(items: TrackMutationTarget[]): Promise<PermanentDeleteTracksJob> {
    return adminApi.createPermanentDeleteTracksJob(items);
  }

  getPermanentDeleteTracksJob(jobId: string, signal?: AbortSignal): Promise<PermanentDeleteTracksJob> {
    return adminApi.permanentDeleteTracksJob(jobId, signal);
  }

  deleteTrackPermanently(trackId: string, expectedVersion: number) {
    return adminApi.deleteTrackPermanently(trackId, expectedVersion);
  }

  async writeTrackMetadata(trackId: string, expectedVersion: number, reason: string): Promise<void> {
    await adminApi.writeTrackMetadata(trackId, expectedVersion, reason);
  }

  batchUpdateTrackMetadata(command: BatchUpdateTrackMetadataCommand): Promise<BatchUpdateTrackMetadataResult> {
    return adminApi.bulkUpdateTracks(command.items, command.patch, command.reason);
  }

  restoreTagRevision(trackId: string, revisionId: string, expectedVersion: number, reason: string): Promise<TrackMetadataRecord> {
    return adminApi.restoreTagRevision(trackId, revisionId, expectedVersion, reason);
  }

  listAlbums(query: MusicListQuery, signal?: AbortSignal): Promise<MusicPage<AlbumSummary>> {
    return adminApi.albums(query, signal);
  }

  getAlbum(albumId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<AlbumDetail> {
    return adminApi.album(albumId, { page, pageSize }, signal);
  }

  getAlbumDuplicates(query: AlbumDuplicateQuery, signal?: AbortSignal): Promise<AlbumDuplicateSummary> {
    return adminApi.albumDuplicates(query, signal);
  }

  async updateAlbum(albumId: string, command: UpdateAlbumCommand): Promise<void> {
    await adminApi.updateAlbum(albumId, command);
  }

  mergeAlbums(command: MergeAlbumsCommand): Promise<AlbumMergeResult> {
    return adminApi.mergeAlbums(command);
  }

  listArtists(query: MusicListQuery, signal?: AbortSignal): Promise<MusicPage<ArtistSummary>> {
    return adminApi.artists(query, signal);
  }

  async updateArtist(artistId: string, command: UpdateArtistCommand): Promise<void> {
    await adminApi.updateArtist(artistId, command);
  }
}
