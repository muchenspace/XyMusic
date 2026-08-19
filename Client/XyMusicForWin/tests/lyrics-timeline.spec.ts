import { describe, expect, it } from "vitest";
import type { Lyrics } from "../src/domain/music";
import {
	countLyricGraphemes,
	interpolateLyricPlaybackSeconds,
	resolveLyricPlaybackRenderPlan,
	resolveLyricPlaybackPosition,
	resolveLyricWordEnd,
	resolveLyricWordProgress,
	shouldReanchorLyricPlaybackClock,
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

	it("uses a bounded grapheme duration for an unterminated final word", () => {
		expect(resolveLyricWordEnd({ time: 1, text: "A" })).toBe(1.24);
		expect(resolveLyricWordEnd({ time: 1, text: "final" })).toBe(1.45);
		expect(resolveLyricWordEnd({ time: 1, text: "a very long final word" })).toBe(1.9);
		expect(resolveLyricWordProgress({ time: 1, text: "final" }, 1.225)).toBe(0.5);
	});

	it("counts the extended grapheme units used by Android", () => {
		const graphemes = [
			"\u{1F468}\u200D\u{1F469}\u200D\u{1F467}\u200D\u{1F466}",
			"e\u0301",
			"\u{1F1FA}\u{1F1F8}",
			"\u2764\uFE0F",
			"\u{1F44B}\u{1F3FD}",
			"\u{1F3F4}\u{E0065}\u{E006E}\u{E0067}\u{E006C}\u{E0061}\u{E006E}\u{E0064}\u{E007F}",
		];

		expect(graphemes.map(countLyricGraphemes)).toEqual([1, 1, 1, 1, 1, 1]);
		expect(resolveLyricWordEnd({ time: 1, text: graphemes.join("") })).toBe(1.54);
	});

	it("uses adjacent boundaries without overriding explicit word ends", () => {
		const word = { time: 2, text: "word" };

		expect(resolveLyricWordEnd(word, 1)).toBe(2);
		expect(resolveLyricWordEnd(word, undefined, 1.5)).toBe(2);
		expect(resolveLyricWordEnd({ ...word, endTime: 3 }, 1, 1.5)).toBe(3);
	});

	it("advances a sampled playing position between audio updates", () => {
		const clock = { positionSeconds: 10, anchoredAtMs: 1_000, isPlaying: true };

		expect(interpolateLyricPlaybackSeconds(clock, 1_016)).toBeCloseTo(10.016, 6);
		expect(interpolateLyricPlaybackSeconds({ ...clock, isPlaying: false }, 1_500)).toBe(10);
	});

	it("treats a 250ms clock error as smooth correction and larger errors as a snap", () => {
		const current = { positionSeconds: 10, anchoredAtMs: 1_000, isPlaying: true };
		expect(shouldReanchorLyricPlaybackClock(
			current,
			{ positionSeconds: 10.25, anchoredAtMs: 1_000, isPlaying: true },
			1_000,
		)).toBe(false);
		expect(shouldReanchorLyricPlaybackClock(
			current,
			{ positionSeconds: 10.251, anchoredAtMs: 1_000, isPlaying: true },
			1_000,
		)).toBe(true);
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

	it("animates an inferred final-word interval and then sleeps", () => {
		const lyrics = synchronizedLyrics("WORD", [{
			time: 1,
			text: "final",
			words: [{ time: 1, text: "final" }],
		}]);

		expect(resolveLyricPlaybackRenderPlan(lyrics, 1.2)).toEqual({
			requiresAnimationFrame: true,
			nextChangeAtSeconds: 1.45,
		});
		expect(resolveLyricPlaybackRenderPlan(lyrics, 1.5)).toEqual({
			requiresAnimationFrame: false,
			nextChangeAtSeconds: null,
		});
	});

	it("bounds an inferred final-word render interval at the next line", () => {
		const lyrics = synchronizedLyrics("WORD", [
			{ time: 1, text: "final", words: [{ time: 1, text: "final" }] },
			{ time: 1.2, text: "next", words: [{ time: 1.2, endTime: 1.5, text: "next" }] },
		]);

		expect(resolveLyricPlaybackRenderPlan(lyrics, 1.1)).toEqual({
			requiresAnimationFrame: true,
			nextChangeAtSeconds: 1.2,
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
