import { mount } from "@vue/test-utils";
import { toRaw } from "vue";
import { describe, expect, it } from "vitest";
import type { Track } from "../src/domain/music";
import TrackTable from "../src/presentation/components/TrackTable.vue";

describe("track table", () => {
  it("keeps memoized rows responsive to current-track and favorite changes", async () => {
    const tracks = [track("track-1", false), track("track-2", false)];
    const wrapper = mount(TrackTable, { props: { tracks } });

    await wrapper.setProps({ currentId: "track-2" });
    const rows = wrapper.findAll(".track-row:not(.track-header)");
    expect(rows[0]?.classes()).not.toContain("current");
    expect(rows[1]?.classes()).toContain("current");

    tracks[0]!.liked = true;
    await wrapper.setProps({ tracks: [...tracks] });
    expect(wrapper.get('button[aria-pressed="true"]').exists()).toBe(true);

    const replacement = { ...tracks[0]! };
    await wrapper.setProps({ tracks: [replacement, tracks[1]!] });
    await wrapper.findAll(".track-row:not(.track-header)")[0]!.trigger("click");
    expect(toRaw(wrapper.emitted("play")?.at(-1)?.[0] as Track)).toBe(replacement);

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
    publishedAt: "2024-01-01T00:00:00.000Z",
  };
}
