import { createPinia } from "pinia";
import { createApp, defineComponent, h } from "vue";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { Playlist, PlaylistDetail, PlaylistEntry, Track } from "../src/domain/music";
import { applicationServicesKey } from "../src/presentation/services";
import { useLibraryStore } from "../src/presentation/stores/libraryStore";

describe("playlist newest-first order", () => {
  it("keeps paged entries newest-first and stores reordered positions in reverse", async () => {
    const playlist = playlistSummary();
    const getPage = vi.fn()
      .mockResolvedValueOnce(playlistDetail(playlist, [entry("newest", 2)], "next-page"))
      .mockResolvedValueOnce(playlistDetail(playlist, [entry("middle", 1), entry("oldest", 0)], null));
    const reorder = vi.fn().mockResolvedValue(4);
    const services = {
      catalog: {},
      library: {},
      playlists: { getPage, reorder },
    } as unknown as ApplicationServices;
    let store!: ReturnType<typeof useLibraryStore>;
    const Root = defineComponent({
      setup() {
        store = useLibraryStore();
        return () => h("div");
      },
    });
    const app = createApp(Root);
    app.use(createPinia());
    app.provide(applicationServicesKey, services);
    const element = document.createElement("div");
    app.mount(element);

    await store.openPlaylist(playlist);
    await store.loadMoreCollection();

    expect(store.selectedPlaylist?.entries.map((item) => item.id))
      .toEqual(["newest", "middle", "oldest"]);
    expect(store.tracks.map((item) => item.id))
      .toEqual(["track-newest", "track-middle", "track-oldest"]);

    await store.reorderEntries(["oldest", "newest", "middle"]);

    expect(reorder).toHaveBeenCalledWith(
      expect.objectContaining({ id: playlist.id }),
      ["oldest", "newest", "middle"],
    );
    expect(store.selectedPlaylist?.entries.map(({ id, position }) => ({ id, position })))
      .toEqual([
        { id: "oldest", position: 2 },
        { id: "newest", position: 1 },
        { id: "middle", position: 0 },
      ]);
    app.unmount();
  });
});

function playlistSummary(): Playlist {
  return {
    id: "playlist-1",
    title: "Playlist",
    description: "",
    coverUrl: "",
    trackCount: 3,
    accent: "#000000",
    version: 3,
    visibility: "PRIVATE",
  };
}

function playlistDetail(playlist: Playlist, entries: PlaylistEntry[], nextCursor: string | null): PlaylistDetail {
  return { ...playlist, entries, nextCursor };
}

function entry(id: string, position: number): PlaylistEntry {
  return { id, position, track: track(`track-${id}`) };
}

function track(id: string): Track {
  return {
    id,
    title: id,
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    albumId: "album-1",
    coverUrl: "",
    duration: 180,
    liked: false,
    publishedAt: "2026-01-01T00:00:00Z",
  };
}
