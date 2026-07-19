import { describe, expect, it } from "vitest";
import { normalizeTrackTagScalars } from "@/features/music/presentation/track-tag-form";

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
});
