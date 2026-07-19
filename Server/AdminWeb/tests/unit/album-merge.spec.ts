import { describe, expect, it } from "vitest";
import { buildAlbumMergeCommand, createAlbumMergeDraft } from "@/features/music/application/album-merge";
import type { AlbumSummary } from "@/features/music/domain/models";

function album(id: string, version: number): AlbumSummary {
  return {
    id,
    version,
    title: "Same title",
    artistCredits: [],
    artwork: null,
    releaseDate: null,
    description: null,
    trackCount: 0,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  };
}

describe("album merge command", () => {
  const albums = [album("album-a", 2), album("album-b", 4), album("album-c", 6)];

  it("separates the survivor from version-locked sources and preserves field choices", () => {
    const draft = createAlbumMergeDraft(albums, "album-b");
    draft.selectedIds = ["album-a", "album-b"];
    draft.fieldSources.title = "album-a";
    draft.fieldSources.cover = null;
    const command = buildAlbumMergeCommand(albums, draft);
    expect(command).toEqual({
      target: { albumId: "album-b", expectedVersion: 4 },
      sources: [{ albumId: "album-a", expectedVersion: 2 }],
      fieldSources: {
        title: "album-a",
        cover: null,
        artistCredits: "album-b",
        releaseDate: "album-b",
        description: "album-b",
      },
    });
  });

  it("requires at least two participants", () => {
    const draft = createAlbumMergeDraft(albums);
    draft.selectedIds = ["album-a"];
    expect(() => buildAlbumMergeCommand(albums, draft)).toThrow("至少选择两个专辑参与合并");
  });

  it("rejects a field source that was deselected", () => {
    const draft = createAlbumMergeDraft(albums);
    draft.selectedIds = ["album-a", "album-b"];
    draft.fieldSources.description = "album-c";
    expect(() => buildAlbumMergeCommand(albums, draft)).toThrow("字段来源必须是参与合并的专辑");
  });
});
