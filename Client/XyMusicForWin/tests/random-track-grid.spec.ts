import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import type { Track } from "../src/domain/music";
import RandomTrackGrid from "../src/presentation/components/RandomTrackGrid.vue";

describe("random track grid", () => {
  it("updates the favorite icon when a track is mutated in place", async () => {
    const tracks = [track("track-1", false)];
    const wrapper = mount(RandomTrackGrid, { props: { tracks, isPlaying: false } });

    expect(wrapper.get('button[aria-pressed="false"]').exists()).toBe(true);

    tracks[0]!.liked = true;
    await wrapper.setProps({ tracks: [...tracks] });

    expect(wrapper.get('button[aria-pressed="true"]').exists()).toBe(true);
    expect(wrapper.get('button[title="取消收藏《track-1》"]').exists()).toBe(true);

    wrapper.unmount();
  });
});

function track(id: string, liked: boolean): Track {
  return {
    id,
    title: id,
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    coverUrl: "",
    duration: 180,
    liked,
    publishedAt: "2026-01-01T00:00:00.000Z",
  };
}
