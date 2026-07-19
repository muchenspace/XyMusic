import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import AppSidebar from "../src/presentation/components/AppSidebar.vue";
import SearchView from "../src/presentation/views/SearchView.vue";

describe("catalog entry points", () => {
  it("removes album and artist list navigation from the sidebar", () => {
    const wrapper = mount(AppSidebar, {
      props: {
        active: "discover",
        playlists: [],
        user: {
          id: "user-1",
          username: "listener",
          displayName: "Listener",
          bio: null,
          role: "USER",
          version: 1,
        },
      },
    });

    const navigationTitles = wrapper.findAll("button.nav-item").map((button) => button.attributes("title"));
    expect(navigationTitles).toContain("最近播放");
    expect(navigationTitles).toContain("喜欢的音乐");
    expect(navigationTitles).not.toContain("专辑");
    expect(navigationTitles).not.toContain("歌手");
  });

  it("keeps album and artist results reachable through search", async () => {
    const album = {
      id: "album-1",
      title: "Album",
      artist: "Artist",
      artistIds: ["artist-1"],
      coverUrl: "",
      trackCount: 1,
      accent: "#123456",
    };
    const artist = { id: "artist-1", name: "Artist" };
    const wrapper = mount(SearchView, {
      props: {
        query: "artist",
        resultsQuery: "artist",
        results: { tracks: [], albums: [album], artists: [artist] },
        searching: false,
        loadingMore: null,
        isPlaying: false,
      },
      global: {
        stubs: {
          TrackTable: true,
          AlbumRow: {
            props: ["albums"],
            emits: ["open", "play"],
            template: '<button class="search-album" @click="$emit(\'open\', albums[0])">album</button>',
          },
          ArtistGrid: {
            props: ["artists"],
            emits: ["open"],
            template: '<button class="search-artist" @click="$emit(\'open\', artists[0])">artist</button>',
          },
        },
      },
    });

    await wrapper.get(".search-album").trigger("click");
    await wrapper.get(".search-artist").trigger("click");

    expect(wrapper.emitted("openAlbum")?.[0]).toEqual([album]);
    expect(wrapper.emitted("openArtist")?.[0]).toEqual([artist]);
  });
});
