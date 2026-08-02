import { describe, expect, it } from "vitest";
import type { Lyrics } from "../src/domain/music";
import {
	interpolateLyricPlaybackSeconds,
	resolveLyricPlaybackRenderPlan,
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

	it("sleeps line-timed lyrics until the next line boundary", () => {
		const lyrics = synchronizedLyrics("LINE", [
			{ time: 0, text: "First line" },
			{ time: 4, text: "Second line" },
		]);

		expect(resolveLyricPlaybackRenderPlan(lyrics, 1)).toEqual({
			requiresAnimationFrame: false,
			nextChangeAtSeconds: 4,
		});
	});

	it("uses animation frames only while an enhanced word fill is active", () => {
		const lyrics = synchronizedLyrics("WORD", [{
			time: 1,
			text: "First second",
			words: [
				{ time: 1, endTime: 1.5, text: "First" },
				{ time: 2, endTime: 2.5, text: " second" },
			],
		}]);

		expect(resolveLyricPlaybackRenderPlan(lyrics, 1.2)).toEqual({
			requiresAnimationFrame: true,
			nextChangeAtSeconds: 1.5,
		});
		expect(resolveLyricPlaybackRenderPlan(lyrics, 1.7)).toEqual({
			requiresAnimationFrame: false,
			nextChangeAtSeconds: 2,
		});
	});

	it("sleeps until the next line instead of waking for hidden future-line words", () => {
		const lyrics = synchronizedLyrics("WORD", [
			{
				time: 0,
				text: "Current line",
				words: [{ time: 0, endTime: 0.5, text: "Current line" }],
			},
			{
				time: 4,
				text: "Future line",
				words: [{ time: 2, endTime: 2.5, text: "Future line" }],
			},
		]);

		expect(resolveLyricPlaybackRenderPlan(lyrics, 1)).toEqual({
			requiresAnimationFrame: false,
			nextChangeAtSeconds: 4,
		});
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
