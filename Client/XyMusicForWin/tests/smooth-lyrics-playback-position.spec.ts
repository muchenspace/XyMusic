import { nextTick, ref } from "vue";
import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSmoothLyricsPlaybackPosition } from "../src/presentation/composables/useSmoothLyricsPlaybackPosition";

describe("smooth lyrics playback position", () => {
  const animationFrames: FrameRequestCallback[] = [];
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    animationFrames.splice(0);
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    vi.stubGlobal("cancelAnimationFrame", () => undefined);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("does not jump when a normal coarse audio sample is ahead of the local clock", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    let displayedPosition!: { value: number };
    const wrapper = mount({
      setup() {
        displayedPosition = useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
        });
        return () => null;
      },
    });

    await nextTick();
    vi.setSystemTime(33);
    animationFrames.shift()?.(33);
    expect(displayedPosition.value).toBeCloseTo(0.033, 3);

    vi.setSystemTime(34);
    nativePosition.value = 0.1;
    await nextTick();
    vi.setSystemTime(50);
    animationFrames.shift()?.(50);

    expect(displayedPosition.value).toBeLessThan(0.08);
    wrapper.unmount();
  });

  it("corrects ordinary clock error over 120ms with the Android easing", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    let displayedPosition!: { value: number };
    const wrapper = mount({
      setup() {
        displayedPosition = useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          renderPlan: () => ({ requiresAnimationFrame: true, nextChangeAtSeconds: null }),
        });
        return () => null;
      },
    });

    await nextTick();
    animationFrames.shift()?.(0);
    vi.setSystemTime(40);
    animationFrames.shift()?.(40);
    expect(displayedPosition.value).toBeCloseTo(0.04, 3);

    nativePosition.value = 0.2;
    await nextTick();
    expect(displayedPosition.value).toBeCloseTo(0.04, 3);

    vi.setSystemTime(100);
    animationFrames.shift()?.(100);
    expect(displayedPosition.value).toBeGreaterThan(0.04);
    expect(displayedPosition.value).toBeLessThan(0.26);

    vi.setSystemTime(160);
    animationFrames.shift()?.(160);
    expect(displayedPosition.value).toBeCloseTo(0.32, 3);
    wrapper.unmount();
  });

  it("snaps errors above 250ms, playback-state changes, and paused position updates", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    let displayedPosition!: { value: number };
    const wrapper = mount({
      setup() {
        displayedPosition = useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
        });
        return () => null;
      },
    });

    await nextTick();
    vi.setSystemTime(50);
    animationFrames.shift()?.(50);
    nativePosition.value = 0.302;
    await nextTick();
    expect(displayedPosition.value).toBeCloseTo(0.302, 6);

    nativePosition.value = 0.29;
    playing.value = false;
    await nextTick();
    expect(displayedPosition.value).toBeCloseTo(0.29, 6);

    nativePosition.value = 0.4;
    await nextTick();
    expect(displayedPosition.value).toBeCloseTo(0.4, 6);
    wrapper.unmount();
  });

  it("never moves backwards while correcting a routine delayed sample", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    let displayedPosition!: { value: number };
    const wrapper = mount({
      setup() {
        displayedPosition = useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          renderPlan: () => ({ requiresAnimationFrame: true, nextChangeAtSeconds: null }),
        });
        return () => null;
      },
    });

    await nextTick();
    vi.setSystemTime(100);
    animationFrames.shift()?.(100);
    expect(displayedPosition.value).toBeCloseTo(0.1, 3);

    nativePosition.value = 0.01;
    await nextTick();
    vi.setSystemTime(160);
    animationFrames.shift()?.(160);
    expect(displayedPosition.value).toBeCloseTo(0.1, 6);

    vi.setSystemTime(220);
    animationFrames.shift()?.(220);
    expect(displayedPosition.value).toBeCloseTo(0.13, 3);
    wrapper.unmount();
  });

  it("snaps a small backwards change when the playback discontinuity changes", async () => {
    const nativePosition = ref(1);
    const discontinuity = ref(0);
    let displayedPosition!: { value: number };
    const wrapper = mount({
      setup() {
        displayedPosition = useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => true,
          discontinuityToken: () => discontinuity.value,
        });
        return () => null;
      },
    });

    await nextTick();
    vi.setSystemTime(100);
    animationFrames.shift()?.(100);
    expect(displayedPosition.value).toBeCloseTo(1.1, 3);

    nativePosition.value = 1.05;
    discontinuity.value += 1;
    await nextTick();
    expect(displayedPosition.value).toBeCloseTo(1.05, 6);
    wrapper.unmount();
  });

  it("returns line-timed lyrics to sleep after a clock correction settles", async () => {
    const wakeTimers: Array<{ callback: () => void; delay: number }> = [];
    const clearTimeout = vi.fn();
    vi.stubGlobal("setTimeout", (callback: () => void, delay?: number) => {
      wakeTimers.push({ callback, delay: delay ?? 0 });
      return wakeTimers.length;
    });
    vi.stubGlobal("clearTimeout", clearTimeout);

    const nativePosition = ref(0);
    const playing = ref(true);
    const wrapper = mount({
      setup() {
        useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          renderPlan: () => ({ requiresAnimationFrame: false, nextChangeAtSeconds: 10 }),
        });
        return () => null;
      },
    });

    await nextTick();
    animationFrames.shift()?.(0);
    expect(wakeTimers).toHaveLength(1);

    nativePosition.value = 0.1;
    await nextTick();
    expect(clearTimeout).toHaveBeenCalledWith(1);
    expect(animationFrames).toHaveLength(1);

    vi.setSystemTime(60);
    animationFrames.shift()?.(60);
    expect(animationFrames).toHaveLength(1);
    vi.setSystemTime(120);
    animationFrames.shift()?.(120);
    expect(animationFrames).toHaveLength(0);
    expect(wakeTimers).toHaveLength(2);
    wrapper.unmount();
  });

  it("does not keep an animation loop alive while the lyrics view is hidden", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    const active = ref(false);
    const wrapper = mount({
      setup() {
        useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          isActive: () => active.value,
        });
        return () => null;
      },
    });

    await nextTick();
    expect(animationFrames).toHaveLength(0);

    active.value = true;
    await nextTick();
    expect(animationFrames).toHaveLength(1);

    active.value = false;
    await nextTick();
    animationFrames.shift()?.(16);
    expect(animationFrames).toHaveLength(0);
    wrapper.unmount();
  });

  it("does not subscribe to frequent position updates while lyrics are hidden", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    const active = ref(false);
    const currentTime = vi.fn(() => nativePosition.value);
    const wrapper = mount({
      setup() {
        useSmoothLyricsPlaybackPosition({
          currentTime,
          isPlaying: () => playing.value,
          isActive: () => active.value,
        });
        return () => null;
      },
    });

    await nextTick();
    currentTime.mockClear();

    nativePosition.value = 0.1;
    await nextTick();
    expect(currentTime).not.toHaveBeenCalled();

    active.value = true;
    await nextTick();
    expect(currentTime).toHaveBeenCalled();
    wrapper.unmount();
  });

  it("ignores a cancelled frame callback after restarting lyric scheduling", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    const active = ref(true);
    const wrapper = mount({
      setup() {
        useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          isActive: () => active.value,
        });
        return () => null;
      },
    });

    await nextTick();
    expect(animationFrames).toHaveLength(1);

    active.value = false;
    await nextTick();
    active.value = true;
    await nextTick();
    expect(animationFrames).toHaveLength(2);

    // Browser cancellation can race a callback already selected for delivery.
    animationFrames[0]?.(16);
    expect(animationFrames).toHaveLength(2);
    wrapper.unmount();
  });

  it("ignores a cancelled line-wake callback after restarting lyric scheduling", async () => {
    const wakeTimers: Array<() => void> = [];
    vi.stubGlobal("setTimeout", (callback: () => void) => {
      wakeTimers.push(callback);
      return wakeTimers.length;
    });
    vi.stubGlobal("clearTimeout", () => undefined);

    const nativePosition = ref(0);
    const playing = ref(true);
    const active = ref(true);
    const wrapper = mount({
      setup() {
        useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          isActive: () => active.value,
          renderPlan: () => ({ requiresAnimationFrame: false, nextChangeAtSeconds: 10 }),
        });
        return () => null;
      },
    });

    await nextTick();
    animationFrames[0]?.(0);
    expect(wakeTimers).toHaveLength(1);

    active.value = false;
    await nextTick();
    active.value = true;
    await nextTick();
    animationFrames[1]?.(0);
    expect(wakeTimers).toHaveLength(2);

    wakeTimers[0]?.();
    expect(animationFrames).toHaveLength(2);
    wrapper.unmount();
  });

  it("replans a pending line wake when render-plan dependencies change", async () => {
    const wakeTimers: Array<{ callback: () => void; delay: number }> = [];
    const clearTimeout = vi.fn();
    vi.stubGlobal("setTimeout", (callback: () => void, delay?: number) => {
      wakeTimers.push({ callback, delay: delay ?? 0 });
      return wakeTimers.length;
    });
    vi.stubGlobal("clearTimeout", clearTimeout);

    const nativePosition = ref(0);
    const playing = ref(true);
    const lyricOffset = ref(0);
    const wrapper = mount({
      setup() {
        useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          renderPlan: () => ({
            requiresAnimationFrame: false,
            nextChangeAtSeconds: lyricOffset.value === 0 ? 10 : 0.2,
          }),
          // This intentionally returns a fresh array, matching LyricsView.
          renderPlanDependencies: () => [lyricOffset.value],
        });
        return () => null;
      },
    });

    await nextTick();
    animationFrames.shift()?.(0);
    expect(wakeTimers).toHaveLength(1);
    expect(wakeTimers[0]?.delay).toBeGreaterThan(9_000);

    nativePosition.value = 0.0004;
    await nextTick();
    expect(clearTimeout).not.toHaveBeenCalled();
    expect(animationFrames).toHaveLength(0);

    lyricOffset.value = 0.1;
    await nextTick();
    expect(clearTimeout).toHaveBeenCalledWith(1);
    expect(animationFrames).toHaveLength(1);

    animationFrames.shift()?.(0);
    expect(wakeTimers).toHaveLength(2);
    expect(wakeTimers[1]?.delay).toBeLessThan(1_000);
    wrapper.unmount();
  });

  it("sleeps line-timed lyrics until their next visual boundary", async () => {
    const nativePosition = ref(0);
    const playing = ref(true);
    let displayedPosition!: { value: number };
    const wrapper = mount({
      setup() {
        displayedPosition = useSmoothLyricsPlaybackPosition({
          currentTime: () => nativePosition.value,
          isPlaying: () => playing.value,
          renderPlan: (positionSeconds) => positionSeconds < 0.2
            ? { requiresAnimationFrame: false, nextChangeAtSeconds: 0.2 }
            : { requiresAnimationFrame: false, nextChangeAtSeconds: null },
        });
        return () => null;
      },
    });

    await nextTick();
    animationFrames.shift()?.(0);
    expect(animationFrames).toHaveLength(0);

    await vi.advanceTimersByTimeAsync(200);
    expect(animationFrames).toHaveLength(1);
    animationFrames.shift()?.(200);
    expect(displayedPosition.value).toBeCloseTo(0.2, 3);
    expect(animationFrames).toHaveLength(0);
    wrapper.unmount();
  });
});
