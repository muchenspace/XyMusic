import { flushPromises, mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import DesktopLyricsApp from "../src/desktop-lyrics/DesktopLyricsApp.vue";
import type { DesktopLyricsBridge } from "../src/desktop-lyrics/bridge";
import type { DesktopLyricsClockPayload, DesktopLyricsStatePayload } from "../src/desktop-lyrics/protocol";

describe("desktop lyrics window UI", () => {
  it("requests initial state, renders two lines, ignores stale clocks, and can lock", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    let clockListener!: (clock: DesktopLyricsClockPayload) => void;
    const emitAction = vi.fn(async () => undefined);
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock(listener) { clockListener = listener; return () => undefined; },
      emitAction,
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    expect(emitAction).toHaveBeenCalledWith(expect.objectContaining({ version: 2, action: "ready" }));

    stateListener({
      version: 2,
      track: { id: "track-1", title: "Song", artist: "Artist" },
      lyrics: {
        trackId: "track-1",
        source: "lrc",
        synchronized: true,
        lines: [
          { time: 0, text: "first line" },
          { time: 2, text: "second line" },
          { time: 4, text: "third line" },
        ],
      },
      isPlaying: false,
      positionSeconds: 0.5,
      anchoredAtMs: Date.now(),
      offsetSeconds: 0,
      showTranslation: true,
      wordLyricsEnabled: true,
      locked: false,
      fontScale: 1,
    });
    await nextTick();
    expect(wrapper.text()).toContain("first line");
    expect(wrapper.text()).toContain("second line");
    expect(wrapper.get(".desktop-lyric-fill").attributes("style")).toContain("25%");

    clockListener({ version: 2, trackId: "other", isPlaying: false, positionSeconds: 4.5, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("first line");

    clockListener({ version: 2, trackId: "track-1", isPlaying: false, positionSeconds: 2.5, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("second line");
    expect(wrapper.text()).toContain("third line");

    clockListener({ version: 2, trackId: "track-1", isPlaying: false, positionSeconds: 8, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("third line");
    expect(wrapper.get(".desktop-lyric-fill").attributes("style")).toContain("0%");
    expect(wrapper.get(".desktop-lyric-line-current").classes()).not.toContain("has-started");

    await wrapper.get('button[aria-label="增大桌面歌词字号"]').trigger("click");
    expect(emitAction).toHaveBeenLastCalledWith(expect.objectContaining({ action: "set-font-scale", value: 1.05 }));
    await wrapper.get('input[aria-label="高亮文字颜色"]').setValue("#ff3366");
    expect(emitAction).toHaveBeenLastCalledWith(expect.objectContaining({ action: "set-highlight-color", value: "#ff3366" }));

    await wrapper.get('button[aria-label="锁定桌面歌词"]').trigger("click");
    expect(emitAction).toHaveBeenLastCalledWith(expect.objectContaining({ action: "lock", locked: true }));

    wrapper.unmount();
  });

  it("highlights the whole current line when desktop word lyrics are disabled", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock() { return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    stateListener({
      version: 2,
      track: { id: "track-1", title: "Song", artist: "Artist" },
      lyrics: {
        trackId: "track-1",
        source: "lrc",
        synchronized: true,
        lines: [
          { time: 1, text: "first line" },
          { time: 3, text: "second line" },
        ],
      },
      isPlaying: false,
      positionSeconds: 2,
      anchoredAtMs: Date.now(),
      offsetSeconds: 0,
      showTranslation: false,
      wordLyricsEnabled: false,
      locked: false,
      fontScale: 1,
    });
    await nextTick();

    expect(wrapper.get(".desktop-lyric-fill").attributes("style")).toContain("100%");
    wrapper.unmount();
  });
});
