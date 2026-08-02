import { afterEach, describe, expect, it, vi } from "vitest";
import { BrowserRuntimeEnvironment } from "../src/infrastructure/runtime/BrowserRuntimeEnvironment";
import { BrowserTextClipboard } from "../src/infrastructure/runtime/BrowserTextClipboard";

const tauriInternalsDescriptor = Object.getOwnPropertyDescriptor(window, "__TAURI_INTERNALS__");
const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");

describe("browser runtime adapters", () => {
  afterEach(() => {
    restoreProperty(window, "__TAURI_INTERNALS__", tauriInternalsDescriptor);
    restoreProperty(navigator, "clipboard", clipboardDescriptor);
  });

  it("reports the desktop runtime only when Tauri internals are present", () => {
    const runtime = new BrowserRuntimeEnvironment();

    Reflect.deleteProperty(window, "__TAURI_INTERNALS__");
    expect(runtime.kind()).toBe("browser");

    Object.defineProperty(window, "__TAURI_INTERNALS__", { configurable: true, value: {} });
    expect(runtime.kind()).toBe("tauri");
    expect(runtime.userAgent()).toBe(navigator.userAgent);
  });

  it("writes through the browser clipboard and reports unavailable clipboard access", async () => {
    const writeText = vi.fn(async () => undefined);
    const clipboard = new BrowserTextClipboard();
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });

    await clipboard.writeText("diagnostic output");
    expect(writeText).toHaveBeenCalledExactlyOnceWith("diagnostic output");

    Reflect.deleteProperty(navigator, "clipboard");
    await expect(clipboard.writeText("retry")).rejects.toThrow("Clipboard is unavailable");
  });
});

function restoreProperty(target: object, key: string, descriptor: PropertyDescriptor | undefined): void {
  if (descriptor) Object.defineProperty(target, key, descriptor);
  else Reflect.deleteProperty(target, key);
}
