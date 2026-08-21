import { mount } from "@vue/test-utils";
import { toRaw } from "vue";
import { describe, expect, it } from "vitest";
import type { PlaylistEntry, Track } from "../src/domain/music";
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

  it("resets all pointer drag state before a second reorder", async () => {
    const tracks = [track("track-1", false), track("track-2", false), track("track-3", false)];
    const entries = tracks.map((item, index) => playlistEntry(`entry-${index + 1}`, index, item));
    const wrapper = mount(TrackTable, { props: { tracks, entries } });

    const firstHandle = wrapper.findAll(".drag-handle")[0]!;
    await firstHandle.trigger("pointerdown", { pointerType: "mouse", button: 0, pointerId: 1, clientY: 0 });
    await firstHandle.trigger("pointermove", { pointerType: "mouse", pointerId: 1, clientY: 140 });
    await firstHandle.trigger("pointerup", { pointerType: "mouse", pointerId: 1, clientY: 140 });
    await new Promise((resolve) => setTimeout(resolve, 220));

    const secondHandle = wrapper.findAll(".drag-handle")[0]!;
    await secondHandle.trigger("pointerdown", { pointerType: "mouse", button: 0, pointerId: 2, clientY: 140 });
    await secondHandle.trigger("pointermove", { pointerType: "mouse", pointerId: 2, clientY: 0 });
    await secondHandle.trigger("pointerup", { pointerType: "mouse", pointerId: 2, clientY: 0 });
    await new Promise((resolve) => setTimeout(resolve, 220));

    expect(wrapper.findAll(".track-row:not(.track-header)").every((row) => !row.classes("dragging"))).toBe(true);
    expect(wrapper.findAll(".track-row:not(.track-header)")[0]?.attributes("style") ?? "").not.toContain("--track-drag-offset-y");
    expect(wrapper.emitted("reorder")).toHaveLength(2);

    wrapper.unmount();
  });
  it("moves the original row with pointer capture and commits the animated visible order", async () => {
    const tracks = [track("track-1", false), track("track-2", false), track("track-3", false)];
    const entries = tracks.map((item, index) => playlistEntry(`entry-${index + 1}`, index, item));
    const wrapper = mount(TrackTable, { props: { tracks, entries } });
    const handle = wrapper.findAll(".drag-handle")[0]!;

    await handle.trigger("pointerdown", { pointerType: "mouse", button: 0, pointerId: 1, clientY: 0 });
    await handle.trigger("pointermove", { pointerType: "mouse", pointerId: 1, clientY: 140 });

    expect(wrapper.findAll(".track-main-title").map((title) => title.text()))
      .toEqual(["track-2", "track-3", "track-1"]);
    expect(wrapper.findAll(".track-row:not(.track-header)")[2]!.classes()).toContain("dragging");
    expect(wrapper.findAll(".track-row:not(.track-header)")[2]!.attributes("style"))
      .toContain("--track-drag-offset-y");

    await handle.trigger("pointerup", { pointerType: "mouse", pointerId: 1, clientY: 140 });
    await new Promise((resolve) => setTimeout(resolve, 220));

    expect(wrapper.findAll(".track-row:not(.track-header)")[2]!.classes()).not.toContain("dragging");
    expect(wrapper.findAll(".track-row:not(.track-header)")[2]!.attributes("style") ?? "")
      .not.toContain("--track-drag-offset-y");
    expect(wrapper.emitted("reorder")).toEqual([[["entry-2", "entry-3", "entry-1"]]]);
    expect(wrapper.findAll(".track-main-title").map((title) => title.text()))
      .toEqual(["track-2", "track-3", "track-1"]);

    wrapper.unmount();
  });
});

function playlistEntry(id: string, position: number, item: Track): PlaylistEntry {
  return { id, position, track: item };
}

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
