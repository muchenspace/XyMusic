import { describe, expect, it } from "vitest";
import type { LyricTiming } from "../src/domain/music";
import { buildLyrics as parseSingleLyrics, type LyricResource } from "../src/infrastructure/lyrics/LyricsParser";

describe("lyrics parser", () => {
	it("parses ordinary LRC as line-timed lyrics", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "[00:01.00]First line\n[00:03.00]Second line",
			isDefault: true,
		}]);

		expect(result).toMatchObject({
			 timing: "LINE",
			 synchronized: true,
			 lines: [
				{ time: 1, text: "First line" },
				{ time: 3, text: "Second line" },
			],
		});
	});

	it("rejects complete word-timed content when the server declares LINE", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "[00:01.00]<00:01.00>First<00:01.50> second",
			isDefault: true,
		}])).toThrow();
	});

	it("parses a mixed document as LINE without using embedded word times", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "[00:01.00]<00:01.00>First<00:01.50> line\n[00:03.00]Second line",
			isDefault: true,
		}]);

		expect(result).toMatchObject({
			timing: "LINE",
			lines: [{ time: 1, text: "First line" }, { time: 3, text: "Second line" }],
		});
		expect(result?.lines[0]?.words).toBeUndefined();
	});

	it("normalizes a leading BOM before enforcing declared timing", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "\uFEFF[00:01]<00:01>word",
			isDefault: true,
		}])).toThrow();
	});

	it("treats an NBSP-prefixed enhanced line as server-declared LINE", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "\u00A0[00:01]<00:01>word",
			isDefault: true,
		}]);

		expect(result?.lines).toEqual([{ time: 1, text: "word" }]);
		expect(result?.lines[0]?.words).toBeUndefined();
	});

	it("displays a server-declared LINE resource prefixed with U+0085", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "\u0085[00:01]line",
			isDefault: true,
		}]);

		expect(result?.lines).toEqual([{ time: 1, text: "line" }]);
	});

	it("parses every line timestamp when timestamps are separated by whitespace", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "[00:01] [00:02]chorus",
			isDefault: true,
		}]);

		expect(result?.lines).toEqual([
			{ time: 1, text: "chorus" },
			{ time: 2, text: "chorus" },
		]);
	});

	it("parses Enhanced LRC word markers only when the server declares WORD", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01.00]<00:01.00>First<00:01.50> second<00:02.00>",
			isDefault: true,
		}]);

		expect(result).toMatchObject({
			timing: "WORD",
			synchronized: true,
			lines: [{
				time: 1,
				text: "First second",
				words: [
					{ time: 1, endTime: 1.5, text: "First" },
					{ time: 1.5, endTime: 2, text: " second" },
				],
			}],
		});
	});

	it("parses a terminal Enhanced LRC marker as the final word end time", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01.00]<00:01.00>First<00:02.00>",
			isDefault: true,
		}]);

		expect(result?.lines[0]?.words).toEqual([{ time: 1, endTime: 2, text: "First" }]);
	});

	it("rejects multiple resources instead of selecting a primary lyric", () => {
		expect(() => buildLyrics("track-1", [
			{
				language: "zh",
				format: "LRC",
				timing: "LINE",
				content: "[00:01.00]Primary line",
				isDefault: true,
			},
			{
				language: "en",
				format: "LRC",
				timing: "WORD",
				content: "[00:01.00]<00:01.00>Translated line",
				isDefault: false,
			},
		])).toThrow();
	});

	it("rejects multiple resources even when one is word-timed", () => {
		expect(() => buildLyrics("track-1", [
			{
				language: "zh",
				format: "LRC",
				timing: "WORD",
				content: "[00:01.00]<00:01.00>Primary<00:01.50> line",
				isDefault: true,
			},
			{
				language: "en",
				format: "LRC",
				timing: "LINE",
				content: "[00:01.00]Translated line",
				isDefault: false,
			},
		])).toThrow();
	});

	it("rejects a WORD resource without word timestamps", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01.00]First line",
			isDefault: true,
		}])).toThrow();
	});

	it("rejects WORD content with text before the first word timestamp", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01.00]prefix<00:01.00>word",
			isDefault: true,
		}])).toThrow();
	});

	it("rejects invalid WORD timestamps and unknown formats", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh", format: "LRC", timing: "WORD",
			content: "[00:01.00]<00:60.00>word", isDefault: true,
		}])).toThrow();
		expect(() => buildLyrics("track-1", [{
			language: "zh", format: "QRC", timing: "LINE",
			content: "plain", isDefault: true,
		}])).toThrow();
	});

	it("rejects an invalid WORD marker left after a valid word timestamp", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01.00]<00:01.00>valid<00:60.00>invalid",
			isDefault: true,
		}])).toThrow();
	});

	it("rejects an unterminated invalid WORD marker left after a valid word timestamp", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01.00]<00:01.00>valid<00:60.00invalid",
			isDefault: true,
		}])).toThrow();
	});

	it("rejects a missing or unknown server timing instead of assuming LINE", () => {
		for (const timing of [undefined, "UNKNOWN"]) {
			expect(() => buildLyrics("track-1", [{
				language: "zh",
				format: "LRC",
				timing: timing as LyricTiming,
				content: "[00:01.00]line",
				isDefault: true,
			}])).toThrow();
		}
	});

	it("rejects WORD timing for plain lyrics", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "PLAIN",
			timing: "WORD",
			content: "plain lyric",
			isDefault: true,
		}])).toThrow();
	});

	it("rejects an empty PLAIN resource declared as WORD", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "PLAIN",
			timing: "WORD",
			content: "",
			isDefault: true,
		}])).toThrow();
	});

	it("rejects a Unicode-whitespace LRC resource declared as WORD", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "\u00A0\u0085\t",
			isDefault: true,
		}])).toThrow();
	});

	it("omits a valid whitespace-only LINE resource after validation", () => {
		expect(buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "\u00A0\u0085\t",
			isDefault: true,
		}])).toBeNull();
	});

	it("rejects an invalid unselected third resource", () => {
		expect(() => buildLyrics("track-1", [
			{
				language: "zh",
				format: "LRC",
				timing: "LINE",
				content: "[00:01.00]Primary line",
				isDefault: true,
			},
			{
				language: "en",
				format: "LRC",
				timing: "LINE",
				content: "[00:01.00]Translation",
				isDefault: false,
			},
			{
				language: "ja",
				format: "LRC",
				timing: "WORD",
				content: "[00:01.00]Missing word marker",
				isDefault: false,
			},
		])).toThrow();
	});

	it("rejects an invalid unselected blank third resource", () => {
		expect(() => buildLyrics("track-1", [
			{
				language: "zh",
				format: "LRC",
				timing: "LINE",
				content: "[00:01.00]Primary line",
				isDefault: true,
			},
			{
				language: "en",
				format: "LRC",
				timing: "LINE",
				content: "[00:01.00]Translation",
				isDefault: false,
			},
			{
				language: "ja",
				format: "LRC",
				timing: "WORD",
				content: "\u3000",
				isDefault: false,
			},
		])).toThrow();
	});

	it("preserves invalid closed and unterminated markers in LINE lyrics", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "[00:01]<00:01>valid<bad>invalid\n[00:02]<00:02>valid<bad",
			isDefault: true,
		}]);

		expect(result?.lines).toEqual([
			{ time: 1, text: "valid<bad>invalid" },
			{ time: 2, text: "valid<bad" },
		]);
		expect(result?.lines.every((line) => line.words === undefined)).toBe(true);
	});

	it("rejects a WORD document whose marked lyric text is only whitespace", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01.00]<00:01.00>   ",
			isDefault: true,
		}])).toThrow();
	});

	it("accepts untimed ordinary text mixed with enhanced content when declared LINE", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "[00:01.00]<00:01.00>Timed line\nordinary untimed text",
			isDefault: true,
		}]);

		expect(result?.lines).toEqual([{ time: 1, text: "Timed line" }]);
	});

	it("accepts metadata-only lines alongside complete enhanced WORD lyrics", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[ar:Artist]\n[custom_key:value]\n[00:01.0]<00:01.0>Word",
			isDefault: true,
		}]);

		expect(result?.lines).toEqual([{
			time: 1,
			text: "Word",
			words: [{ time: 1, endTime: 1.36, text: "Word" }],
		}]);
	});

	it("rejects decreasing WORD timestamps within one line", () => {
		expect(() => buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01]<00:11>later<00:10>earlier",
			isDefault: true,
		}])).toThrow();
	});

	it("accepts duplicate WORD timestamps within one line", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01]<00:10>same<00:10> time",
			isDefault: true,
		}]);

		expect(result?.lines[0]?.words?.map((word) => word.time)).toEqual([10, 10]);
	});

	it("resets WORD timestamp ordering at each new lyric line", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01]<00:11>first\n[00:02]<00:10>second",
			isDefault: true,
		}]);

		expect(result?.lines.map((line) => line.words?.[0]?.time)).toEqual([11, 10]);
	});

	it("normalizes BOM and mixed line endings without joining lines", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "\uFEFF[00:01]first\r[00:02]second\r\n[00:03]third\n[00:04]fourth",
			isDefault: true,
		}]);

		expect(result?.lines.map((line) => line.text)).toEqual(["first", "second", "third", "fourth"]);
	});

	it("applies offset to lines and words and clamps negative results", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[offset:-1500]\n[00:01]<00:01>A<00:02>B<00:03>",
			isDefault: true,
		}]);

		expect(result?.lines[0]).toEqual({
			time: 0,
			text: "AB",
			words: [
				{ time: 0, endTime: 0.5, text: "A" },
				{ time: 0.5, endTime: 1.5, text: "B" },
			],
		});
	});

	it("rebases word intervals for every repeated line timestamp", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01][00:10]<00:01>A<00:02>B<00:03>",
			isDefault: true,
		}]);

		expect(result?.lines.map((line) => line.words)).toEqual([
			[
				{ time: 1, endTime: 2, text: "A" },
				{ time: 2, endTime: 3, text: "B" },
			],
			[
				{ time: 10, endTime: 11, text: "A" },
				{ time: 11, endTime: 12, text: "B" },
			],
		]);
	});

	it("infers final word ends and bounds them at the next line", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01]<00:01>final\n[00:01.20]<00:01.20>next",
			isDefault: true,
		}]);

		expect(result?.lines[0]?.words?.[0]?.endTime).toBe(1.2);
		expect(result?.lines[1]?.words?.[0]?.endTime).toBe(1.56);
	});

	it("keeps explicit final word ends authoritative past the next line", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "WORD",
			content: "[00:01]<00:01>A<00:03>\n[00:02]<00:02>B",
			isDefault: true,
		}]);

		expect(result?.lines[0]?.words?.[0]?.endTime).toBe(3);
	});

	it("parses offset from a shared metadata line and clamps duplicate boundaries", () => {
		const sharedOffset = buildLyrics("track-1", [{
			language: "zh", format: "LRC", timing: "WORD",
			content: "[ar:Artist][OFFSET:+500]\n[00:01]<00:01>A", isDefault: true,
		}]);
		const duplicates = buildLyrics("track-2", [{
			language: "zh", format: "LRC", timing: "WORD",
			content: "[offset:-2000]\n[00:01][00:02]<00:01>A", isDefault: true,
		}]);

		expect(sharedOffset?.lines[0]).toEqual({
			time: 1.5,
			text: "A",
			words: [{ time: 1.5, endTime: 1.74, text: "A" }],
		});
		expect(duplicates?.lines.map((line) => line.words?.[0]?.endTime)).toEqual([0, 0.24]);
	});

	it("saturates the minimum signed offset at zero", () => {
		const result = buildLyrics("track-1", [{
			language: "zh", format: "LRC", timing: "WORD",
			content: "[offset:-9223372036854775808]\n[00:01]<00:01>A<00:02>", isDefault: true,
		}]);

		expect(result?.lines[0]).toEqual({
			time: 0,
			text: "A",
			words: [{ time: 0, endTime: 0, text: "A" }],
		});
	});

	it("only parses line timestamps at the prefix and strips following LRC tags", () => {
		const result = buildLyrics("track-1", [{
			language: "zh",
			format: "LRC",
			timing: "LINE",
			content: "[00:10.50][Verse] Later\ntext [00:02]not a prefix",
			isDefault: true,
		}]);

		expect(result?.lines).toEqual([{ time: 10.5, text: "Later" }]);
	});
});

type TestResource = LyricResource & { isDefault?: boolean };

function buildLyrics(trackId: string, resources: TestResource | TestResource[]): ReturnType<typeof parseSingleLyrics> {
	if (Array.isArray(resources)) {
		if (resources.length > 1) throw new Error("Multiple lyric resources are not supported");
		return parseSingleLyrics(trackId, resources[0] ?? null);
	}
	return parseSingleLyrics(trackId, resources);
}
