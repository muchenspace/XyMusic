import type { LyricLine, LyricTiming, LyricWord, Lyrics } from "../../domain/music";

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
const CONTRACT_LINE_TIMESTAMP_PREFIX = /^[\t\n\f\r ]*(?:\[\d{1,3}:[0-5]\d(?:[.:]\d{1,3})?\])+[\t\n\f\r ]*/;
const DISPLAY_LINE_TIMESTAMP_PREFIX = /^[\s\u0085\uFEFF]*(?:\[\d{1,3}:[0-5]\d(?:[.:]\d{1,3})?\][\s\u0085\uFEFF]*)+/;
const WORD_TIMESTAMP = /<(\d{1,3}):([0-5]\d)(?:[.:](\d{1,3}))?>/g;
const RESIDUAL_WORD_MARKER = /<[^>]*(?:>|$)/;
const WORD_TIMESTAMP_START = /^[\t\n\f\r ]*<\d{1,3}:[0-5]\d(?:[.:]\d{1,3})?>/;
const METADATA_ONLY_LINE = /^[\t\n\f\r ]*(?:\[[A-Za-z][A-Za-z0-9_-]*:[^\[\]\r\n]*\][\t\n\f\r ]*)+$/;
const GO_TRIM_SPACE_START = /^[\u0009-\u000D\u0020\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+/;
const GO_TRIM_SPACE_END = /[\u0009-\u000D\u0020\u0085\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000]+$/;

export function buildLyrics(trackId: string, resource: LyricResource | null): Lyrics | null {
  if (!resource) return null;
  const parsed = parseResource(resource);
  if (!goTrimSpace(resource.content)) return null;
  return {
    trackId,
    lines: parsed.lines,
    source: parsed.language || "und",
    synchronized: parsed.synchronized,
    timing: parsed.timing,
  };
}

function parsePlainLyrics(content: string): LyricLine[] {
  return content.replace(/\r/g, "").split("\n")
    .map(goTrimSpace)
    .filter(Boolean)
    .map((text) => ({ time: null, text }));
}

function parseResource(resource: LyricResource): ParsedResource {
  const format = resource.format;
  const declaredTiming = parseTiming(resource.timing);
  if (format !== "LRC" && format !== "PLAIN") throw new Error("Lyrics format is invalid");
  if (format === "PLAIN") {
    if (declaredTiming !== "LINE") throw new Error("Plain lyrics must use LINE timing");
    return {
      language: resource.language,
      synchronized: false,
      timing: declaredTiming,
      lines: parsePlainLyrics(resource.content),
    };
  }

  validateLrcAgainstDeclaredTiming(resource.content, declaredTiming);
  return {
    language: resource.language,
    synchronized: true,
    timing: declaredTiming,
    lines: parseTimedLrc(resource.content, declaredTiming),
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
  for (const rawLine of content.replace(/\r/g, "").split("\n")) {
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
  const offsetSeconds = lrcOffset(content);
  const lines: LyricLine[] = [];
  for (const rawLine of content.replace(/\r/g, "").split("\n")) {
    const prefix = rawLine.match(DISPLAY_LINE_TIMESTAMP_PREFIX)?.[0];
    if (prefix === undefined) continue;
    const timestamps = [...prefix.matchAll(LINE_TIMESTAMP)];
    const rawBody = goTrimSpace(rawLine.slice(prefix.length));
    if (!rawBody) continue;
    for (const timestamp of timestamps) {
      const time = Math.max(0, secondsOf(timestamp) + offsetSeconds);
      const parsedBody = timing === "WORD"
        ? parseWordBody(rawBody, offsetSeconds)
        : { text: rawBody.replace(WORD_TIMESTAMP, "") };
      if (!parsedBody.text) continue;
      lines.push({
        time,
        text: parsedBody.text,
        ...(parsedBody.words?.length ? { words: parsedBody.words } : {}),
      });
    }
  }
  return lines.sort((left, right) => (left.time ?? 0) - (right.time ?? 0));
}

function parseWordBody(body: string, offsetSeconds: number): { text: string; words?: LyricWord[] } {
  const markers = [...body.matchAll(WORD_TIMESTAMP)];
  if (!markers.length) return { text: body };

  const words: LyricWord[] = [];
  for (let index = 0; index < markers.length; index += 1) {
    const marker = markers[index]!;
    const start = (marker.index ?? 0) + marker[0].length;
		const end = markers[index + 1]?.index ?? body.length;
		const text = body.slice(start, end);
		if (!text) continue;
		const endMarker = markers[index + 1];
		const endTime = endMarker
			? Math.max(0, secondsOf(endMarker) + offsetSeconds)
			: undefined;
		words.push({
			time: Math.max(0, secondsOf(marker) + offsetSeconds),
			...(endTime === undefined ? {} : { endTime }),
			text,
		});
	}
  return { text: words.map((word) => word.text).join(""), words };
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

function lrcOffset(content: string): number {
  const match = content.match(/^\s*\[offset:([+-]?\d+)\]\s*$/im);
  return match ? Number(match[1]) / 1_000 : 0;
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
