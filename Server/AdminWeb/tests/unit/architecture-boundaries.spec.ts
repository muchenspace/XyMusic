import { readdirSync, readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { ApiError as ClientApiError } from "@/api/client";
import { ApiError } from "@/shared/application/api-error";

const sourceRoot = resolve(process.cwd(), "src");
const sourceFiles = collectSourceFiles(sourceRoot);
const sourceModules = sourceFiles.map((file) => {
  const path = toPosix(relative(sourceRoot, file));
  const source = readFileSync(file, "utf8");
  return {
    id: stripExtension(path),
    path,
    source,
    imports: extractSpecifiers(source).map((specifier) => ({
      specifier,
      resolved: resolveSpecifier(path, specifier),
    })),
  };
});

describe("source architecture boundaries", () => {
  it("keeps the client ApiError export compatible with the shared application contract", () => {
    expect(ClientApiError).toBe(ApiError);
  });

  it("does not import error presentation contracts from the concrete API client", () => {
    const violations = sourceFiles.flatMap((file) => {
      const source = readFileSync(file, "utf8");
      return [...source.matchAll(/import\s*\{([^}]*)\}\s*from\s*["']@\/api\/client["']/gu)]
        .filter((match) => /\b(ApiError|ApiConnectionError|apiErrorMessage)\b/u.test(match[1] ?? ""))
        .map(() => relative(sourceRoot, file));
    });

    expect(violations).toEqual([]);
  });

  it("keeps feature presentation modules independent from infrastructure and composition roots", () => {
    const presentationFiles = sourceFiles.filter((file) => relative(sourceRoot, file).replace(/\\/gu, "/").match(/^features\/[^/]+\/presentation\//u));
    const violations = presentationFiles
      .filter((file) => /from\s*["'](?:@\/app\/services\/|[^"']*\/infrastructure(?:\/|["']))/u.test(readFileSync(file, "utf8")))
      .map((file) => relative(sourceRoot, file));

    expect(violations).toEqual([]);
  });

  it("keeps shared presentation independent from features and composition roots", () => {
    const presentationFiles = sourceFiles.filter((file) => relative(sourceRoot, file).replace(/\\/gu, "/").startsWith("shared/presentation/"));
    const violations = presentationFiles
      .filter((file) => /from\s*["'](?:@\/(?:api|app|features)\/|(?:\.\.\/)+(?:api|app|features)\/|[^"']*\/infrastructure(?:\/|["']))/u.test(readFileSync(file, "utf8")))
      .map((file) => relative(sourceRoot, file));

    expect(violations).toEqual([]);
  });

  it("keeps pages, components and layouts independent from the low-level api modules", () => {
    const violations = sourceModules
      .filter((module) => inLayer(module.path, "pages") || inLayer(module.path, "components") || inLayer(module.path, "layouts"))
      .flatMap((module) => module.imports
        .filter((item) => item.resolved !== null && /^api(?:\/|$)/u.test(item.resolved))
        .map((item) => `${module.path} -> ${item.specifier}`));

    expect(violations).toEqual([]);
  });

  it("keeps stores independent from the low-level api modules and the query client instance", () => {
    const forbidden = new Set(["api/client", "api/admin", "app/query-client"]);
    const violations = sourceModules
      .filter((module) => inLayer(module.path, "stores"))
      .flatMap((module) => module.imports
        .filter((item) => item.resolved !== null && forbidden.has(item.resolved))
        .map((item) => `${module.path} -> ${item.specifier}`));

    expect(violations).toEqual([]);
  });

  it("keeps feature domain modules independent from api, app, stores, utils and other layers of any feature", () => {
    const violations = sourceModules
      .filter((module) => featureLayerOf(module.path)?.layer === "domain")
      .flatMap((module) => {
        const owner = featureLayerOf(module.path)?.feature;
        return module.imports
          .filter((item) => {
            if (item.resolved === null) return false;
            if (/^(?:api|app|stores|utils|pages|components|layouts)(?:\/|$)/u.test(item.resolved)) return true;
            const target = featureLayerOf(item.resolved);
            return target !== null && (target.feature !== owner || target.layer !== "domain");
          })
          .map((item) => `${module.path} -> ${item.specifier}`);
      });

    expect(violations).toEqual([]);
  });

  it("keeps feature application modules independent from api, app, stores, utils, query libraries and foreign or outer feature layers", () => {
    const violations = sourceModules
      .filter((module) => featureLayerOf(module.path)?.layer === "application")
      .flatMap((module) => {
        const owner = featureLayerOf(module.path)?.feature;
        return module.imports
          .filter((item) => {
            if (item.resolved === null) return item.specifier.startsWith("@tanstack/");
            if (/^(?:api|app|stores|utils|pages|components|layouts)(?:\/|$)/u.test(item.resolved)) return true;
            const target = featureLayerOf(item.resolved);
            return target !== null && (target.layer === "infrastructure"
              || target.layer === "presentation"
              || (target.layer === "application" && target.feature !== owner));
          })
          .map((item) => `${module.path} -> ${item.specifier}`);
      });

    expect(violations).toEqual([]);
  });

  it("keeps feature presentation modules independent from api, stores, utils, composition roots and feature infrastructure", () => {
    const violations = sourceModules
      .filter((module) => featureLayerOf(module.path)?.layer === "presentation")
      .flatMap((module) => module.imports
        .filter((item) => {
          if (item.resolved === null) return false;
          if (/^(?:api|stores|utils)(?:\/|$)/u.test(item.resolved)) return true;
          if (/^app\/services(?:\/|$)/u.test(item.resolved)) return true;
          return featureLayerOf(item.resolved)?.layer === "infrastructure";
        })
        .map((item) => `${module.path} -> ${item.specifier}`));

    expect(violations).toEqual([]);
  });

  it("keeps shared modules independent from features and composition roots", () => {
    const violations = sourceModules
      .filter((module) => inLayer(module.path, "shared"))
      .flatMap((module) => module.imports
        .filter((item) => item.resolved !== null && /^(?:features|app|api|stores|utils)(?:\/|$)/u.test(item.resolved))
        .map((item) => `${module.path} -> ${item.specifier}`));

    expect(violations).toEqual([]);
  });

  it("keeps the source import graph free of circular dependencies", () => {
    const moduleIds = new Set(sourceModules.map((module) => module.id));
    const graph = new Map<string, string[]>();
    for (const module of sourceModules) {
      const dependencies = module.imports
        .map((item) => item.resolved)
        .filter((resolved): resolved is string => resolved !== null && moduleIds.has(resolved));
      graph.set(module.id, [...new Set(dependencies)]);
    }

    expect(findImportCycles(graph)).toEqual([]);
  });

  it("keeps pages and components from mutating the query cache directly", () => {
    const cacheMutationCall = /\.(?:setQueryData|setQueriesData|invalidateQueries|removeQueries|resetQueries|cancelQueries|clear)\s*\(/u;
    const violations = sourceModules
      .filter((module) => inLayer(module.path, "pages") || inLayer(module.path, "components"))
      .flatMap((module) => {
        const importsRawClient = /import\s*\{[^}]*\bqueryClient\b[^}]*\}\s*from\s*["']@\/app\/query-client["']/u.test(module.source);
        const rawClientMutation = importsRawClient && /\bqueryClient\s*\./u.test(module.source) && cacheMutationCall.test(module.source);
        const hookClient = /const\s+([A-Za-z_$][\w$]*)\s*=\s*useQueryClient\s*\(\s*\)/u.exec(module.source)?.[1];
        const hookMutation = hookClient !== undefined
          && new RegExp(`\\b${hookClient}\\s*\\.`, "u").test(module.source)
          && cacheMutationCall.test(module.source);
        const inlineHookMutation = /useQueryClient\s*\(\s*\)\s*\./u.test(module.source) && cacheMutationCall.test(module.source);
        return rawClientMutation || hookMutation || inlineHookMutation ? [module.path] : [];
      });

    expect(violations).toEqual([]);
  });

  it("keeps route rendering single-mounted and preloads navigation targets", () => {
    const layout = readFileSync(resolve(sourceRoot, "layouts/AdminLayout.vue"), "utf8");
    const styles = readFileSync(resolve(sourceRoot, "styles/main.css"), "utf8");

    expect(layout).not.toContain('Transition name="route"');
    expect(layout).not.toContain(".route-leave-active");
    expect(layout).toContain(':data-route-path="viewRoute.path"');
    expect(layout).toContain("loadRouteLocation");
    expect(layout).toContain('@pointerenter="preload(item.to)"');
    expect(styles).toContain(".page-enter");
    expect(styles).toContain("@keyframes xy-page-in");
    expect(styles).toContain(".content-swap-enter-active");
    expect(styles).toContain("animation-delay: 0ms !important");
    expect(styles).not.toContain("@keyframes enter");
  });
});

function collectSourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) return collectSourceFiles(path);
    return /\.(ts|vue)$/u.test(entry.name) ? [path] : [];
  });
}

