export interface TrackTagScalarInput {
  releaseDate: string;
  trackNumber: string;
  trackTotal: string;
  discNumber: string;
  discTotal: string;
  bpm: string;
  isrc: string;
  lyricsLanguage: string;
}

export interface TrackTagScalars {
  releaseDate: string | null;
  trackNumber: number | null;
  trackTotal: number | null;
  discNumber: number | null;
  discTotal: number | null;
  bpm: number | null;
  isrc: string | null;
  lyricsLanguage: string;
}

const LANGUAGE_PATTERN = /^(?:[A-Za-z]{2,8}(?:-[A-Za-z0-9]{2,8})*|und)$/;
const ISRC_PATTERN = /^[A-Z]{2}[A-Z0-9]{3}[0-9]{7}$/;

export function normalizeTrackTagScalars(input: TrackTagScalarInput): TrackTagScalars {
  const trackNumber = optionalNumber(input.trackNumber, "音轨号", 9_999, true);
  const trackTotal = optionalNumber(input.trackTotal, "总音轨", 9_999, true);
  const discNumber = optionalNumber(input.discNumber, "碟号", 999, true);
  const discTotal = optionalNumber(input.discTotal, "总碟数", 999, true);

  if (trackTotal !== null && trackNumber === null) throw new Error("填写总音轨时也需要填写音轨号");
  if (trackNumber !== null && trackTotal !== null && trackNumber > trackTotal) throw new Error("音轨号不能大于总音轨");
  if (discTotal !== null && discNumber === null) throw new Error("填写总碟数时也需要填写碟号");
  if (discNumber !== null && discTotal !== null && discNumber > discTotal) throw new Error("碟号不能大于总碟数");

  return {
    releaseDate: normalizeReleaseDate(input.releaseDate),
    trackNumber,
    trackTotal,
    discNumber,
    discTotal,
    bpm: optionalNumber(input.bpm, "BPM", 999.99, false),
    isrc: normalizeIsrc(input.isrc),
    lyricsLanguage: normalizeLanguage(input.lyricsLanguage),
  };
}

function optionalNumber(value: string, label: string, maximum: number, integer: boolean): number | null {
  const text = value.trim();
  if (!text) return null;
  const parsed = Number(text);
  if (!Number.isFinite(parsed) || parsed < 1 || parsed > maximum || (integer && !Number.isInteger(parsed))) {
    const range = integer ? `1–${maximum} 的整数` : `1–${maximum} 的数字`;
    throw new Error(`${label}必须是${range}`);
  }
  return parsed;
}

function normalizeReleaseDate(value: string): string | null {
  const text = value.trim();
  if (!text) return null;
  const match = /^(\d{4})(?:-(\d{2})(?:-(\d{2}))?)?$/.exec(text);
  if (!match) throw new Error("发行日期须使用 YYYY、YYYY-MM 或 YYYY-MM-DD 格式");
  const month = match[2] ? Number(match[2]) : undefined;
  const day = match[3] ? Number(match[3]) : undefined;
  if (month !== undefined && (month < 1 || month > 12)) throw new Error("发行日期中的月份无效");
  if (day !== undefined && month !== undefined) {
    const year = Number(match[1]);
    const maximumDay = [31, leapYear(year) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1] ?? 0;
    if (day < 1 || day > maximumDay) throw new Error("发行日期中的日期无效");
  }
  return text;
}

function normalizeIsrc(value: string): string | null {
  const text = value.trim().toUpperCase().replace(/[\s-]+/g, "");
  if (!text) return null;
  if (!ISRC_PATTERN.test(text)) throw new Error("ISRC 须为 12 位标准编码，例如 USABC1234567");
  return text;
}

function normalizeLanguage(value: string): string {
  const text = value.trim() || "und";
  if (text.length > 35 || !LANGUAGE_PATTERN.test(text)) throw new Error("歌词语言须为有效语言标签，例如 zh-CN 或 und");
  return text;
}

function leapYear(year: number): boolean {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}
