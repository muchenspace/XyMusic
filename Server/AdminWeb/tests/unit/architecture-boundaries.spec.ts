import { readdirSync, readFileSync } from "node:fs";
import { relative, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { ApiError as ClientApiError } from "@/api/client";
import { ApiError } from "@/shared/application/api-error";

const sourceRoot = resolve(process.cwd(), "src");
const sourceFiles = collectSourceFiles(sourceRoot);

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

  it("routes every audit-code UI consumer through shared presentation", () => {
    const consumers = [
      resolve(sourceRoot, "pages/audit/AuditPage.vue"),
      resolve(sourceRoot, "pages/dashboard/DashboardPage.vue"),
    ];

    for (const consumer of consumers) {
      const source = readFileSync(consumer, "utf8");
      expect(source).toContain('from "@/shared/presentation/audit"');
      expect(source).toContain("auditActionLabel(");
      expect(source).toContain("auditTargetTypeLabel(");
    }
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
