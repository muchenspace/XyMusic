import { flushPromises, mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { describe, expect, it, vi } from "vitest";
import DesktopLyricsApp from "../src/desktop-lyrics/DesktopLyricsApp.vue";
import type { DesktopLyricsBridge } from "../src/desktop-lyrics/bridge";
import type { DesktopLyricsClockPayload, DesktopLyricsStatePayload } from "../src/desktop-lyrics/protocol";

const TEST_TRANSPORT_EPOCH = "test-main-window";

describe("desktop lyrics window UI", () => {
  it("runs an adjacent line rotation on the shared frame clock and settles it", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);

    try {
      let clockListener!: (clock: DesktopLyricsClockPayload) => void;
      const bridge: DesktopLyricsBridge = {
        async onState() { return () => undefined; },
        async onClock(listener) { clockListener = listener; return () => undefined; },
        async emitAction() {},
      };
      const wrapper = mount(DesktopLyricsApp, {
        props: { bridge, initialState: desktopLyricsState({ positionSeconds: 0.5 }) },
      });
      await flushPromises();
      expect(animationFrames).toHaveLength(0);

      const transitionStartedAt = performance.now();
      clockListener({
        version: 4,
        transportEpoch: TEST_TRANSPORT_EPOCH,
        revision: 2,
        trackId: "track-1",
        isPlaying: false,
        positionSeconds: 2.1,
        anchoredAtMs: 200,
      });
      await nextTick();

      expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("second line");
      expect(wrapper.get(".desktop-lyric-line-outgoing").text()).toContain("first line");
      expect(animationFrames).toHaveLength(1);

      animationFrames.shift()?.(transitionStartedAt + 600);
      await nextTick();
      expect(wrapper.find(".desktop-lyric-line-outgoing").exists()).toBe(false);
      expect(wrapper.get(".desktop-lyric-line-current").attributes("style"))
        .toContain("--desktop-lyric-line-emphasis: 1");
      wrapper.unmount();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("separates and fades a wrapped outgoing line before removing it", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
    const offsetHeight = vi.spyOn(HTMLElement.prototype, "offsetHeight", "get")
      .mockImplementation(function measuredLyricHeight() {
        if (this.textContent?.includes("wrapped source line")) return 96;
        if (this.classList.contains("desktop-lyric-line")) return 48;
        return 0;
      });
    const computedStyle = vi.spyOn(window, "getComputedStyle").mockImplementation((element) => ({
      direction: "ltr",
      rowGap: element.classList.contains("desktop-lyrics-copy") ? "8px" : "0px",
    }) as CSSStyleDeclaration);
    let wrapper: ReturnType<typeof mount> | null = null;

    try {
      let clockListener!: (clock: DesktopLyricsClockPayload) => void;
      const bridge: DesktopLyricsBridge = {
        async onState() { return () => undefined; },
        async onClock(listener) { clockListener = listener; return () => undefined; },
        async emitAction() {},
      };
      wrapper = mount(DesktopLyricsApp, {
        props: {
          bridge,
          initialState: desktopLyricsState({
            positionSeconds: 0.5,
            lyrics: {
              trackId: "track-1",
              source: "lrc",
              synchronized: true,
              timing: "LINE",
              lines: [
                { time: 0, text: "wrapped source line that occupies two visual rows" },
                { time: 2, text: "target line" },
                { time: 4, text: "following line" },
              ],
            },
          }),
        },
      });
      await flushPromises();

      const transitionStartedAt = performance.now();
      clockListener({
        version: 4,
        transportEpoch: TEST_TRANSPORT_EPOCH,
        revision: 2,
        trackId: "track-1",
        isPlaying: false,
        positionSeconds: 2.1,
        anchoredAtMs: 200,
      });
      await nextTick();
      await nextTick();

      const incoming = wrapper.get<HTMLElement>(".desktop-lyric-line-current");
      const outgoing = wrapper.get<HTMLElement>(".desktop-lyric-line-outgoing");
      const currentTopBeforeFinish = incoming.element.getBoundingClientRect().top;
      const incomingShift = Number.parseFloat(
        incoming.element.style.getPropertyValue("--desktop-lyric-transition-shift"),
      );
      const initialOpacity = Number.parseFloat(
        outgoing.element.style.getPropertyValue("--desktop-lyric-transition-opacity"),
      );
      expect(incomingShift).toBeGreaterThan(103.5);
      expect(incomingShift).toBeLessThanOrEqual(104);
      expect(initialOpacity).toBeGreaterThan(0.99);
      expect(initialOpacity).toBeLessThanOrEqual(1);

      animationFrames.shift()?.(transitionStartedAt + 150);
      await nextTick();
      const fadingOpacity = Number.parseFloat(
        wrapper.get<HTMLElement>(".desktop-lyric-line-outgoing").element.style
          .getPropertyValue("--desktop-lyric-transition-opacity"),
      );
      expect(fadingOpacity).toBeGreaterThan(0);
      expect(fadingOpacity).toBeLessThan(1);

      animationFrames.shift()?.(transitionStartedAt + 600);
      await nextTick();
      expect(wrapper.find(".desktop-lyric-line-outgoing").exists()).toBe(false);
      expect(wrapper.get<HTMLElement>(".desktop-lyric-line-current").element.getBoundingClientRect().top)
        .toBeCloseTo(currentTopBeforeFinish, 0);
    } finally {
      wrapper?.unmount();
      offsetHeight.mockRestore();
      computedStyle.mockRestore();
      vi.unstubAllGlobals();
    }
  });

  it("snaps a backwards line jump without leaving stale outgoing text", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);

    try {
      let clockListener!: (clock: DesktopLyricsClockPayload) => void;
      const bridge: DesktopLyricsBridge = {
        async onState() { return () => undefined; },
        async onClock(listener) { clockListener = listener; return () => undefined; },
        async emitAction() {},
      };
      const wrapper = mount(DesktopLyricsApp, {
        props: { bridge, initialState: desktopLyricsState({ positionSeconds: 4.5 }) },
      });
      await flushPromises();

      clockListener({
        version: 4,
        transportEpoch: TEST_TRANSPORT_EPOCH,
        revision: 2,
        trackId: "track-1",
        isPlaying: false,
        positionSeconds: 0.5,
        anchoredAtMs: 200,
      });
      await nextTick();

      expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("first line");
      expect(wrapper.find(".desktop-lyric-line-outgoing").exists()).toBe(false);
      expect(animationFrames).toHaveLength(0);
      wrapper.unmount();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("snaps a sub-250ms backwards seek when the discontinuity generation changes", async () => {
    let clockListener!: (clock: DesktopLyricsClockPayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState() { return () => undefined; },
      async onClock(listener) { clockListener = listener; return () => undefined; },
      async emitAction() {},
    };
    const initialState = desktopLyricsState({
      isPlaying: true,
      positionSeconds: 1.05,
      positionDiscontinuityVersion: 0,
      lyrics: {
        trackId: "track-1",
        source: "lrc",
        synchronized: true,
        timing: "LINE",
        lines: [
          { time: 0, text: "before boundary" },
          { time: 1, text: "after boundary" },
        ],
      },
    });
    const wrapper = mount(DesktopLyricsApp, { props: { bridge, initialState } });
    await flushPromises();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("after boundary");

    clockListener({
      version: 4,
      transportEpoch: TEST_TRANSPORT_EPOCH,
      revision: 2,
      trackId: "track-1",
      isPlaying: true,
      positionSeconds: 0.95,
      anchoredAtMs: 200,
      positionDiscontinuityVersion: 1,
    });
    await nextTick();

    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("before boundary");
    expect(wrapper.find(".desktop-lyric-line-outgoing").exists()).toBe(false);
    wrapper.unmount();
  });

  it("does not keep a frame loop for ordinary line-timed lyrics", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);

    try {
      let stateListener!: (state: DesktopLyricsStatePayload) => void;
      const bridge: DesktopLyricsBridge = {
        async onState(listener) { stateListener = listener; return () => undefined; },
        async onClock() { return () => undefined; },
        async emitAction() {},
      };
      const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
      await flushPromises();

      stateListener({
        ...desktopLyricsState({ isPlaying: true, positionSeconds: 0.5, anchoredAtMs: Date.now() }),
        lyrics: {
          trackId: "track-1",
          source: "lrc",
          synchronized: true,
          timing: "LINE",
          lines: [{ time: 0, text: "first line" }, { time: 2, text: "second line" }],
        },
      });
      await nextTick();

      expect(animationFrames).toHaveLength(0);
      wrapper.unmount();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("stops enhanced-word animation while the lyric window is not render-active", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);

    try {
      let stateListener!: (state: DesktopLyricsStatePayload) => void;
      const bridge: DesktopLyricsBridge = {
        async onState(listener) { stateListener = listener; return () => undefined; },
        async onClock() { return () => undefined; },
        async emitAction() {},
      };
      const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
      await flushPromises();
      const snapshot = {
        ...desktopLyricsState({ isPlaying: true, positionSeconds: 1.2, anchoredAtMs: Date.now() }),
        lyrics: {
          trackId: "track-1",
          source: "lrc",
          synchronized: true,
          timing: "WORD" as const,
          lines: [{
            time: 1,
            text: "first line",
            words: [{ time: 1, endTime: 2, text: "first" }],
          }],
        },
        renderActive: false,
      };

      stateListener(snapshot);
      await nextTick();
      expect(animationFrames).toHaveLength(0);

      stateListener({ ...snapshot, revision: 2, renderActive: true });
      await nextTick();
      expect(animationFrames).toHaveLength(1);
      wrapper.unmount();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("ignores a cancelled word-animation callback after a state reset", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);

    try {
      let stateListener!: (state: DesktopLyricsStatePayload) => void;
      const snapshot = {
        ...desktopLyricsState({ isPlaying: true, positionSeconds: 1.2, anchoredAtMs: Date.now() }),
        lyrics: {
          trackId: "track-1",
          source: "lrc",
          synchronized: true,
          timing: "WORD" as const,
          lines: [{
            time: 1,
            text: "first line",
            words: [{ time: 1, endTime: 2, text: "first" }],
          }],
        },
      };
      const bridge: DesktopLyricsBridge = {
        async onState(listener) { stateListener = listener; return () => undefined; },
        async onClock() { return () => undefined; },
        async emitAction() {},
      };
      const wrapper = mount(DesktopLyricsApp, { props: { bridge, initialState: snapshot } });
      await flushPromises();
      expect(animationFrames).toHaveLength(1);

      stateListener({ ...snapshot, revision: 2 });
      await nextTick();
      expect(animationFrames).toHaveLength(2);

      // Browser cancellation can race a callback already selected for delivery.
      animationFrames[0]?.(performance.now() + 16);
      expect(animationFrames).toHaveLength(2);
      wrapper.unmount();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("ignores a cancelled line-wake timer after a state reset", async () => {
    const timers: Array<() => void> = [];
    vi.stubGlobal("setTimeout", (callback: () => void) => {
      timers.push(callback);
      return timers.length;
    });
    vi.stubGlobal("clearTimeout", () => undefined);

    try {
      let stateListener!: (state: DesktopLyricsStatePayload) => void;
      const snapshot = desktopLyricsState({ isPlaying: true, positionSeconds: 0.5, anchoredAtMs: Date.now() });
      const bridge: DesktopLyricsBridge = {
        async onState(listener) { stateListener = listener; return () => undefined; },
        async onClock() { return () => undefined; },
        async emitAction() {},
      };
      const wrapper = mount(DesktopLyricsApp, { props: { bridge, initialState: snapshot } });
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
      expect(timers).toHaveLength(1);

      stateListener({ ...snapshot, revision: 2 });
      await nextTick();
      expect(timers).toHaveLength(2);

      timers[0]?.();
      expect(timers).toHaveLength(2);
      wrapper.unmount();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("releases an independently registered bridge listener after unmount", async () => {
    const stateRegistration = deferred<() => void>();
    const clockRegistration = deferred<() => void>();
    const removeStateListener = vi.fn();
    const removeClockListener = vi.fn();
    const bridge: DesktopLyricsBridge = {
      async onState() { return stateRegistration.promise; },
      async onClock() { return clockRegistration.promise; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await Promise.resolve();
    wrapper.unmount();

    stateRegistration.resolve(removeStateListener);
    await flushPromises();

    expect(removeStateListener).toHaveBeenCalledOnce();

    clockRegistration.resolve(removeClockListener);
    await flushPromises();
    expect(removeStateListener).toHaveBeenCalledOnce();
    expect(removeClockListener).toHaveBeenCalledOnce();
  });

  it("retries the ready handshake until a state snapshot arrives", async () => {
    vi.useFakeTimers();
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    const emitAction = vi.fn(async () => undefined);
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock() { return () => undefined; },
      emitAction,
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    try {
      await flushPromises();
      expect(emitAction).toHaveBeenCalledTimes(1);
      expect(emitAction).toHaveBeenLastCalledWith(expect.objectContaining({ action: "ready" }));

      await vi.advanceTimersByTimeAsync(250);
      await flushPromises();
      expect(emitAction).toHaveBeenCalledTimes(2);

      stateListener(desktopLyricsState());
      await nextTick();
      await vi.advanceTimersByTimeAsync(4_000);
      await flushPromises();
      expect(emitAction).toHaveBeenCalledTimes(2);
    } finally {
      wrapper.unmount();
      vi.useRealTimers();
    }
  });

  it("cancels a pending ready retry when the lyric window unmounts", async () => {
    vi.useFakeTimers();
    const emitAction = vi.fn(async () => undefined);
    const bridge: DesktopLyricsBridge = {
      async onState() { return () => undefined; },
      async onClock() { return () => undefined; },
      emitAction,
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    try {
      await flushPromises();
      expect(emitAction).toHaveBeenCalledTimes(1);

      wrapper.unmount();
      await vi.advanceTimersByTimeAsync(4_000);
      await flushPromises();
      expect(emitAction).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

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

    expect(emitAction).toHaveBeenCalledWith(expect.objectContaining({ version: 4, action: "ready" }));

    stateListener({
      version: 4,
      transportEpoch: TEST_TRANSPORT_EPOCH,
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

    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, trackId: "other", isPlaying: false, positionSeconds: 4.5, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("first line");

    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, trackId: "track-1", isPlaying: false, positionSeconds: 2.5, anchoredAtMs: Date.now() });
    await nextTick();
    expect(wrapper.text()).toContain("second line");
    expect(wrapper.text()).toContain("third line");

    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, trackId: "track-1", isPlaying: false, positionSeconds: 8, anchoredAtMs: Date.now() });
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

  it("keeps the newest same-track clock when a delayed clock arrives", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    let clockListener!: (clock: DesktopLyricsClockPayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock(listener) { clockListener = listener; return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    stateListener(desktopLyricsState({ revision: 1, positionSeconds: 0.5, anchoredAtMs: 100 }));
    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, revision: 3, trackId: "track-1", isPlaying: false, positionSeconds: 4.5, anchoredAtMs: 300 });
    await nextTick();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("third line");

    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, revision: 2, trackId: "track-1", isPlaying: false, positionSeconds: 2.5, anchoredAtMs: 200 });
    await nextTick();

    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("third line");
    wrapper.unmount();
  });

  it("does not let a delayed snapshot reanchor a newer same-track clock", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    let clockListener!: (clock: DesktopLyricsClockPayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock(listener) { clockListener = listener; return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    stateListener(desktopLyricsState({ revision: 1, positionSeconds: 0.5, anchoredAtMs: 100 }));
    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, revision: 3, trackId: "track-1", isPlaying: false, positionSeconds: 4.5, anchoredAtMs: 300 });
    await nextTick();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("third line");

    stateListener(desktopLyricsState({ revision: 2, positionSeconds: 2.5, anchoredAtMs: 200 }));
    await nextTick();

    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("third line");
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
      version: 4,
      transportEpoch: TEST_TRANSPORT_EPOCH,
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
    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, trackId: "track-1", isPlaying: false, positionSeconds: 2, anchoredAtMs: Date.now() });
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
    clockListener({ version: 4, transportEpoch: TEST_TRANSPORT_EPOCH, trackId: "track-1", isPlaying: false, positionSeconds: 2.5, anchoredAtMs: 200 });
    await nextTick();
    expect(wrapper.get(".desktop-lyric-line-current").text()).toContain("second line");

    clockListener({
      version: 2 as DesktopLyricsClockPayload["version"],
      transportEpoch: TEST_TRANSPORT_EPOCH,
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

  it("accepts a lower revision from a new epoch and rejects old epoch replays", async () => {
    let stateListener!: (state: DesktopLyricsStatePayload) => void;
    let clockListener!: (clock: DesktopLyricsClockPayload) => void;
    const bridge: DesktopLyricsBridge = {
      async onState(listener) { stateListener = listener; return () => undefined; },
      async onClock(listener) { clockListener = listener; return () => undefined; },
      async emitAction() {},
    };
    const wrapper = mount(DesktopLyricsApp, { props: { bridge } });
    await flushPromises();

    // 模拟主窗口刷新前：歌词窗口已收到 revision 较大的旧快照（track-1）
    stateListener({
      version: 4,
      transportEpoch: "main-before-restart",
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

    // 主窗口刷新后换用新 epoch，低 revision 也应作为新的传输流被接受。
    stateListener({
      version: 4,
      transportEpoch: "main-after-restart",
      revision: 1,
      track: { id: "track-1", title: "New Song", artist: "New Artist" },
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
    expect(wrapper.find(".lucide-pause").exists()).toBe(true);

    clockListener({
      version: 4,
      transportEpoch: "main-before-restart",
      revision: 1_000_000_006,
      trackId: "track-1",
      isPlaying: false,
      positionSeconds: 30,
      anchoredAtMs: Date.now(),
    });
    stateListener({
      version: 4,
      transportEpoch: "main-before-restart",
      revision: 1_000_000_007,
      track: { id: "track-1", title: "Old Song Replay", artist: "Old Artist" },
      lyrics: null,
      isPlaying: false,
      positionSeconds: 30,
      anchoredAtMs: Date.now(),
      offsetSeconds: 0,
      showTranslation: false,
      locked: false,
      fontScale: 1,
    });
    await nextTick();

    expect(wrapper.text()).toContain("New Song");
    expect(wrapper.text()).not.toContain("Old Song Replay");
    expect(wrapper.find(".lucide-pause").exists()).toBe(true);

    wrapper.unmount();
  });
});

function desktopLyricsState(overrides: Partial<DesktopLyricsStatePayload> = {}): DesktopLyricsStatePayload {
  return {
    version: 4,
    transportEpoch: TEST_TRANSPORT_EPOCH,
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

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
