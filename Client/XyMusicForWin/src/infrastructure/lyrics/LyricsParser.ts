import type { LyricLine, Lyrics } from "../../domain/music";

export interface LyricResource {
  language: string;
  format: "LRC" | "PLAIN" | string;
  content: string;
  isDefault: boolean;
}

interface ParsedResource {
  language: string;
  lines: LyricLine[];
  synchronized: boolean;
}

const LINE_TIMESTAMP = /\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]/g;
const TRANSLATION_TOLERANCE_SECONDS = 0.35;

export function buildLyrics(trackId: string, resources: LyricResource[]): Lyrics | null {
  const available = resources.filter((resource) => resource.content.trim());
  if (!available.length) return null;
  const primaryResource = available.find((resource) => resource.isDefault) ?? available[0]!;
  const primary = parseResource(primaryResource);
  const translationResource = available.find((resource) => resource !== primaryResource && resource.language !== primaryResource.language);
  const translation = translationResource ? parseResource(translationResource) : null;
  const lines = translation ? mergeTranslation(primary, translation) : primary.lines;
  return {
    trackId,
    lines,
    source: primary.language || "und",
    synchronized: primary.synchronized,
    ...(translation ? { translationSource: translation.language || "und" } : {}),
  };
}

export function parseLrc(content: string): LyricLine[] {
  const offsetSeconds = lrcOffset(content);
  const lines: LyricLine[] = [];
  for (const rawLine of content.replace(/\r/g, "").split("\n")) {
    const timestamps = [...rawLine.matchAll(LINE_TIMESTAMP)];
    if (!timestamps.length) continue;
    const lyricBody = rawLine.replace(LINE_TIMESTAMP, "").trim();
    if (!lyricBody) continue;
    for (const timestamp of timestamps) {
      const time = Math.max(0, secondsOf(timestamp) + offsetSeconds);
      lines.push({ time, text: lyricBody });
    }
  }
  return lines.sort((left, right) => (left.time ?? 0) - (right.time ?? 0));
}

export function parsePlainLyrics(content: string): LyricLine[] {
  return content.replace(/\r/g, "").split("\n")
    .map((text) => text.trim())
    .filter(Boolean)
    .map((text) => ({ time: null, text }));
}

function parseResource(resource: LyricResource): ParsedResource {
  const synchronized = resource.format.toUpperCase() === "LRC";
  return {
    language: resource.language,
    synchronized,
    lines: synchronized ? parseLrc(resource.content) : parsePlainLyrics(resource.content),
  };
}

function mergeTranslation(primary: ParsedResource, translation: ParsedResource): LyricLine[] {
  if (!primary.lines.length || !translation.lines.length) return primary.lines;
  if (!primary.synchronized || !translation.synchronized) {
    return primary.lines.map((line, index) => ({
      ...line,
      ...(translation.lines[index]?.text ? { translation: translation.lines[index].text } : {}),
    }));
  }
  return primary.lines.map((line) => {
    if (line.time === null) return line;
    let nearest: LyricLine | undefined;
    let distance = Number.POSITIVE_INFINITY;
    for (const candidate of translation.lines) {
      if (candidate.time === null) continue;
      const nextDistance = Math.abs(candidate.time - line.time);
      if (nextDistance < distance) { nearest = candidate; distance = nextDistance; }
    }
    return nearest && distance <= TRANSLATION_TOLERANCE_SECONDS
      ? { ...line, translation: nearest.text }
      : line;
  });
}

function lrcOffset(content: string): number {
  const match = content.match(/^\s*\[offset:([+-]?\d+)\]\s*$/im);
  return match ? Number(match[1]) / 1_000 : 0;
}

function secondsOf(match: RegExpMatchArray): number {
  const fraction = match[3] ? Number(`0.${match[3].padEnd(3, "0").slice(0, 3)}`) : 0;
  return Number(match[1]) * 60 + Number(match[2]) + fraction;
}
