import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const sourceRoot = resolve(process.cwd(), "src");

describe("admin layout fill behavior", () => {
  it("lets the main content use the full width while preserving responsive padding", () => {
    const layout = readFileSync(resolve(sourceRoot, "layouts/AdminLayout.vue"), "utf8");

    expect(layout).toContain('class="w-full px-4 py-5 sm:px-6 lg:px-8"');
    expect(layout).not.toContain('max-w-[1800px]');
    expect(layout).not.toContain('class="mx-auto max-w-[1800px]');
  });

  it("allows settings navigation and dashboard columns to stretch", () => {
    const settings = readFileSync(resolve(sourceRoot, "pages/settings/SettingsPage.vue"), "utf8");
    const dashboard = readFileSync(resolve(sourceRoot, "pages/dashboard/DashboardPage.vue"), "utf8");

    expect(settings).toContain('<nav class="ui-card p-2">');
    expect(settings).not.toContain('<nav class="ui-card h-max p-2">');
    expect(dashboard).toContain('<div class="grid gap-6">');
    expect(dashboard).not.toContain('<div class="grid content-start gap-6">');
  });
});
