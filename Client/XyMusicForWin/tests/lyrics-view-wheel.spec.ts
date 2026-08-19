import { mount, type VueWrapper } from "@vue/test-utils";
import { createPinia } from "pinia";
import { nextTick } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LYRICS_PREFERENCE_PERSIST_DEBOUNCE_MS } from "../src/presentation/stores/LyricsPreferencePersistence";
import type { ApplicationServices } from "../src/application/services";
import type { Lyrics, Track } from "../src/domain/music";
import { FakePlaybackSession } from "./support/FakePlaybackSession";
import LyricsView from "../src/presentation/components/LyricsView.vue";
import { applicationServicesKey } from "../src/presentation/services";
import { useLyricsStore } from "../src/presentation/stores/lyricsStore";
import { usePlayerStore } from "../src/presentation/stores/playerStore";

const originalScrollIntoView = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");
const scrollIntoView = vi.fn();

describe("playback lyrics wheel controls", () => {
  beforeEach(() => {
    scrollIntoView.mockReset();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      writable: true,
      value: scrollIntoView,
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
    if (originalScrollIntoView) Object.defineProperty(HTMLElement.prototype, "scrollIntoView", originalScrollIntoView);
    else Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
  });

  it("prevents Ctrl+wheel defaults while coalescing font-size persistence", async () => {
    vi.useFakeTimers();
    const mounted = await mountLyricsView({ timing: "LINE" });
    try {
      const scroll = mounted.wrapper.get(".lyrics-scroll").element;
      const increase = new WheelEvent("wheel", { cancelable: true, ctrlKey: true, deltaY: -100 });
      scroll.dispatchEvent(increase);
      await nextTick();

      expect(increase.defaultPrevented).toBe(true);
      expect(mounted.lyrics.fontScale).toBe(1.1);
      expect(mounted.writeLyricsFontScale).not.toHaveBeenCalled();

      const decrease = new WheelEvent("wheel", { cancelable: true, ctrlKey: true, deltaY: 100 });
      scroll.dispatchEvent(decrease);
      await nextTick();

      expect(decrease.defaultPrevented).toBe(true);
      expect(mounted.lyrics.fontScale).toBe(1);
      expect(mounted.writeLyricsFontScale).not.toHaveBeenCalled();

      vi.advanceTimersByTime(LYRICS_PREFERENCE_PERSIST_DEBOUNCE_MS);
      expect(mounted.writeLyricsFontScale).toHaveBeenCalledExactlyOnceWith(1);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("binds both theme palettes to playback lyric CSS variables", async () => {
    const mounted = await mountLyricsView({ timing: "LINE" });
    try {
      const style = mounted.wrapper.get(".lyrics-scroll").attributes("style");
      expect(style).toContain("--playback-lyric-text-dark: #8e98a3");
      expect(style).toContain("--playback-lyric-highlight-dark: #d7e6f3");
      expect(style).toContain("--playback-lyric-text-light: #626a74");
      expect(style).toContain("--playback-lyric-highlight-light: #1b4269");
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("keeps ordinary wheel scrolling and pauses automatic lyric following", async () => {
    const mounted = await mountLyricsView({ timing: "LINE" });
    try {
      scrollIntoView.mockClear();
      const scroll = mounted.wrapper.get(".lyrics-scroll").element;
      const wheel = new WheelEvent("wheel", { cancelable: true, deltaY: 100 });
      scroll.dispatchEvent(wheel);

      expect(wheel.defaultPrevented).toBe(false);
      expect(mounted.writeLyricsFontScale).not.toHaveBeenCalled();

      mounted.playbackSession.update({ currentTime: 1.5 });
      await nextTick();
      await nextTick();

      expect(scrollIntoView).not.toHaveBeenCalled();
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("smoothly chases adjacent and dense lyric targets without native scrolling", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "first line" },
        { time: 0.1, text: "second line" },
        { time: 0.2, text: "third line" },
        { time: 0.3, text: "fourth line" },
      ],
    });
    try {
      await nextTick();
      await nextTick();
      const scroll = installScrollMetrics(mounted.wrapper);
      scrollIntoView.mockClear();

      mounted.playbackSession.update({ currentTime: 0.1 });
      await nextTick();
      await nextTick();
      await nextTick();

      expect(scrollIntoView).not.toHaveBeenCalled();
      expect(animationFrames).toHaveLength(1);
      animationFrames.shift()?.(16);
      const firstFrameTop = scroll.scrollTop;
      expect(firstFrameTop).toBeGreaterThan(0);
      expect(firstFrameTop).toBeLessThan(70);

      mounted.playbackSession.update({ currentTime: 0.2 });
      await nextTick();
      await nextTick();
      // A browser may still deliver a cancelled callback selected before the retarget. The
      // generation guard ignores it; the next live frame continues from the visible position.
      for (let index = 0; index < 2 && scroll.scrollTop <= firstFrameTop; index += 1) {
        animationFrames.shift()?.(32);
      }
      expect(scroll.scrollTop).toBeGreaterThan(firstFrameTop);
      expect(scroll.scrollTop).toBeLessThan(150);

      for (let index = 0; index < 120 && animationFrames.length; index += 1) {
        animationFrames.shift()?.(48 + index * 16);
      }
      expect(scroll.scrollTop).toBeCloseTo(150, 1);

      mounted.playbackSession.update({ currentTime: 0.3 });
      await nextTick();
      await nextTick();
      expect(scrollIntoView).not.toHaveBeenCalled();
      expect(animationFrames).toHaveLength(1);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("snaps only for a non-dense line jump", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "first line" },
        { time: 1, text: "second line" },
        { time: 5, text: "third line" },
        { time: 6, text: "fourth line" },
      ],
    });
    try {
      await nextTick();
      await nextTick();
      installScrollMetrics(mounted.wrapper);
      scrollIntoView.mockClear();

      mounted.playbackSession.update({ currentTime: 5 });
      await nextTick();
      await nextTick();
      await nextTick();

      expect(scrollIntoView).not.toHaveBeenCalled();
      expect(animationFrames).toHaveLength(0);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("restores auto-follow without using browser smooth scrolling when lyrics reload", async () => {
    const mounted = await mountLyricsView({ timing: "LINE" });
    try {
      const reloadedLyrics = mounted.lyrics.lyrics;
      expect(reloadedLyrics).not.toBeNull();
      scrollIntoView.mockClear();

      mounted.lyrics.lyrics = null;
      await nextTick();
      mounted.lyrics.lyrics = reloadedLyrics;
      await nextTick();
      await nextTick();

      expect(scrollIntoView).not.toHaveBeenCalled();
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("restores auto-follow when reloaded lyrics begin after the current position", async () => {
    const mounted = await mountLyricsView({ timing: "LINE" });
    try {
      mounted.lyrics.lyrics = null;
      await nextTick();
      mounted.lyrics.lyrics = {
        trackId: "track-1",
        source: "lrc",
        synchronized: true,
        timing: "LINE",
        lines: [
          { time: 5, text: "first delayed line" },
          { time: 6, text: "second delayed line" },
        ],
      };
      await nextTick();
      await nextTick();
      scrollIntoView.mockClear();

      mounted.playbackSession.update({ currentTime: 5 });
      await nextTick();
      await nextTick();

      expect(scrollIntoView).not.toHaveBeenCalled();
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("attaches global manual-scroll listeners only during an active lyric gesture", async () => {
    const addEventListener = vi.spyOn(window, "addEventListener");
    const removeEventListener = vi.spyOn(window, "removeEventListener");
    const mounted = await mountLyricsView({ timing: "LINE" });
    try {
      expect(listenerCalls(addEventListener, "pointermove")).toHaveLength(0);
      expect(listenerCalls(addEventListener, "pointerup")).toHaveLength(0);
      expect(listenerCalls(addEventListener, "pointercancel")).toHaveLength(0);

      const scroll = mounted.wrapper.get(".lyrics-scroll").element;
      scroll.dispatchEvent(pointerEvent("pointerdown", { pointerId: 7, clientY: 40 }));

      expect(listenerCalls(addEventListener, "pointermove")).toHaveLength(1);
      expect(listenerCalls(addEventListener, "pointerup")).toHaveLength(1);
      expect(listenerCalls(addEventListener, "pointercancel")).toHaveLength(1);

      window.dispatchEvent(pointerEvent("pointerup", { pointerId: 7, clientY: 40 }));

      expect(listenerCalls(removeEventListener, "pointermove")).toHaveLength(1);
      expect(listenerCalls(removeEventListener, "pointerup")).toHaveLength(1);
      expect(listenerCalls(removeEventListener, "pointercancel")).toHaveLength(1);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("coalesces manual lyric scrolling and cancels pending frames on gesture end and unmount", async () => {
    const mounted = await mountLyricsView({ timing: "LINE" });
    const animationFrames: FrameRequestCallback[] = [];
    const cancelAnimationFrame = vi.fn();
    let nextFrameHandle = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      nextFrameHandle += 1;
      return nextFrameHandle;
    });
    vi.stubGlobal("cancelAnimationFrame", cancelAnimationFrame);

    try {
      const scroll = mounted.wrapper.get(".lyrics-scroll").element;
      scroll.scrollTop = 100;
      scroll.dispatchEvent(pointerEvent("pointerdown", { pointerId: 8, clientY: 10 }));

      const firstMove = pointerEvent("pointermove", { pointerId: 8, clientY: 20 });
      window.dispatchEvent(firstMove);
      const latestMove = pointerEvent("pointermove", { pointerId: 8, clientY: 50 });
      window.dispatchEvent(latestMove);

      expect(firstMove.defaultPrevented).toBe(true);
      expect(latestMove.defaultPrevented).toBe(true);
      expect(animationFrames).toHaveLength(1);
      expect(scroll.scrollTop).toBe(100);

      animationFrames[0]?.(16);
      expect(scroll.scrollTop).toBe(60);

      window.dispatchEvent(pointerEvent("pointermove", { pointerId: 8, clientY: 70 }));
      expect(animationFrames).toHaveLength(2);

      window.dispatchEvent(pointerEvent("pointerup", { pointerId: 8, clientY: 70 }));
      expect(cancelAnimationFrame).toHaveBeenCalledWith(2);
      animationFrames[1]?.(32);
      expect(scroll.scrollTop).toBe(60);

      scroll.dispatchEvent(pointerEvent("pointerdown", { pointerId: 9, clientY: 10 }));
      window.dispatchEvent(pointerEvent("pointermove", { pointerId: 9, clientY: 30 }));
      expect(animationFrames).toHaveLength(3);

      mounted.wrapper.unmount();
      expect(cancelAnimationFrame).toHaveBeenCalledWith(3);
      animationFrames[2]?.(48);
      expect(scroll.scrollTop).toBe(60);
    } finally {
      if (mounted.wrapper.exists()) mounted.wrapper.unmount();
    }
  });

  it("renders explicit word timing without the removed line-progress layer", async () => {
    const mounted = await mountLyricsView({
      timing: "WORD",
      lines: [{
        time: 0,
        text: "first second",
        words: [{ time: 0, endTime: 1, text: "first" }, { time: 1, endTime: 2, text: " second" }],
      }],
    });
    try {
      expect(mounted.wrapper.findAll(".lyric-line-fill")).toHaveLength(0);
      expect(mounted.wrapper.findAll(".lyric-word")).toHaveLength(2);
      expect(mounted.wrapper.findAll(".lyric-word.is-sung")).toHaveLength(0);

      mounted.playbackSession.update({ currentTime: 1.2 });
      await nextTick();
      expect(mounted.wrapper.findAll(".lyric-word.is-sung")).toHaveLength(2);
      expect(mounted.wrapper.findAll(".lyric-word")[1]?.attributes("style")).toContain("--lyric-word-progress: 20%;");
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("keeps outgoing word progress on the shared line-transition clock", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
    const mounted = await mountLyricsView({
      timing: "WORD",
      currentTime: 1.2,
      lines: [
        {
          time: 0,
          text: "first second",
          words: [{ time: 0, endTime: 1, text: "first" }, { time: 1, endTime: 2, text: " second" }],
        },
        {
          time: 3,
          text: "third fourth",
          words: [{ time: 3, endTime: 4, text: "third" }, { time: 4, endTime: 5, text: " fourth" }],
        },
      ],
    });
    try {
      expect(mounted.player.isPlaying).toBe(false);
      const lines = mounted.wrapper.findAll(".lyric-line");
      expect(lines[0]?.findAll(".lyric-word.is-sung")).toHaveLength(2);
      expect(lines[1]?.findAll(".lyric-word.is-sung")).toHaveLength(0);
      expect(lines[1]?.findAll(".lyric-word").every((word) => word.attributes("style").includes("--lyric-word-progress: 0%;"))).toBe(true);

      mounted.playbackSession.update({ currentTime: 3.2 });
      await nextTick();
      await nextTick();

      const updatedLines = mounted.wrapper.findAll(".lyric-line");
      expect(updatedLines[0]?.findAll(".lyric-word.is-sung")).toHaveLength(2);
      expect(updatedLines[1]?.findAll(".lyric-word.is-sung")).toHaveLength(0);

      animationFrames.shift()?.(16);
      await nextTick();
      expect(updatedLines[1]?.findAll(".lyric-word.is-sung")).toHaveLength(1);

      for (let frame = 0; frame < 80 && animationFrames.length; frame += 1) {
        animationFrames.shift()?.(32 + frame * 16);
        await nextTick();
      }
      await nextTick();
      expect(updatedLines[0]?.findAll(".lyric-word.is-sung")).toHaveLength(0);
      expect(updatedLines[0]?.findAll(".lyric-word").every((word) => word.attributes("style").includes("--lyric-word-progress: 0%;"))).toBe(true);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("keeps the committed highlight while a lyric seek waits for its target acknowledgement", async () => {
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "first line" },
        { time: 1, text: "intermediate line" },
        { time: 2, text: "third line" },
        { time: 3, text: "target line" },
      ],
    });
    const seekTo = vi.spyOn(mounted.playbackSession, "seekTo").mockImplementation(() => undefined);
    try {
      expect(mounted.wrapper.findAll(".lyric-line")[0]?.classes()).toContain("active");
      await mounted.wrapper.findAll(".lyric-line")[3]!.trigger("click");

      mounted.playbackSession.update({ currentTime: 1.1 });
      await nextTick();
      await nextTick();

      const waitingLines = mounted.wrapper.findAll(".lyric-line");
      expect(waitingLines[0]?.classes()).toContain("active");
      expect(waitingLines[1]?.classes()).not.toContain("active");

      mounted.playbackSession.update({ currentTime: 3.1 });
      await nextTick();
      await nextTick();

      expect(mounted.wrapper.findAll(".lyric-line")[3]?.classes()).toContain("active");
    } finally {
      seekTo.mockRestore();
      mounted.wrapper.unmount();
    }
  });

  it("settles an interrupted transition when a lyric seek acknowledgement times out", async () => {
    vi.useFakeTimers();
    const animationFrames = new Map<number, FrameRequestCallback>();
    let nextFrameHandle = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      nextFrameHandle += 1;
      animationFrames.set(nextFrameHandle, callback);
      return nextFrameHandle;
    });
    vi.stubGlobal("cancelAnimationFrame", (handle: number) => {
      animationFrames.delete(handle);
    });
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "timeout source" },
        { time: 0.1, text: "timeout current" },
        { time: 0.2, text: "timeout target" },
      ],
    });
    const seekTo = vi.spyOn(mounted.playbackSession, "seekTo").mockImplementation(() => undefined);
    const runNextFrame = (timestamp: number): boolean => {
      const next = animationFrames.entries().next().value as [number, FrameRequestCallback] | undefined;
      if (!next) return false;
      animationFrames.delete(next[0]);
      next[1](timestamp);
      return true;
    };
    try {
      const scroll = installScrollMetrics(mounted.wrapper);
      mounted.playbackSession.update({ currentTime: 0.1 });
      await nextTick();
      await nextTick();
      expect(runNextFrame(16)).toBe(true);
      const interruptedTop = scroll.scrollTop;
      expect(interruptedTop).toBeGreaterThan(0);
      expect(interruptedTop).toBeLessThan(70);

      await mounted.wrapper.findAll(".lyric-line")[2]!.trigger("click");
      expect(animationFrames.size).toBe(0);
      vi.advanceTimersByTime(1_500);
      await nextTick();
      await nextTick();
      expect(animationFrames.size).toBe(1);

      for (let frame = 0; frame < 100 && animationFrames.size; frame += 1) {
        runNextFrame(32 + frame * 16);
      }
      const lines = mounted.wrapper.findAll(".lyric-line");
      expect(scroll.scrollTop).toBeCloseTo(70, 1);
      expect(Number((lines[0]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"))).toBe(0);
      expect(Number((lines[1]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"))).toBe(1);
    } finally {
      seekTo.mockRestore();
      mounted.wrapper.unmount();
    }
  });

  it("animates a lyric seek while the restored track is still paused", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "paused source" },
        { time: 1, text: "paused second" },
        { time: 2, text: "paused third" },
        { time: 3, text: "paused target" },
      ],
    });
    try {
      const scroll = installScrollMetrics(mounted.wrapper);
      expect(mounted.player.isPlaying).toBe(false);

      await mounted.wrapper.findAll(".lyric-line")[3]!.trigger("click");
      await nextTick();
      await nextTick();

      expect(mounted.playbackSession.state().currentTime).toBe(3);
      expect(mounted.playbackSession.state().isPlaying).toBe(false);
      expect(mounted.wrapper.findAll(".lyric-line")[3]?.classes()).toContain("active");
      expect(scroll.scrollTop).toBe(0);

      animationFrames.shift()?.(16);
      animationFrames.shift()?.(32);
      animationFrames.shift()?.(48);
      expect(scroll.scrollTop).toBe(0);

      animationFrames.shift()?.(64);
      const animatingLines = mounted.wrapper.findAll(".lyric-line");
      const sourceEmphasis = Number((animatingLines[0]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"));
      const targetEmphasis = Number((animatingLines[3]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"));
      expect(scroll.scrollTop).toBeGreaterThan(0);
      expect(scroll.scrollTop).toBeLessThan(230);
      expect(sourceEmphasis).toBeGreaterThan(0);
      expect(sourceEmphasis).toBeLessThan(1);
      expect(targetEmphasis).toBeGreaterThan(0);
      expect(targetEmphasis).toBeLessThan(1);

      for (let frame = 0; frame < 80 && animationFrames.length; frame += 1) {
        animationFrames.shift()?.(80 + frame * 16);
      }
      expect(scroll.scrollTop).toBeCloseTo(230, 1);
      expect(Number((animatingLines[3]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"))).toBe(1);
      expect(mounted.playbackSession.state().isPlaying).toBe(false);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("retargets an in-flight paused lyric seek from its visible frame", async () => {
    const animationFrames = new Map<number, FrameRequestCallback>();
    let nextFrameHandle = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      nextFrameHandle += 1;
      animationFrames.set(nextFrameHandle, callback);
      return nextFrameHandle;
    });
    vi.stubGlobal("cancelAnimationFrame", (handle: number) => {
      animationFrames.delete(handle);
    });
    const runNextFrame = (timestamp: number): boolean => {
      const next = animationFrames.entries().next().value as [number, FrameRequestCallback] | undefined;
      if (!next) return false;
      animationFrames.delete(next[0]);
      next[1](timestamp);
      return true;
    };
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "retarget source" },
        { time: 1, text: "retarget first" },
        { time: 2, text: "retarget second" },
        { time: 3, text: "retarget final" },
      ],
    });
    try {
      const scroll = installScrollMetrics(mounted.wrapper);
      await mounted.wrapper.findAll(".lyric-line")[2]!.trigger("click");
      await nextTick();
      await nextTick();

      expect(runNextFrame(16)).toBe(true);
      expect(runNextFrame(32)).toBe(true);
      expect(runNextFrame(48)).toBe(true);
      expect(runNextFrame(64)).toBe(true);
      const interruptedTop = scroll.scrollTop;
      expect(interruptedTop).toBeGreaterThan(0);
      expect(interruptedTop).toBeLessThan(150);

      await mounted.wrapper.findAll(".lyric-line")[3]!.trigger("click");
      await nextTick();
      await nextTick();
      expect(mounted.wrapper.findAll(".lyric-line")[3]?.classes()).toContain("active");

      expect(runNextFrame(80)).toBe(true);
      expect(runNextFrame(96)).toBe(true);
      expect(runNextFrame(112)).toBe(true);
      expect(runNextFrame(128)).toBe(true);
      expect(scroll.scrollTop).toBeGreaterThan(interruptedTop);
      expect(scroll.scrollTop).toBeLessThan(230);

      for (let frame = 0; frame < 120 && animationFrames.size; frame += 1) {
        runNextFrame(144 + frame * 16);
      }
      const lines = mounted.wrapper.findAll(".lyric-line");
      expect(scroll.scrollTop).toBeCloseTo(230, 1);
      expect(Number((lines[2]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"))).toBe(0);
      expect(Number((lines[3]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"))).toBe(1);
      expect(animationFrames.size).toBe(0);
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("cancels a paused lyric seek cleanly when manual dragging starts", async () => {
    const animationFrames = new Map<number, FrameRequestCallback>();
    let nextFrameHandle = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      nextFrameHandle += 1;
      animationFrames.set(nextFrameHandle, callback);
      return nextFrameHandle;
    });
    vi.stubGlobal("cancelAnimationFrame", (handle: number) => {
      animationFrames.delete(handle);
    });
    const runNextFrame = (timestamp: number): boolean => {
      const next = animationFrames.entries().next().value as [number, FrameRequestCallback] | undefined;
      if (!next) return false;
      animationFrames.delete(next[0]);
      next[1](timestamp);
      return true;
    };
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "drag source" },
        { time: 1, text: "drag second" },
        { time: 2, text: "drag third" },
        { time: 3, text: "drag target" },
      ],
    });
    try {
      const scroll = installScrollMetrics(mounted.wrapper);
      await mounted.wrapper.findAll(".lyric-line")[3]!.trigger("click");
      await nextTick();
      await nextTick();
      runNextFrame(16);
      runNextFrame(32);
      runNextFrame(48);
      runNextFrame(64);
      const interruptedTop = scroll.scrollTop;
      const staleTransition = animationFrames.values().next().value as FrameRequestCallback | undefined;
      expect(interruptedTop).toBeGreaterThan(0);
      expect(staleTransition).toBeDefined();

      scroll.dispatchEvent(pointerEvent("pointerdown", { pointerId: 18, clientY: 100 }));
      window.dispatchEvent(pointerEvent("pointermove", { pointerId: 18, clientY: 80 }));

      const lines = mounted.wrapper.findAll(".lyric-line");
      expect(lines[3]?.classes()).toContain("active");
      expect(Number((lines[0]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"))).toBe(0);
      expect(Number((lines[3]!.element as HTMLElement).style.getPropertyValue("--lyric-line-emphasis"))).toBe(1);
      expect(animationFrames.size).toBe(1);

      staleTransition?.(80);
      expect(scroll.scrollTop).toBe(interruptedTop);
      expect(runNextFrame(96)).toBe(true);
      expect(scroll.scrollTop).toBeCloseTo(interruptedTop + 20, 5);
      expect(animationFrames.size).toBe(0);
      window.dispatchEvent(pointerEvent("pointerup", { pointerId: 18, clientY: 80 }));
    } finally {
      mounted.wrapper.unmount();
    }
  });

  it("holds later lyric commits through the seek animation after two stable frames", async () => {
    const animationFrames: FrameRequestCallback[] = [];
    let nextFrameHandle = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      nextFrameHandle += 1;
      return nextFrameHandle;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "source line" },
        { time: 1, text: "seek target" },
        { time: 1.1, text: "next dense line" },
      ],
    });
    const seekTo = vi.spyOn(mounted.playbackSession, "seekTo").mockImplementation(() => undefined);
    try {
      const scroll = installScrollMetrics(mounted.wrapper);
      await mounted.wrapper.findAll(".lyric-line")[1]!.trigger("click");
      mounted.playbackSession.update({ currentTime: 1.01 });
      await nextTick();
      await nextTick();

      expect(mounted.wrapper.findAll(".lyric-line")[1]?.classes()).toContain("active");
      expect(animationFrames).toHaveLength(1);

      mounted.playbackSession.update({ currentTime: 1.11 });
      await nextTick();
      await nextTick();
      expect(mounted.wrapper.findAll(".lyric-line")[1]?.classes()).toContain("active");

      animationFrames.shift()?.(16);
      animationFrames.shift()?.(32);
      expect(mounted.wrapper.findAll(".lyric-line")[1]?.classes()).toContain("active");

      animationFrames.shift()?.(48);
      expect(mounted.wrapper.findAll(".lyric-line")[1]?.classes()).toContain("active");
      expect(mounted.wrapper.findAll(".lyric-line")[2]?.classes()).not.toContain("active");

      animationFrames.shift()?.(64);
      expect(scroll.scrollTop).toBeGreaterThan(0);
      expect(scroll.scrollTop).toBeLessThan(70);
      for (let frame = 0; frame < 80; frame += 1) {
        const callback = animationFrames.shift();
        if (!callback) break;
        callback(80 + frame * 16);
        await nextTick();
        if (mounted.wrapper.findAll(".lyric-line")[2]?.classes().includes("active")) break;
      }
      await nextTick();
      await nextTick();
      expect(mounted.wrapper.findAll(".lyric-line")[2]?.classes()).toContain("active");
      expect(animationFrames).toHaveLength(1);
    } finally {
      seekTo.mockRestore();
      mounted.wrapper.unmount();
    }
  });

  it("starts the seek animation after an unstable baseline reaches frame eight", async () => {
    const animationFrames: Array<{ handle: number; callback: FrameRequestCallback }> = [];
    const cancelAnimationFrame = vi.fn();
    let nextFrameHandle = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      nextFrameHandle += 1;
      animationFrames.push({ handle: nextFrameHandle, callback });
      return nextFrameHandle;
    });
    vi.stubGlobal("cancelAnimationFrame", cancelAnimationFrame);
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "source line" },
        { time: 1, text: "unstable target" },
        { time: 1.1, text: "latest line" },
      ],
    });
    const seekTo = vi.spyOn(mounted.playbackSession, "seekTo").mockImplementation(() => undefined);
    try {
      const scroll = installScrollMetrics(mounted.wrapper);
      const target = mounted.wrapper.findAll(".lyric-line")[1]!.element as HTMLElement;
      let layoutShift = 0;
      target.getBoundingClientRect = () => rect(80 + layoutShift - scroll.scrollTop, 80);

      await mounted.wrapper.findAll(".lyric-line")[1]!.trigger("click");
      mounted.playbackSession.update({ currentTime: 1.01 });
      await nextTick();
      await nextTick();
      mounted.playbackSession.update({ currentTime: 1.11 });
      await nextTick();

      for (let frame = 1; frame < 8; frame += 1) {
        layoutShift = frame * 2;
        animationFrames.shift()?.callback(frame * 16);
        expect(mounted.wrapper.findAll(".lyric-line")[1]?.classes()).toContain("active");
      }

      layoutShift = 16;
      animationFrames.shift()?.callback(128);
      expect(mounted.wrapper.findAll(".lyric-line")[1]?.classes()).toContain("active");
      expect(mounted.wrapper.findAll(".lyric-line")[2]?.classes()).not.toContain("active");

      const beforeAnimation = scroll.scrollTop;
      animationFrames.shift()?.callback(144);
      expect(scroll.scrollTop).toBeGreaterThan(beforeAnimation);
      for (let frame = 0; frame < 80; frame += 1) {
        const pendingFrame = animationFrames.shift();
        if (!pendingFrame) break;
        pendingFrame.callback(160 + frame * 16);
        await nextTick();
        if (mounted.wrapper.findAll(".lyric-line")[2]?.classes().includes("active")) break;
      }
      await nextTick();
      await nextTick();
      expect(mounted.wrapper.findAll(".lyric-line")[2]?.classes()).toContain("active");

    } finally {
      seekTo.mockRestore();
      if (mounted.wrapper.exists()) mounted.wrapper.unmount();
    }
  });

  it("cancels a pending seek-layout frame on unmount and ignores its stale callback", async () => {
    const animationFrames: Array<{ handle: number; callback: FrameRequestCallback }> = [];
    const cancelAnimationFrame = vi.fn();
    let nextFrameHandle = 0;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      nextFrameHandle += 1;
      animationFrames.push({ handle: nextFrameHandle, callback });
      return nextFrameHandle;
    });
    vi.stubGlobal("cancelAnimationFrame", cancelAnimationFrame);
    const mounted = await mountLyricsView({
      timing: "LINE",
      lines: [
        { time: 0, text: "source line" },
        { time: 1, text: "seek target" },
        { time: 1.1, text: "later line" },
      ],
    });
    const seekTo = vi.spyOn(mounted.playbackSession, "seekTo").mockImplementation(() => undefined);
    try {
      installScrollMetrics(mounted.wrapper);
      await mounted.wrapper.findAll(".lyric-line")[1]!.trigger("click");
      mounted.playbackSession.update({ currentTime: 1.01 });
      await nextTick();
      await nextTick();
      mounted.playbackSession.update({ currentTime: 1.11 });
      await nextTick();

      const pendingFrame = animationFrames.shift();
      expect(pendingFrame).toBeDefined();
      mounted.wrapper.unmount();
      expect(cancelAnimationFrame).toHaveBeenCalledWith(pendingFrame!.handle);

      pendingFrame!.callback(16);
      await nextTick();
      expect(animationFrames).toHaveLength(0);
    } finally {
      seekTo.mockRestore();
      if (mounted.wrapper.exists()) mounted.wrapper.unmount();
    }
  });

});

async function mountLyricsView(options: {
  timing: Lyrics["timing"];
  lines?: Lyrics["lines"];
  currentTime?: number;
}): Promise<{
  wrapper: VueWrapper;
  player: ReturnType<typeof usePlayerStore>;
  playbackSession: FakePlaybackSession;
  lyrics: ReturnType<typeof useLyricsStore>;
  writeLyricsFontScale: ReturnType<typeof vi.fn>;
}> {
  const pinia = createPinia();
  const writeLyricsFontScale = vi.fn();
  const playbackSession = new FakePlaybackSession({
    state: {
      queue: [track()],
      currentIndex: 0,
      currentTime: options.currentTime ?? 0,
      duration: 180,
    },
  });
  const services = createServices(writeLyricsFontScale, playbackSession);
  const wrapper = mount(LyricsView, {
    global: {
      plugins: [pinia],
      provide: { [applicationServicesKey as symbol]: services },
      stubs: {
        ArtworkImage: { template: "<div />" },
        LyricsPlayerControls: { template: "<div />" },
      },
    },
  });
  const player = usePlayerStore(pinia);
  const lyrics = useLyricsStore(pinia);
  player.lyricsOpen = true;
  lyrics.lyrics = {
    trackId: "track-1",
    source: "lrc",
    synchronized: true,
    timing: options.timing,
    lines: options.lines ?? [
      { time: 0, text: "first line" },
      { time: 1, text: "second line" },
    ],
  };
  await nextTick();
  await nextTick();
  return { wrapper, player, playbackSession, lyrics, writeLyricsFontScale };
}

function createServices(
  writeLyricsFontScale: (value: number) => void,
  playbackSession: FakePlaybackSession,
): ApplicationServices {
  return {
    catalog: { lyrics: vi.fn(async () => null) },
    playbackSession,
    desktopWindowController: {
      state: () => ({ maximized: false, fullscreen: false }),
      subscribe: () => () => undefined,
      toggleMaximize: vi.fn(async () => undefined),
    },
    uiPreferences: {
      readLyrics: () => ({
        fontScale: 1,
        showTranslation: true,
        colors: {
          dark: { textColor: "#8e98a3", highlightColor: "#d7e6f3" },
          light: { textColor: "#626a74", highlightColor: "#1b4269" },
        },
      }),
      writeLyricsFontScale,
      writeLyricsTranslation() {},
      writeLyricsTextColor() {},
      writeLyricsHighlightColor() {},
      readLyricsOffset: () => 0,
      writeLyricsOffset() {},
      clearLyricsOffsets() {},
    },
  } as unknown as ApplicationServices;
}

function track(): Track {
  return {
    id: "track-1",
    title: "Track",
    artist: "Artist",
    artistIds: ["artist-1"],
    album: "Album",
    coverUrl: "",
    duration: 180,
    liked: false,
    publishedAt: "2026-07-18T00:00:00.000Z",
  };
}

function pointerEvent(type: string, options: { pointerId: number; clientY: number; button?: number }): PointerEvent {
  const event = new MouseEvent(type, {
    bubbles: true,
    cancelable: true,
    button: options.button ?? 0,
    clientY: options.clientY,
  });
  Object.defineProperty(event, "pointerId", { value: options.pointerId });
  return event as PointerEvent;
}


function listenerCalls(spy: ReturnType<typeof vi.spyOn>, type: string): unknown[][] {
  return spy.mock.calls.filter(([eventType]) => eventType === type);
}

function installScrollMetrics(wrapper: VueWrapper): HTMLElement {
  const scroll = wrapper.get(".lyrics-scroll").element as HTMLElement;
  Object.defineProperty(scroll, "clientHeight", { configurable: true, value: 100 });
  Object.defineProperty(scroll, "scrollHeight", { configurable: true, value: 1_000 });
  scroll.getBoundingClientRect = () => rect(0, 100);
  wrapper.findAll(".lyric-line").forEach((line, index) => {
    line.element.getBoundingClientRect = () => rect(index * 80 - scroll.scrollTop, 80);
  });
  return scroll;
}

function rect(top: number, height: number): DOMRect {
  return {
    x: 0,
    y: top,
    width: 240,
    height,
    top,
    right: 240,
    bottom: top + height,
    left: 0,
    toJSON: () => ({}),
  } as DOMRect;
}
