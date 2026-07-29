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

    expect(emitAction).toHaveBeenCalledWith(expect.objectContaining({ version: 3, action: "ready" }));

    stateListener({
      version: 3,
      track: { id: "track-1", title: "Song", artist: "Artist" },
      lyrics: {
        trackId: "track-1",
        source: "lrc",
        synchronized: true,
        timing: "LINE",
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
      locked: false,
      fontScale: 1,
    });
    await nextTick();
    expect(wrapper.text()).toContain("first line");
    expect(wrapper.text()).toContain("second line");
    expect(wrapper.findAll(".desktop-lyric-fill")).toHaveLength(0);

    clockListener({ version: 3, trackId: "other", isPlaying: false, positionSeconds: 4.5, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("first line");

    clockListener({ version: 3, trackId: "track-1", isPlaying: false, positionSeconds: 2.5, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("second line");
    expect(wrapper.text()).toContain("third line");

    clockListener({ version: 3, trackId: "track-1", isPlaying: false, positionSeconds: 8, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("third line");
    expect(wrapper.findAll(".desktop-lyric-fill")).toHaveLength(0);
    expect(wrapper.get(".desktop-lyric-line-current").classes()).toContain("has-started");

    await wrapper.get('button[aria-label="增大桌面歌词字号"]').trigger("click");
    expect(emitAction).toHaveBeenLastCalledWith(expect.objectContaining({ action: "set-font-scale", value: 1.05 }));
    await wrapper.get('input[aria-label="高亮文字颜色"]').setValue("#ff3366");
    expect(emitAction).toHaveBeenLastCalledWith(expect.objectContaining({ action: "set-highlight-color", value: "#ff3366" }));

    await wrapper.get('button[aria-label="锁定桌面歌词"]').trigger("click");
    expect(emitAction).toHaveBeenLastCalledWith(expect.objectContaining({ action: "lock", locked: true }));

    wrapper.unmount();
  });

  it("renders explicit desktop word timing without line progress", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    let clockListener!: (clock: DesktopLyricsClockPayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock(listener) { clockListener = listener; return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    stateListener({
      version: 3,
      track: { id: "track-1", title: "Song", artist: "Artist" },
      lyrics: {
        trackId: "track-1",
        source: "lrc",
        synchronized: true,
        timing: "WORD",
        lines: [
          {
            time: 1,
            text: "first line",
            words: [{ time: 1, endTime: 1.5, text: "first" }, { time: 1.5, endTime: 2.5, text: " line" }],
          },
          { time: 3, text: "second line" },
        ],
      },
      isPlaying: false,
      positionSeconds: 1.2,
      anchoredAtMs: Date.now(),
      offsetSeconds: 0,
      showTranslation: false,
      locked: false,
      fontScale: 1,
    });
    await nextTick();

    expect(wrapper.findAll(".desktop-lyric-progress")).toHaveLength(0);
    expect(wrapper.findAll(".desktop-lyric-words")).toHaveLength(1);
    expect(wrapper.findAll(".desktop-lyric-word.is-sung")).toHaveLength(1);
    expect(wrapper.findAll(".desktop-lyric-word")[0]?.attributes("style")).toContain("--desktop-lyric-word-progress: 40%;");
    clockListener({ version: 3, trackId: "track-1", isPlaying: false, positionSeconds: 2, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.findAll(".desktop-lyric-word.is-sung")).toHaveLength(2);
    wrapper.unmount();
  });

  it("clears stale lyrics and clock state on a snapshot protocol mismatch", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock() { return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    stateListener(desktopLyricsState({ revision: 10, positionSeconds: 2.5, anchoredAtMs: 200 }));
    await nextTick();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("second line");

    stateListener({
      ...desktopLyricsState({ revision: 11 }),
      version: 2 as DesktopLyricsStatePayload["version"],
    });
    await nextTick();

    expect(wrapper.find(".desktop-lyric-line-current").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Song");

    stateListener(desktopLyricsState({ revision: 1, positionSeconds: 0.5, anchoredAtMs: 100 }));
    await nextTick();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("first line");

    wrapper.unmount();
  });

  it("clears stale lyrics and clock state on a clock protocol mismatch", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    let clockListener!: (clock: DesktopLyricsClockPayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock(listener) { clockListener = listener; return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    stateListener(desktopLyricsState({ revision: 10, positionSeconds: 0.5, anchoredAtMs: 100 }));
    clockListener({ version: 3, trackId: "track-1", isPlaying: false, positionSeconds: 2.5, anchoredAtMs: 200 });
    await nextTick();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("second line");

    clockListener({
      version: 2 as DesktopLyricsClockPayload["version"],
      trackId: "track-1",
      isPlaying: false,
      positionSeconds: 4.5,
      anchoredAtMs: 300,
    });
    await nextTick();

    expect(wrapper.find(".desktop-lyric-line-current").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Song");

    stateListener(desktopLyricsState({ revision: 1, positionSeconds: 0.5, anchoredAtMs: 100 }));
    await nextTick();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("first line");

    wrapper.unmount();
  });

  it("accepts snapshot after main window restart even when revision is smaller", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock() { return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    // 模拟主窗口刷新前：歌词窗口已收到 revision 较大的旧快照（track-1）
    stateListener({
      version: 3,
      revision: 1_000_000_005,
      track: { id: "track-1", title: "Old Song", artist: "Old Artist" },
      lyrics: null,
      isPlaying: false,
      positionSeconds: 0,
      anchoredAtMs: Date.now(),
      offsetSeconds: 0,
      showTranslation: false,
      locked: false,
      fontScale: 1,
    });
    await nextTick();
    expect(wrapper.text()).toContain("Old Song");

    // 主窗口刷新后 revision 重置为小值，差值远超阈值，应接受新快照（track-2）
    stateListener({
      version: 3,
      revision: 1,
      track: { id: "track-2", title: "New Song", artist: "New Artist" },
      lyrics: null,
      isPlaying: true,
      positionSeconds: 0,
      anchoredAtMs: Date.now(),
      offsetSeconds: 0,
      showTranslation: false,
      locked: false,
      fontScale: 1,
    });
    await nextTick();
    expect(wrapper.text()).toContain("New Song");
    expect(wrapper.text()).not.toContain("Old Song");

    wrapper.unmount();
  });
});

function desktopLyricsState(overrides: Partial<DesktopLyricsStatePayload> = {}): DesktopLyricsStatePayload {
  return {
    version: 3,
    revision: 1,
    track: { id: "track-1", title: "Song", artist: "Artist" },
    lyrics: {
      trackId: "track-1",
      source: "lrc",
      synchronized: true,
      timing: "LINE",
      lines: [
        { time: 0, text: "first line" },
        { time: 2, text: "second line" },
        { time: 4, text: "third line" },
      ],
    },
    isPlaying: false,
    positionSeconds: 0.5,
    anchoredAtMs: 100,
    offsetSeconds: 0,
    showTranslation: false,
    locked: false,
    fontScale: 1,
    ...overrides,
  };
}
