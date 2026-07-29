import { describe, expect, it } from "vitest";
import type { Lyrics } from "../src/domain/music";
import {
	interpolateLyricPlaybackSeconds,
	resolveLyricPlaybackPosition,
	resolveLyricWordProgress,
} from "../src/domain/lyricsTimeline";

describe("playback lyrics timeline", () => {
	it("selects the active ordinary LRC line without estimating line progress", () => {
		const lyrics = synchronizedLyrics("LINE", [
			{ time: 10, text: "First line" },
			{ time: 14, text: "Second line" },
		]);

		expect(resolveLyricPlaybackPosition(lyrics, 12)).toEqual({
			lineIndex: 0,
			wordIndex: -1,
		});
	});

	it("selects a word from explicit Enhanced LRC word timestamps", () => {
		const lyrics = synchronizedLyrics("WORD", [
			{
				time: 10,
				text: "First second",
				words: [
					{ time: 10, endTime: 11.5, text: "First" },
					{ time: 11.5, endTime: 13, text: " second" },
				],
			},
		]);

		expect(resolveLyricPlaybackPosition(lyrics, 10.5)).toEqual({
			lineIndex: 0,
			wordIndex: 0,
		});
		expect(resolveLyricPlaybackPosition(lyrics, 12)).toEqual({
			lineIndex: 0,
			wordIndex: 1,
		});
	});

	it("uses explicit word end times for continuous progress", () => {
		const word = { time: 10, endTime: 12, text: "First" };

		expect(resolveLyricWordProgress(word, 10)).toBe(0);
		expect(resolveLyricWordProgress(word, 11)).toBe(0.5);
		expect(resolveLyricWordProgress(word, 12)).toBe(1);
	});

	it("advances a sampled playing position between audio updates", () => {
		const clock = { positionSeconds: 10, anchoredAtMs: 1_000, isPlaying: true };

		expect(interpolateLyricPlaybackSeconds(clock, 1_016)).toBeCloseTo(10.016, 6);
		expect(interpolateLyricPlaybackSeconds({ ...clock, isPlaying: false }, 1_500)).toBe(10);
	});
});

function synchronizedLyrics(timing: Lyrics["timing"], lines: Lyrics["lines"]): Lyrics {
  return {
    trackId: "track-1",
		lines,
		source: "zh",
		synchronized: true,
		timing,
	};
}
