import { afterEach, describe, expect, it, vi } from "vitest";
import { HtmlAudioPlayer } from "../src/infrastructure/audio/HtmlAudioPlayer";

vi.mock("hls.js", () => ({
  default: {
    isSupported: () => false,
  },
}));

describe("HTML audio bandwidth measurement", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("merges fast short fills into a meaningful bandwidth sample", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
    const elements = stubAudio();
    const player = new HtmlAudioPlayer();
    const samples: Array<{ bitsPerSecond: number; durationMs: number }> = [];
    player.onBandwidthSample((sample) => samples.push(sample));
    const audio = elements[0]!;

    // Segment bitrate on a 128kbps stream; a fast link fills 0.1s of audio in 30ms.
    const loadPromise = player.load("https://example.test/audio.m4a", undefined, { bitrate: 128_000, duration: 180 });
    audio.readyState = 1;
    audio.dispatchEvent(new Event("loadedmetadata"));
    await loadPromise;
    audio.buffered = ranges(0);

    // 30ms window, 0.002s buffered -> below the 30ms floor, must be accumulated.
    advanceMicroseconds(15);
    audio.buffered = ranges(0.002);
    audio.dispatchEvent(new Event("progress"));

    advanceMicroseconds(15);
    audio.buffered = ranges(0.032);
    audio.dispatchEvent(new Event("progress"));

    // Now 30ms total: 0.032s * 128_000 / (30/1000) = 136_533 bps
    expect(samples).toHaveLength(1);
    const sample = samples[0]!;
    expect(sample.durationMs).toBe(30);
    expect(sample.bitsPerSecond).toBeCloseTo(0.032 * 128_000 / 0.03, 0);
  });

  it("emits samples for long windows immediately", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
    const elements = stubAudio();
    const player = new HtmlAudioPlayer();
    const samples: Array<{ bitsPerSecond: number; durationMs: number }> = [];
    player.onBandwidthSample((sample) => samples.push(sample));
    const audio = elements[0]!;

    const loadPromise = player.load("https://example.test/audio.m4a", undefined, { bitrate: 128_000, duration: 180 });
    audio.readyState = 1;
    audio.dispatchEvent(new Event("loadedmetadata"));
    await loadPromise;
    audio.buffered = ranges(0);

    advanceMicroseconds(500);
    audio.buffered = ranges(1);
    audio.dispatchEvent(new Event("progress"));

    expect(samples).toHaveLength(1);
    const sample = samples[0]!;
    expect(sample.durationMs).toBe(500);
    expect(sample.bitsPerSecond).toBeCloseTo(1 * 128_000 / 0.5, 0);
  });

  it("caps LAN-rate samples below the estimator sanity filter", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
    const elements = stubAudio();
    const player = new HtmlAudioPlayer();
    const samples: Array<{ bitsPerSecond: number; durationMs: number }> = [];
    player.onBandwidthSample((sample) => samples.push(sample));
    const audio = elements[0]!;

    const loadPromise = player.load("https://example.test/audio.m4a", undefined, { bitrate: 128_000, duration: 180 });
    audio.readyState = 1;
    audio.dispatchEvent(new Event("loadedmetadata"));
    await loadPromise;
    audio.buffered = ranges(0);

    // 20s of audio buffered in 30ms on a 128kbps stream: raw ~85.3Mbps, above
    // both the cap and the estimator's 100Mbps reject line if uncapped. The
    // sample must be capped to 50Mbps so the estimator keeps it.
    advanceMicroseconds(30);
    audio.buffered = ranges(20);
    audio.dispatchEvent(new Event("progress"));

    expect(samples).toHaveLength(1);
    expect(samples[0]!.durationMs).toBe(30);
    expect(samples[0]!.bitsPerSecond).toBe(50_000_000);
  });

  it("discards stale pending bytes after an idle gap", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
    const elements = stubAudio();
    const player = new HtmlAudioPlayer();
    const samples: Array<{ bitsPerSecond: number; durationMs: number }> = [];
    player.onBandwidthSample((sample) => samples.push(sample));
    const audio = elements[0]!;

    const loadPromise = player.load("https://example.test/audio.m4a", undefined, { bitrate: 128_000, duration: 180 });
    audio.readyState = 1;
    audio.dispatchEvent(new Event("loadedmetadata"));
    await loadPromise;
    audio.buffered = ranges(0);

    // A short sub-floor burst is accumulated.
    advanceMicroseconds(15);
    audio.buffered = ranges(0.002);
    audio.dispatchEvent(new Event("progress"));
    expect(samples).toHaveLength(0);

    // Idle gap > 5s resets the pending window.
    advanceMicroseconds(6_000);
    audio.dispatchEvent(new Event("progress"));

    // The first growth after the gap resets the pending window (its elapsed
    // time attribution is untrustworthy) and is not emitted.
    advanceMicroseconds(6_000);
    audio.buffered = ranges(0.12);
    audio.dispatchEvent(new Event("progress"));
    expect(samples).toHaveLength(0);

    // A subsequent burst is measured from a fresh window and must not include
    // the stale 0.002s that was pending before the gap.
    advanceMicroseconds(50);
    audio.buffered = ranges(0.2);
    audio.dispatchEvent(new Event("progress"));

    expect(samples).toHaveLength(1);
    const sample = samples[0]!;
    expect(sample.durationMs).toBe(50);
    expect(sample.bitsPerSecond).toBeCloseTo(0.08 * 128_000 / 0.05, 0);
  });
});

function stubAudio(): FakeAudioElement[] {
  const audioElements: FakeAudioElement[] = [];
  vi.stubGlobal("Audio", function FakeAudioConstructor() {
    const audio = new FakeAudioElement();
    audioElements.push(audio);
    return audio;
  });
  return audioElements;
}

class FakeAudioElement extends EventTarget {
  currentTime = 0;
  duration = 180;
  paused = true;
  volume = 1;
  readyState = 0;
  error: MediaError | null = null;
  preload = "";
  buffered: TimeRangesLike = ranges(0);
  private readonly attributes = new Set<string>();
  private source = "";

  get src(): string {
    return this.source;
  }

  set src(value: string) {
    this.source = value;
    if (value) this.attributes.add("src");
    else this.attributes.delete("src");
  }

  async play(): Promise<void> {
    this.paused = false;
    this.dispatchEvent(new Event("play"));
  }

  pause(): void {
    if (this.paused) return;
    this.paused = true;
    this.dispatchEvent(new Event("pause"));
  }

  load(): void {}

  hasAttribute(name: string): boolean {
    return this.attributes.has(name);
  }

  removeAttribute(name: string): void {
    this.attributes.delete(name);
    if (name === "src") this.source = "";
  }
}

interface TimeRangesLike {
  length: number;
  start(index: number): number;
  end(index: number): number;
}

function ranges(end: number): TimeRangesLike {
  return { length: end > 0 ? 1 : 0, start: () => 0, end: () => end };
}

function advanceMicroseconds(microseconds: number): void {
  vi.advanceTimersByTime(microseconds);
}
