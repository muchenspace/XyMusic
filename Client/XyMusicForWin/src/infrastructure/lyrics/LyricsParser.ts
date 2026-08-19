import type { LyricLine, LyricTiming, LyricWord, Lyrics } from "../../domain/music";
import { resolveLyricWordEnd } from "../../domain/lyricsTimeline";

export interface LyricResource {
  language: string;
  format: "LRC" | "PLAIN" | string;
  timing: LyricTiming;
  content: string;
}

interface ParsedResource {
  language: string;
  lines: LyricLine[];
  synchronized: boolean;
  timing: LyricTiming;
}

const LINE_TIMESTAMP = /\[(\d{1,3}):([0-5]\d)(?:[.:](\d{1,3}))?\]/g;
const CONTRACT_LINE_TIMESTAMP_PREFIX = /^[\t\v\f\r ]*(?:\[\d{1,3}:[0-5]\d(?:[.:]\d{1,3})?\][\t\v\f\r ]*)+/;
const DISPLAY_LINE_TIMESTAMP_PREFIX = /^[\s\u0085\uFEFF]*(?:\[\d{1,3}:[0-5]\d(?:[.:]\d{1,3})?\][\s\u0085\uFEFF]*)+/;
const WORD_TIMESTAMP = /<(\d{1,3}):([0-5]\d)(?:[.:](\d{1,3}))?>/g;
const RESIDUAL_WORD_MARKER = /<[^>]*(?:>|$)/;
const WORD_TIMESTAMP_START = /^[\t\v\f\r ]*<\d{1,3}:[0-5]\d(?:[.:]\d{1,3})?>/;
const METADATA_ONLY_LINE = /^[\t\v\f\r ]*(?:\[[A-Za-z][A-Za-z0-9_-]*:[^\[\]\r\n]*\][\t\v\f\r ]*)+$/;
const LRC_LINE_TAG = /\[[^\]\r\n]*\]/g;
const LRC_OFFSET_TAG = /\[offset:([+-]?\d+)\]/i;
const GO_TRIM_SPACE_START = /^[\u0009-\u000D\u0020\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+/;
const GO_TRIM_SPACE_END = /[\u0009-\u000D\u0020\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+$/;
const SIGNED_LONG_MIN = -9_223_372_036_854_775_808n;
const SIGNED_LONG_MAX = 9_223_372_036_854_775_807n;
const SIGNED_LONG_MAX_SECONDS = Number(SIGNED_LONG_MAX) / 1_000;

export function buildLyrics(trackId: string, resource: LyricResource | null): Lyrics | null {
  if (!resource) return null;
  const content = normalizeContent(resource.content);
  const parsed = parseResource(resource, content);
  if (!goTrimSpace(content)) return null;
  return {
    trackId,
    lines: parsed.lines,
    source: parsed.language || "und",
    synchronized: parsed.synchronized,
    timing: parsed.timing,
  };
}

function parsePlainLyrics(content: string): LyricLine[] {
  return content.split("\n")
    .map(goTrimSpace)
    .filter(Boolean)
    .map((text) => ({ time: null, text }));
}

function parseResource(resource: LyricResource, content: string): ParsedResource {
  const format = resource.format;
  const declaredTiming = parseTiming(resource.timing);
  if (format !== "LRC" && format !== "PLAIN") throw new Error("Lyrics format is invalid");
  if (format === "PLAIN") {
    if (declaredTiming !== "LINE") throw new Error("Plain lyrics must use LINE timing");
    return {
      language: resource.language,
      synchronized: false,
      timing: declaredTiming,
      lines: parsePlainLyrics(content),
    };
  }

  validateLrcAgainstDeclaredTiming(content, declaredTiming);
  return {
    language: resource.language,
    synchronized: true,
    timing: declaredTiming,
    lines: parseTimedLrc(content, declaredTiming),
  };
}

function validateLrcAgainstDeclaredTiming(
  content: string,
  declaredTiming: LyricTiming,
): void {
  const contentTiming: LyricTiming = isCompleteWordTimedLrc(content) ? "WORD" : "LINE";
  if (declaredTiming !== contentTiming) throw new Error("Lyrics timing does not match lyrics content");
}

function isCompleteWordTimedLrc(content: string): boolean {
  let hasTimedLyricLine = false;
  for (const rawLine of content.split("\n")) {
    if (!goTrimSpace(rawLine)) continue;
    const prefix = rawLine.match(CONTRACT_LINE_TIMESTAMP_PREFIX)?.[0];
    if (prefix === undefined) {
      if (METADATA_ONLY_LINE.test(rawLine)) continue;
      return false;
    }
    const rawBody = goTrimSpace(rawLine.slice(prefix.length));
    if (!rawBody) continue;
    hasTimedLyricLine = true;
    const remaining = rawBody.replace(WORD_TIMESTAMP, "");
    if (!WORD_TIMESTAMP_START.test(rawBody)
        || RESIDUAL_WORD_MARKER.test(remaining)
        || !goTrimSpace(remaining)
        || !wordTimestampsAreNondecreasing(rawBody)) {
      return false;
    }
  }
  return hasTimedLyricLine;
}

function parseTiming(value: unknown): LyricTiming {
  if (value !== "LINE" && value !== "WORD") throw new Error("Lyrics timing is invalid");
  return value;
}

function parseTimedLrc(content: string, timing: LyricTiming): LyricLine[] {
  const offsetSeconds = lrcOffsetSeconds(content);
  const lines: LyricLine[] = [];
  for (const rawLine of content.split("\n")) {
    const prefix = rawLine.match(DISPLAY_LINE_TIMESTAMP_PREFIX)?.[0];
    if (prefix === undefined) continue;
    const timestamps = [...prefix.matchAll(LINE_TIMESTAMP)];
    if (!timestamps.length) continue;
    const lineContent = rawLine.slice(prefix.length).replace(LRC_LINE_TAG, "");
    const parsedBody: { text: string; words?: LyricWord[] } = timing === "WORD"
      ? parseWordBody(lineContent)
      : { text: stripEnhancedTimestamps(lineContent) };
    if (!goTrimSpace(parsedBody.text)) continue;
    const baseLineTime = secondsOf(timestamps[0]!);
    for (const timestamp of timestamps) {
      const lineTime = secondsOf(timestamp);
      const lineShift = lineTime - baseLineTime;
      const words = parsedBody.words?.map((word) => ({
        ...word,
        time: applyLrcTiming(word.time, lineShift, offsetSeconds),
        ...(word.endTime === undefined
          ? {}
          : { endTime: applyLrcTiming(word.endTime, lineShift, offsetSeconds) }),
      }));
      lines.push({
        time: applyLrcOffset(lineTime, offsetSeconds),
        text: parsedBody.text,
        ...(words?.length ? { words } : {}),
      });
    }
  }
  lines.sort((left, right) => (left.time ?? 0) - (right.time ?? 0));
  if (timing === "WORD" && (!lines.length || lines.some((line) => !line.words?.length))) {
    throw new Error("Word-timed lyrics must contain word timestamps for every line");
  }
  return inferFinalWordEnds(lines);
}

function parseWordBody(body: string): { text: string; words?: LyricWord[] } {
  const markers = [...body.matchAll(WORD_TIMESTAMP)];
  if (!markers.length) return { text: stripEnhancedTimestamps(body) };
  if (goTrimSpace(body.slice(0, markers[0]!.index ?? 0))) {
    return { text: stripEnhancedTimestamps(body) };
  }

  const words: LyricWord[] = [];
  for (let index = 0; index < markers.length; index += 1) {
    const marker = markers[index]!;
    const start = (marker.index ?? 0) + marker[0].length;
    const end = markers[index + 1]?.index ?? body.length;
    const text = body.slice(start, end);
    if (!text) continue;
    const endMarker = markers[index + 1];
    const endTime = endMarker
      ? secondsOf(endMarker)
      : undefined;
    words.push({
      time: secondsOf(marker),
      ...(endTime === undefined ? {} : { endTime }),
      text,
    });
  }
  const text = words.map((word) => word.text).join("");
  return words.length && goTrimSpace(text)
    ? { text, words }
    : { text: stripEnhancedTimestamps(body) };
}

function inferFinalWordEnds(lines: LyricLine[]): LyricLine[] {
  return lines.map((line, index) => {
    const words = line.words;
    const lastWord = words?.[words.length - 1];
    if (!words?.length || !lastWord || lastWord.endTime !== undefined) return line;
    const endTime = resolveLyricWordEnd(lastWord, undefined, lines[index + 1]?.time);
    return {
      ...line,
      words: [...words.slice(0, -1), { ...lastWord, endTime }],
    };
  });
}

function wordTimestampsAreNondecreasing(body: string): boolean {
  let previous = -1;
  for (const marker of body.matchAll(WORD_TIMESTAMP)) {
    const current = millisecondsOf(marker);
    if (current < previous) return false;
    previous = current;
  }
  return true;
}

function lrcOffsetSeconds(content: string): number {
  for (const line of content.split("\n")) {
    if (!METADATA_ONLY_LINE.test(line)) continue;
    const match = line.match(LRC_OFFSET_TAG);
    if (!match?.[1]) continue;
    try {
      const milliseconds = BigInt(match[1]);
      if (milliseconds < SIGNED_LONG_MIN || milliseconds > SIGNED_LONG_MAX) return 0;
      return Number(milliseconds) / 1_000;
    } catch {
      return 0;
    }
  }
  return 0;
}

function applyLrcOffset(timeSeconds: number, offsetSeconds: number): number {
  if (offsetSeconds > 0) return Math.min(SIGNED_LONG_MAX_SECONDS, timeSeconds + offsetSeconds);
  if (offsetSeconds < 0) return Math.max(0, timeSeconds + offsetSeconds);
  return timeSeconds;
}

function applyLrcTiming(timeSeconds: number, lineShiftSeconds: number, offsetSeconds: number): number {
  return applyLrcOffset(Math.max(0, timeSeconds + lineShiftSeconds), offsetSeconds);
}

function stripEnhancedTimestamps(content: string): string {
  return content.replace(WORD_TIMESTAMP, "").trim();
}

function secondsOf(match: RegExpMatchArray): number {
  const fraction = match[3] ? Number(`0.${match[3].padEnd(3, "0").slice(0, 3)}`) : 0;
  return Number(match[1]) * 60 + Number(match[2]) + fraction;
}

function millisecondsOf(match: RegExpMatchArray): number {
  const fraction = match[3] ? Number(match[3].padEnd(3, "0")) : 0;
  return Number(match[1]) * 60_000 + Number(match[2]) * 1_000 + fraction;
}

function goTrimSpace(value: string): string {
  return value.replace(GO_TRIM_SPACE_START, "").replace(GO_TRIM_SPACE_END, "");
}

function normalizeContent(value: string): string {
  const normalizedLineEndings = value.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  return normalizedLineEndings.startsWith("\uFEFF")
    ? normalizedLineEndings.slice(1)
    : normalizedLineEndings;
}
