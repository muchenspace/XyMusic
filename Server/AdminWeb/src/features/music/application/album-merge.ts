import type { MergeAlbumsCommand } from "@/features/music/application/music-admin-gateway";
import type { AlbumMergeFieldSources, AlbumSummary } from "@/features/music/domain/models";

export interface AlbumMergeDraft {
  selectedIds: string[];
  survivorId: string;
  fieldSources: AlbumMergeFieldSources;
}

export function createAlbumMergeDraft(albums: readonly AlbumSummary[], preferredAlbumId?: string): AlbumMergeDraft {
  const survivorId = albums.some((album) => album.id === preferredAlbumId)
    ? preferredAlbumId!
    : albums[0]?.id ?? "";
  return {
    selectedIds: albums.map((album) => album.id),
    survivorId,
    fieldSources: {
      title: survivorId,
      cover: survivorId,
      artistCredits: survivorId,
      releaseDate: survivorId,
      description: survivorId,
    },
  };
}

export function buildAlbumMergeCommand(
  albums: readonly AlbumSummary[],
  draft: AlbumMergeDraft,
): MergeAlbumsCommand {
  const albumById = new Map(albums.map((album) => [album.id, album]));
  const selectedIds = [...new Set(draft.selectedIds)];
  const selected = new Set(selectedIds);
  if (selectedIds.length < 2) throw new Error("至少选择两个专辑参与合并");
  if (!selected.has(draft.survivorId)) throw new Error("请选择一个参与合并的保留专辑");
  const requiredSources = [draft.fieldSources.title, draft.fieldSources.artistCredits];
  const nullableSources = [
    draft.fieldSources.cover,
    draft.fieldSources.releaseDate,
    draft.fieldSources.description,
  ].filter((albumId): albumId is string => albumId !== null);
  if ([...requiredSources, ...nullableSources].some((albumId) => !selected.has(albumId))) {
    throw new Error("字段来源必须是参与合并的专辑");
  }
  const selectedAlbums = selectedIds.map((id) => albumById.get(id));
  if (selectedAlbums.some((album) => !album)) throw new Error("合并列表包含已不存在的专辑，请刷新后重试");
  const survivor = albumById.get(draft.survivorId)!;
  return {
    target: { albumId: survivor.id, expectedVersion: survivor.version },
    sources: selectedAlbums
      .filter((album): album is AlbumSummary => album !== undefined && album.id !== survivor.id)
      .map((album) => ({ albumId: album.id, expectedVersion: album.version })),
    fieldSources: { ...draft.fieldSources },
  };
}
