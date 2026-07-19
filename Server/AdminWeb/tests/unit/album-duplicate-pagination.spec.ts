import { describe, expect, it, vi } from "vitest";
import type { MusicAdminGateway } from "@/features/music/application/music-admin-gateway";
import { MusicAdminUseCases } from "@/features/music/application/music-admin-use-cases";
import type { AlbumDuplicateGroup, AlbumDuplicateSummary, AlbumSummary } from "@/features/music/domain/models";

function album(id: string): AlbumSummary {
  return { id } as AlbumSummary;
}

function response(albumPage: number, albums: AlbumSummary[], albumTotal = 3, albumTotalPages = 2): AlbumDuplicateSummary {
  const group: AlbumDuplicateGroup = {
    key: "same",
    title: "Same",
    primaryArtists: [],
    albums,
    albumPage,
    albumPageSize: 100,
    albumTotal,
    albumTotalPages,
  };
  return { groupCount: 1, duplicateAlbumCount: 2, groups: [group], page: 1, pageSize: 1, total: 1, totalPages: 1 };
}

describe("album duplicate member pagination", () => {
  it("loads every bounded member page before returning merge candidates", async () => {
    const getAlbumDuplicates = vi.fn()
      .mockResolvedValueOnce(response(1, [album("album-2"), album("album-3")]))
      .mockResolvedValueOnce(response(2, [album("album-1")]));
    const useCases = new MusicAdminUseCases({ getAlbumDuplicates } as unknown as MusicAdminGateway);

    const group = await useCases.getCompleteAlbumDuplicateGroup("album-1");

    expect(group?.albums.map((item) => item.id)).toEqual(["album-2", "album-3", "album-1"]);
    expect(getAlbumDuplicates).toHaveBeenNthCalledWith(1, {
      page: 1, pageSize: 1, albumId: "album-1", albumPage: 1, albumPageSize: 100,
    }, undefined);
    expect(getAlbumDuplicates).toHaveBeenNthCalledWith(2, {
      page: 1, pageSize: 1, albumId: "album-1", albumPage: 2, albumPageSize: 100,
    }, undefined);
  });

  it("fails instead of silently returning an incomplete capped group", async () => {
    const getAlbumDuplicates = vi.fn().mockResolvedValue(response(1, [album("album-1")], 3, 1));
    const useCases = new MusicAdminUseCases({ getAlbumDuplicates } as unknown as MusicAdminGateway);

    await expect(useCases.getCompleteAlbumDuplicateGroup("album-1")).rejects.toThrow("未执行部分合并");
  });
});
