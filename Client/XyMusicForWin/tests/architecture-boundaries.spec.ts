import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const sourceRoot = path.resolve(import.meta.dirname, "../src");
const layerRoots = ["domain", "application", "infrastructure", "presentation"] as const;
const forbiddenImports: Record<(typeof layerRoots)[number], ReadonlySet<string>> = {
  domain: new Set(["application", "infrastructure", "presentation"]),
  application: new Set(["infrastructure", "presentation"]),
  infrastructure: new Set(["presentation"]),
  presentation: new Set(["infrastructure"]),
};

describe("source architecture boundaries", () => {
  it("keeps dependencies pointing inward", () => {
    const violations: string[] = [];
    for (const sourceLayer of layerRoots) {
      for (const file of sourceFiles(path.join(sourceRoot, sourceLayer))) {
        for (const specifier of relativeImports(readFileSync(file, "utf8"))) {
          const target = path.resolve(path.dirname(file), specifier);
          const targetLayer = path.relative(sourceRoot, target).split(path.sep)[0] ?? "";
          if (forbiddenImports[sourceLayer].has(targetLayer)) {
            violations.push(`${relative(file)} imports ${targetLayer} through ${specifier}`);
          }
        }
      }
    }

    expect(violations).toEqual([]);
  });

  it("keeps browser persistence and network IO in infrastructure", () => {
    const violations: string[] = [];
    for (const layer of ["domain", "application", "presentation"] as const) {
      for (const file of sourceFiles(path.join(sourceRoot, layer))) {
        const source = readFileSync(file, "utf8");
        if (/\b(?:localStorage|sessionStorage)\b|\bfetch\s*\(/u.test(source)) violations.push(relative(file));
      }
    }

    expect(violations).toEqual([]);
  });
});

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const candidate = path.join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(candidate);
    return entry.isFile() && (entry.name.endsWith(".ts") || entry.name.endsWith(".vue")) ? [candidate] : [];
  });
}

function relativeImports(source: string): string[] {
  const imports: string[] = [];
  const pattern = /(?:from\s*|import\s*)["'](\.{1,2}\/[^"']+)["']/gu;
  for (const match of source.matchAll(pattern)) imports.push(match[1]!);
  return imports;
}

function relative(file: string): string {
  return path.relative(sourceRoot, file).replaceAll(path.sep, "/");
}
