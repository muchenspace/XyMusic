import { createPinia } from "pinia";
import { createApp, defineComponent, h } from "vue";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationServices } from "../src/application/services";
import type { Playlist, Track } from "../src/domain/music";
import { applicationServicesKey } from "../src/presentation/services";
import { useLibraryStore } from "../src/presentation/stores/libraryStore";

describe("library list cache", () => {
  it("invalidates an empty favorites cache after a track is favorited elsewhere", async () => {
    const favorites = vi.fn()
      .mockResolvedValueOnce({ items: [], nextCursor: null })
      .mockResolvedValueOnce({ items: [track()], nextCursor: null });
    const services = {
      catalog: {},
      library: { favorites },
      playlists: {},
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

    await store.navigate("favorites");
    await store.navigate("discover");
    store.setFavorite("track-1", true);
    await store.navigate("favorites");

    expect(favorites).toHaveBeenCalledTimes(2);
    expect(store.tracks).toMatchObject([{ id: "track-1", liked: true }]);
    app.unmount();
  });

  it("always refreshes recent playback because history changes outside the view", async () => {
    const history = vi.fn()
      .mockResolvedValueOnce({ items: [], nextCursor: null })
      .mockResolvedValueOnce({ items: [track()], nextCursor: null });
    const services = {
      catalog: {},
      library: { history },
      playlists: {},
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

    await store.navigate("recent");
    await store.navigate("discover");
    await store.navigate("recent");

    expect(history).toHaveBeenCalledTimes(2);
    expect(store.tracks).toMatchObject([{ id: "track-1" }]);
    app.unmount();
  });

  it("keeps a newly created playlist when switching through cached library views", async () => {
    const favorites = vi.fn().mockResolvedValue({ items: [], nextCursor: null });
    const created = playlist("playlist-new", "New Playlist");
    const create = vi.fn().mockResolvedValue(created);
    const services = {
      catalog: {},
      library: { favorites },
      playlists: { create },
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

    store.setPlaylists([playlist("playlist-existing", "Existing Playlist")]);
    await store.navigate("favorites");
    await store.navigate("discover");
    await store.createPlaylist("New Playlist", "", "PRIVATE");
    await store.navigate("favorites");
    await store.navigate("discover");

    expect(favorites).toHaveBeenCalledTimes(1);
    expect(create).toHaveBeenCalledWith("New Playlist", "", "PRIVATE");
    expect(store.playlists.map((item) => item.id)).toEqual(["playlist-new", "playlist-existing"]);
    app.unmount();
  });
});

function track(): Track {
  return {
    id: "track-1",
    title: "Track",
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    albumId: "album-1",
    coverUrl: "",
    duration: 60,
    liked: true,
    publishedAt: "2026-07-17T00:00:00Z",
  };
}

function playlist(id: string, title: string): Playlist {
  return {
    id,
    title,
    description: "",
    coverUrl: "",
    trackCount: 0,
    accent: "",
    version: 1,
    visibility: "PRIVATE",
  };
}
