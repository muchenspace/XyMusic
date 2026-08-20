import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const contentStyles = readFileSync(path.resolve(import.meta.dirname, "../src/styles/content.css"), "utf8");
const desktopLyricsStyles = readFileSync(path.resolve(import.meta.dirname, "../src/styles/desktop-lyrics.css"), "utf8");
const overlayStyles = readFileSync(path.resolve(import.meta.dirname, "../src/styles/overlays.css"), "utf8");

describe("rendering style guards", () => {
  it("keeps intrinsic-size containment off variable-height grid cards", () => {
    for (const selector of [".album-card", ".artist-card", ".playlist-library-card"]) {
      const declarations = declarationsFor(contentStyles, selector);
      expect(declarations).not.toMatch(/\bcontent-visibility\s*:/u);
      expect(declarations).not.toMatch(/\bcontain-intrinsic-size\s*:/u);
    }
  });

  it("retains content visibility for fixed-height discovery rows", () => {
    for (const selector of [".mood-card", ".random-track-card"]) {
      const declarations = declarationsFor(contentStyles, selector);
      expect(declarations).toMatch(/\bcontent-visibility\s*:\s*auto/u);
      expect(declarations).toMatch(/\bcontain-intrinsic-size\s*:/u);
    }
  });

  it("keeps intrinsic-size containment off variable-height lyric lines", () => {
    const declarations = declarationsForRelatedSelectors(overlayStyles, ".lyric-line");
    expect(declarations).not.toMatch(/\bcontent-visibility\s*:/u);
    expect(declarations).not.toMatch(/\bcontain-intrinsic-size\s*:/u);
  });

  it("wraps unbroken playback lyrics in both the source and highlight layers", () => {
    for (const selector of [
      ".lyric-line strong",
      ".lyric-line-words",
      ".word-by-word-lyric-overlay-copy",
    ]) {
      expect(declarationsFor(overlayStyles, selector)).toMatch(/\boverflow-wrap\s*:\s*anywhere/u);
    }
  });

  it("wraps unbroken desktop lyrics in both the source and highlight layers", () => {
    for (const selector of [
      ".desktop-lyric-line p",
      ".desktop-lyric-words",
      ".word-by-word-lyric-overlay-copy",
    ]) {
      expect(declarationsFor(desktopLyricsStyles, selector)).toMatch(/\boverflow-wrap\s*:\s*anywhere/u);
    }
  });

  it("keeps the outgoing desktop lyric from resizing the current grid row", () => {
    const outgoing = declarationsFor(desktopLyricsStyles, ".desktop-lyric-line-outgoing");
    expect(outgoing).toMatch(/\bposition\s*:\s*absolute/u);
    expect(outgoing).toMatch(/\btop\s*:\s*var\(--desktop-lyrics-copy-padding-start\)/u);
    expect(declarationsFor(desktopLyricsStyles, ".desktop-lyric-line-current"))
      .toMatch(/\bgrid-area\s*:\s*current/u);
  });
});

function declarationsFor(styles: string, selector: string): string {
  const declarations: string[] = [];
  for (const match of styles.matchAll(/([^{}]+)\{([^{}]*)\}/gu)) {
    if (match[1]!.split(",").map((candidate) => candidate.trim()).includes(selector)) {
      declarations.push(match[2]!);
    }
  }
  return declarations.join("\n");
}

function declarationsForRelatedSelectors(styles: string, selector: string): string {
  const declarations: string[] = [];
  for (const match of styles.matchAll(/([^{}]+)\{([^{}]*)\}/gu)) {
    if (match[1]!.includes(selector)) declarations.push(match[2]!);
  }
  return declarations.join("\n");
}
