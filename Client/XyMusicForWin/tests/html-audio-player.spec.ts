import { afterEach, describe, expect, it, vi } from "vitest";
import { HtmlAudioPlayer, normalizedAudioVolume } from "../src/infrastructure/audio/HtmlAudioPlayer";

describe("HTML audio volume normalization", () => {
  it.each([
    [-0.000288, 0],
    [0, 0],
    [0.72, 0.72],
    [1, 1],
    [1.000001, 1],
    [Number.NaN, 0],
    [Number.POSITIVE_INFINITY, 0],
  ])("normalizes %s to %s", (input, expected) => {
    expect(normalizedAudioVolume(input)).toBe(expected);
  });
});

describe("HTML audio progress updates", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("coalesces dense native timeupdate events with the synthetic progress loop", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
    const audioElements = stubAudio();
    const player = new HtmlAudioPlayer();
    const updates: Array<{ currentTime: number; duration: number; paused: boolean }> = [];
    player.onUpdate((snapshot) => updates.push(snapshot));
    const audio = audioElements[0]!;

    await player.play();
    expect(updates).toEqual([{ currentTime: 0, duration: 180, paused: false }]);
    updates.splice(0);

    for (let tick = 1; tick <= 20; tick += 1) {
      vi.advanceTimersByTime(5);
      audio.currentTime = tick / 100;
      audio.dispatchEvent(new Event("timeupdate"));
    }

    expect(updates).toHaveLength(1);

    audio.duration = 240;
    audio.dispatchEvent(new Event("durationchange"));
    expect(updates).toHaveLength(2);
    expect(updates[1]).toMatchObject({ duration: 240, paused: false });

    player.seek(42);
    expect(updates).toHaveLength(3);
    expect(updates[2]).toMatchObject({ currentTime: 42, paused: false });

    player.pause();
    expect(updates).toHaveLength(4);
    vi.advanceTimersByTime(1_000);
    expect(updates).toHaveLength(4);
  });

  it("uses the synthetic progress loop when native timeupdate events are sparse", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
    const audioElements = stubAudio();
    const player = new HtmlAudioPlayer();
    const updates: Array<{ currentTime: number; duration: number; paused: boolean }> = [];
    player.onUpdate((snapshot) => updates.push(snapshot));
    const audio = audioElements[0]!;

    await player.play();
    updates.splice(0);
    audio.currentTime = 0.5;
    vi.advanceTimersByTime(67);

    expect(updates).toEqual([{ currentTime: 0.5, duration: 180, paused: false }]);
  });

  it("does not reschedule the synthetic loop after its last listener unsubscribes during an update", async () => {
    vi.useFakeTimers({ toFake: ["Date", "performance", "setTimeout", "clearTimeout"] });
    const audioElements = stubAudio();
    const player = new HtmlAudioPlayer();
    const audio = audioElements[0]!;
    let updates = 0;
    let unsubscribe = () => undefined;
    unsubscribe = player.onUpdate(() => {
      updates += 1;
      if (updates === 2) unsubscribe();
    });

    await player.play();
    audio.currentTime = 0.5;
    vi.advanceTimersByTime(67);

    expect(updates).toBe(2);
    expect(vi.getTimerCount()).toBe(0);
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
