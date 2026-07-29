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
});
