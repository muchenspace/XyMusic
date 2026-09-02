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
  BatchArchiveTracksResult,
  BatchRestoreTracksResult,
  MusicListQuery,
  MusicPage,
  MetadataLyrics,
  PermanentDeleteTracksJob,
  TrackListQuery,
  TrackMetadataRecord,
  TrackMutationTarget,
  TrackSummary,
} from "@/features/music/domain/models";

export class HttpMusicAdminGateway implements MusicAdminGateway {
  listTracks(query: TrackListQuery, signal?: AbortSignal): Promise<MusicPage<TrackSummary>> {
    return adminApi.tracks(query, signal);
  }

  async getTrackMetadata(trackId: string, signal?: AbortSignal): Promise<TrackMetadataRecord> {
    return validateTrackMetadataRecord(await adminApi.trackMetadata(trackId, signal));
  }

  async updateTrackMetadata(trackId: string, command: UpdateTrackMetadataCommand): Promise<TrackMetadataRecord> {
    return validateTrackMetadataRecord(await adminApi.updateTrackMetadata(trackId, command));
  }

  async setTrackState(trackId: string, expectedVersion: number, action: "publish" | "archive" | "restore"): Promise<void> {
    if (action === "publish") await adminApi.publishTrack(trackId, expectedVersion);
    else if (action === "restore") await adminApi.restoreTrack(trackId, expectedVersion);
    else await adminApi.archiveTrack(trackId, expectedVersion);
  }

  batchRestoreTracks(items: TrackMutationTarget[]): Promise<BatchRestoreTracksResult> {
    return adminApi.batchRestoreTracks(items);
  }

  batchArchiveTracks(items: TrackMutationTarget[]): Promise<BatchArchiveTracksResult> {
    return adminApi.batchArchiveTracks(items);
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

  async writeTrackMetadata(trackId: string, expectedVersion: number, reason = ""): Promise<void> {
    await adminApi.writeTrackMetadata(trackId, expectedVersion, reason);
  }

  batchUpdateTrackMetadata(command: BatchUpdateTrackMetadataCommand): Promise<BatchUpdateTrackMetadataResult> {
    return adminApi.bulkUpdateTracks(command.items, command.patch, command.reason ?? "");
  }

  listAlbums(query: MusicListQuery, signal?: AbortSignal): Promise<MusicPage<AlbumSummary>> {
    return adminApi.albums(query, signal);
  }

  getAlbum(albumId: string, query: Pick<MusicListQuery, "page" | "pageSize" | "cursor" | "cursorMode">, signal?: AbortSignal): Promise<AlbumDetail> {
    return adminApi.album(albumId, query, signal);
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

function validateTrackMetadataRecord(record: TrackMetadataRecord): TrackMetadataRecord {
  validateLyricsTiming(record.raw.lyrics);
  validateLyricsTiming(record.effective.lyrics);
  if (Object.prototype.hasOwnProperty.call(record.overrides, "lyrics")) validateLyricsTiming(record.overrides.lyrics);
  return record;
}

function validateLyricsTiming(lyrics: MetadataLyrics | null | undefined): void {
  if (lyrics && lyrics.timing !== "LINE" && lyrics.timing !== "WORD") {
    throw new Error("Track metadata lyrics timing is invalid");
  }
}
