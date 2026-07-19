import type {
  BatchUpdateTrackMetadataResult,
  MergeAlbumsCommand,
  MusicAdminGateway,
  UpdateAlbumCommand,
  UpdateArtistCommand,
  UpdateTrackMetadataCommand,
} from "@/features/music/application/music-admin-gateway";
import {
  toMetadataUpdateTargets,
  type AlbumDetail,
  type AlbumDuplicateGroup,
  type AlbumDuplicateQuery,
  type AlbumDuplicateSummary,
  type AlbumMergeResult,
  type AlbumSummary,
  type ArtistSummary,
  type BatchRestoreTracksResult,
  type MusicListQuery,
  type MusicPage,
  type PermanentDeleteTracksJob,
  type TrackListQuery,
  type TrackMetadataRecord,
  type TrackMutationTarget,
  type TrackSummary,
  type TrackTagPatch,
  type TrackTagRevision,
} from "@/features/music/domain/models";

export class MusicAdminUseCases {
  constructor(private readonly gateway: MusicAdminGateway) {}

  listTracks(query: TrackListQuery, signal?: AbortSignal): Promise<MusicPage<TrackSummary>> {
    return this.gateway.listTracks(query, signal);
  }

  getTrackMetadata(trackId: string, signal?: AbortSignal): Promise<TrackMetadataRecord> {
    return this.gateway.getTrackMetadata(trackId, signal);
  }

  listTagHistory(trackId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<MusicPage<TrackTagRevision>> {
    return this.gateway.listTagHistory(trackId, page, pageSize, signal);
  }

  updateTrackMetadata(trackId: string, command: UpdateTrackMetadataCommand): Promise<TrackMetadataRecord> {
    return this.gateway.updateTrackMetadata(trackId, command);
  }

  setTrackState(trackId: string, expectedVersion: number, action: "publish" | "archive" | "restore"): Promise<void> {
    return this.gateway.setTrackState(trackId, expectedVersion, action);
  }

  batchRestoreTracks(tracks: readonly TrackSummary[]): Promise<BatchRestoreTracksResult> {
    return this.gateway.batchRestoreTracks(trackMutationTargets(tracks));
  }

  createPermanentDeleteTracksJob(tracks: readonly TrackSummary[]): Promise<PermanentDeleteTracksJob> {
    return this.gateway.createPermanentDeleteTracksJob(trackMutationTargets(tracks));
  }

  getPermanentDeleteTracksJob(jobId: string, signal?: AbortSignal): Promise<PermanentDeleteTracksJob> {
    if (!jobId.trim()) throw new Error("永久删除任务 ID 无效");
    return this.gateway.getPermanentDeleteTracksJob(jobId, signal);
  }

  deleteTrackPermanently(trackId: string, expectedVersion: number) {
    if (!trackId || !Number.isSafeInteger(expectedVersion) || expectedVersion < 1) throw new Error("曲目版本无效，请刷新后重试");
    return this.gateway.deleteTrackPermanently(trackId, expectedVersion);
  }

  writeTrackMetadata(trackId: string, expectedVersion: number, reason: string): Promise<void> {
    return this.gateway.writeTrackMetadata(trackId, expectedVersion, reason);
  }

  batchUpdateTrackMetadata(tracks: readonly TrackSummary[], patch: TrackTagPatch, reason: string): Promise<BatchUpdateTrackMetadataResult> {
    return this.gateway.batchUpdateTrackMetadata({
      items: toMetadataUpdateTargets(tracks),
      patch,
      reason,
    });
  }

  restoreTagRevision(trackId: string, revisionId: string, expectedVersion: number, reason: string): Promise<TrackMetadataRecord> {
    return this.gateway.restoreTagRevision(trackId, revisionId, expectedVersion, reason);
  }

  listAlbums(query: MusicListQuery, signal?: AbortSignal): Promise<MusicPage<AlbumSummary>> {
    return this.gateway.listAlbums(query, signal);
  }

  getAlbum(albumId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<AlbumDetail> {
    return this.gateway.getAlbum(albumId, page, pageSize, signal);
  }

  getAlbumDuplicates(query: AlbumDuplicateQuery, signal?: AbortSignal): Promise<AlbumDuplicateSummary> {
    return this.gateway.getAlbumDuplicates(query, signal);
  }

  async getCompleteAlbumDuplicateGroup(albumId: string, signal?: AbortSignal): Promise<AlbumDuplicateGroup | undefined> {
    const albumPageSize = 100;
    const albums = new Map<string, AlbumDuplicateGroup["albums"][number]>();
    let albumPage = 1;
    let firstGroup: AlbumDuplicateGroup | undefined;
    for (;;) {
      const result = await this.gateway.getAlbumDuplicates({
        page: 1,
        pageSize: 1,
        albumId,
        albumPage,
        albumPageSize,
      }, signal);
      const group = result.groups[0];
      if (!group) {
        if (albumPage === 1) return undefined;
        throw new Error("同名专辑组在加载期间发生变化，请刷新后重试");
      }
      firstGroup ??= group;
      const before = albums.size;
      for (const album of group.albums) albums.set(album.id, album);
      if (albums.size >= group.albumTotal) {
        return { ...firstGroup, albums: [...albums.values()], albumTotal: group.albumTotal };
      }
      if (albums.size === before || albumPage >= group.albumTotalPages) {
        throw new Error(`同名专辑组共有 ${group.albumTotal} 张专辑，当前分页范围只能完整加载 ${albums.size} 张，未执行部分合并`);
      }
      albumPage += 1;
    }
  }

  updateAlbum(albumId: string, command: UpdateAlbumCommand): Promise<void> {
    return this.gateway.updateAlbum(albumId, command);
  }

  mergeAlbums(command: MergeAlbumsCommand): Promise<AlbumMergeResult> {
    return this.gateway.mergeAlbums(command);
  }

  listArtists(query: MusicListQuery, signal?: AbortSignal): Promise<MusicPage<ArtistSummary>> {
    return this.gateway.listArtists(query, signal);
  }

  updateArtist(artistId: string, command: UpdateArtistCommand): Promise<void> {
    return this.gateway.updateArtist(artistId, command);
  }
}

const MAX_BATCH_TRACK_MUTATIONS = 200;

function trackMutationTargets(tracks: readonly TrackSummary[]): TrackMutationTarget[] {
  if (!tracks.length) throw new Error("请先选择曲目");
  if (tracks.length > MAX_BATCH_TRACK_MUTATIONS) throw new Error(`一次最多处理 ${MAX_BATCH_TRACK_MUTATIONS} 首曲目`);
  const seen = new Set<string>();
  return tracks.map((track) => {
    if (track.status !== "ARCHIVED") throw new Error(`曲目“${track.title}”不是已归档状态，请刷新后重试`);
    if (!track.id || !Number.isSafeInteger(track.version) || track.version < 1) throw new Error(`曲目“${track.title}”版本无效，请刷新后重试`);
    if (seen.has(track.id)) throw new Error(`曲目“${track.title}”被重复选择`);
    seen.add(track.id);
    return { trackId: track.id, expectedVersion: track.version };
  });
}
