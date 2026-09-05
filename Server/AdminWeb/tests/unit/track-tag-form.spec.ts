import { describe, expect, it } from "vitest";
import { normalizeTrackTagScalars, parseLyricsOffset, updateLyricsOffset } from "@/features/music/presentation/track-tag-form";

const validInput = {
  releaseDate: "2024-02-29",
  trackNumber: "2",
  trackTotal: "12",
  discNumber: "1",
  discTotal: "2",
  bpm: "123.45",
  isrc: "us-abc-12-34567",
  lyricsLanguage: "zh-CN",
};

describe("track Tag form normalization", () => {
  it("normalizes scalar fields before they reach the API", () => {
    expect(normalizeTrackTagScalars(validInput)).toEqual({
      releaseDate: "2024-02-29",
      trackNumber: 2,
      trackTotal: 12,
      discNumber: 1,
      discTotal: 2,
      bpm: 123.45,
      isrc: "USABC1234567",
      lyricsLanguage: "zh-CN",
    });
  });

  it("rejects impossible dates and inconsistent totals", () => {
    expect(() => normalizeTrackTagScalars({ ...validInput, releaseDate: "2023-02-29" })).toThrow("日期无效");
    expect(() => normalizeTrackTagScalars({ ...validInput, trackNumber: "13" })).toThrow("音轨号不能大于总音轨");
    expect(() => normalizeTrackTagScalars({ ...validInput, discNumber: "1.5" })).toThrow("碟号必须是");
  });

  it("requires valid ISRC and language tags", () => {
    expect(() => normalizeTrackTagScalars({ ...validInput, isrc: "invalid" })).toThrow("ISRC");
    expect(() => normalizeTrackTagScalars({ ...validInput, lyricsLanguage: "中文" })).toThrow("歌词语言");
  });

  describe("lyrics offset parsing and updating", () => {
    it("parses offset from lrc content", () => {
      expect(parseLyricsOffset("[offset:500]\n[00:01.00]Line")).toBe(500);
      expect(parseLyricsOffset("[ti:Song]\n[offset:-800]\n[00:01.00]Line")).toBe(-800);
      expect(parseLyricsOffset("[offset: 0 ]\n[00:01.00]Line")).toBe(0);
      expect(parseLyricsOffset("[00:01.00]No offset")).toBe(0);
      expect(parseLyricsOffset("")).toBe(0);
    });

    it("updates existing offset tag in lrc content", () => {
      const lrc = "[ti:Song]\n[offset:100]\n[00:01.00]Line";
      expect(updateLyricsOffset(lrc, 500)).toBe("[ti:Song]\n[offset:500]\n[00:01.00]Line");
      expect(updateLyricsOffset(lrc, -300)).toBe("[ti:Song]\n[offset:-300]\n[00:01.00]Line");
      expect(updateLyricsOffset(lrc, 0)).toBe("[ti:Song]\n[offset:0]\n[00:01.00]Line");
    });

    it("inserts offset tag after metadata tags or before first timestamped line", () => {
      const lrc = "[ti:Song]\n[ar:Artist]\n[00:01.00]Line";
      expect(updateLyricsOffset(lrc, 800)).toBe("[ti:Song]\n[ar:Artist]\n[offset:800]\n[00:01.00]Line");

      const pureLrc = "[00:01.00]First line\n[00:02.00]Second line";
      expect(updateLyricsOffset(pureLrc, -200)).toBe("[offset:-200]\n[00:01.00]First line\n[00:02.00]Second line");
    });

    it("handles empty content and bounds check", () => {
      expect(updateLyricsOffset("", 0)).toBe("");
      expect(updateLyricsOffset("", 500)).toBe("[offset:500]\n");
      expect(updateLyricsOffset("[00:01.00]A", 999_999)).toBe("[offset:300000]\n[00:01.00]A");
      expect(updateLyricsOffset("[00:01.00]A", -999_999)).toBe("[offset:-300000]\n[00:01.00]A");
    });
  });
});