function toPosix(path: string): string {
  return path.replace(/\\/gu, "/");
}

function stripExtension(path: string): string {
  return path.replace(/\.(?:ts|vue)$/u, "");
}

/** Extracts static import/export specifiers and dynamic import() targets. */
function extractSpecifiers(source: string): string[] {
  const specifiers: string[] = [];
  for (const match of source.matchAll(/\b(?:import|export)\s+(?:type\s+)?(?:[^"']*?\sfrom\s+)?["']([^"']+)["']/gu)) {
    if (match[1]) specifiers.push(match[1]);
  }
  for (const match of source.matchAll(/\bimport\s*\(\s*["']([^"']+)["']\s*\)/gu)) {
    if (match[1]) specifiers.push(match[1]);
  }
  return [...new Set(specifiers)];
}

/** Resolves an `@/` alias or relative specifier to a src-relative module id, or null for packages. */
function resolveSpecifier(fromPath: string, specifier: string): string | null {
  let candidate: string;
  if (specifier.startsWith("@/")) {
    candidate = specifier.slice(2);
  } else if (specifier.startsWith("./") || specifier.startsWith("../")) {
    candidate = toPosix(relative(sourceRoot, resolve(sourceRoot, dirname(fromPath), specifier)));
  } else {
    return null;
  }
  if (candidate.startsWith("..")) return null;
  return stripExtension(candidate);
}

function inLayer(path: string, layer: string): boolean {
  return path === layer || path.startsWith(`${layer}/`);
}

function featureLayerOf(path: string): { feature: string; layer: string } | null {
  const match = /^features\/([^/]+)\/(domain|application|infrastructure|presentation)\//u.exec(path);
  return match?.[1] && match[2] ? { feature: match[1], layer: match[2] } : null;
}

/** Returns every strongly connected component larger than one node (Tarjan), plus self-imports. */
function findImportCycles(graph: Map<string, string[]>): string[][] {
  const indices = new Map<string, number>();
  const lowLinks = new Map<string, number>();
  const onStack = new Set<string>();
  const stack: string[] = [];
  const cycles: string[][] = [];
  let nextIndex = 0;

  function connect(node: string): void {
    indices.set(node, nextIndex);
    lowLinks.set(node, nextIndex);
    nextIndex += 1;
    stack.push(node);
    onStack.add(node);

    for (const dependency of graph.get(node) ?? []) {
      if (!indices.has(dependency)) {
        connect(dependency);
        lowLinks.set(node, Math.min(lowLinks.get(node) ?? 0, lowLinks.get(dependency) ?? 0));
      } else if (onStack.has(dependency)) {
        lowLinks.set(node, Math.min(lowLinks.get(node) ?? 0, indices.get(dependency) ?? 0));
      }
    }

    if (lowLinks.get(node) === indices.get(node)) {
      const component: string[] = [];
      let current = stack.pop();
      while (current !== undefined) {
        onStack.delete(current);
        component.push(current);
        if (current === node) break;
        current = stack.pop();
      }
      if (component.length > 1 || (graph.get(node) ?? []).includes(node)) cycles.push(component.sort());
    }
  }

  for (const node of [...graph.keys()].sort()) {
    if (!indices.has(node)) connect(node);
  }
  return cycles;
}
