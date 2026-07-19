import { describe, expect, it } from "vitest";
import type { Lyrics } from "../src/domain/music";
import { resolveLyricPlaybackPosition } from "../src/domain/lyricsTimeline";

describe("playback lyrics timeline", () => {
  it("provides continuous progress between ordinary LRC lines", () => {
    const lyrics = synchronizedLyrics([
      { time: 10, text: "First line" },
      { time: 14, text: "Second line" },
    ]);

    expect(resolveLyricPlaybackPosition(lyrics, 12)).toEqual({
      lineIndex: 0,
      lineProgress: 0.5,
    });
  });

  it("uses a bounded fallback duration for the final LRC line", () => {
    const lyrics = synchronizedLyrics([
      { time: 10, text: "Final line" },
    ]);

    expect(resolveLyricPlaybackPosition(lyrics, 12)).toEqual({
      lineIndex: 0,
      lineProgress: 0.5,
    });

    expect(resolveLyricPlaybackPosition(lyrics, 14)).toEqual({
      lineIndex: -1,
      lineProgress: 0,
    });
  });
});

function synchronizedLyrics(lines: Lyrics["lines"]): Lyrics {
  return {
    trackId: "track-1",
    lines,
    source: "zh",
    synchronized: true,
  };
}
