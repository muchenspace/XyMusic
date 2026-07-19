import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const projectRoot = path.resolve(import.meta.dirname, "..");

describe("Tauri window integration", () => {
  it("uses the native application-exit command for the custom close button", () => {
    const source = readFileSync(
      path.join(projectRoot, "src/infrastructure/windows/TauriDesktopWindow.ts"),
      "utf8",
    );

    expect(source).toContain('invoke("exit_application")');
    expect(source).not.toContain("getCurrentWindow().close()");
  });
});
