import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TauriDesktopLyricsWindowPlacement } from "../src/infrastructure/windows/TauriDesktopLyricsWindowPlacement";

const windowApi = vi.hoisted(() => ({
  availableMonitors: vi.fn(),
  getCurrentWindow: vi.fn(),
}));

vi.mock("@tauri-apps/api/window", () => ({
  availableMonitors: windowApi.availableMonitors,
  getCurrentWindow: windowApi.getCurrentWindow,
}));

describe("desktop lyrics window placement", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    windowApi.availableMonitors.mockResolvedValue([]);
    (window as Window & { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__ = {};
  });

  afterEach(() => {
    delete (window as Window & { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__;
  });

  it("releases every registered listener when a companion registration fails", async () => {
    const removeMoved = vi.fn();
    const appWindow = {
      onMoved: vi.fn(async () => removeMoved),
      onResized: vi.fn(async () => { throw new Error("resize listener unavailable"); }),
    };
    windowApi.getCurrentWindow.mockReturnValue(appWindow);
    const placement = new TauriDesktopLyricsWindowPlacement(memoryStorage());

    const stop = await placement.observe();

    expect(appWindow.onMoved).toHaveBeenCalledOnce();
    expect(appWindow.onResized).toHaveBeenCalledOnce();
    expect(removeMoved).toHaveBeenCalledOnce();
    stop();
    expect(removeMoved).toHaveBeenCalledOnce();
  });

  it("cleans both native listeners exactly once after observation stops", async () => {
    const removeMoved = vi.fn();
    const removeResized = vi.fn();
    const appWindow = {
      onMoved: vi.fn(async () => removeMoved),
      onResized: vi.fn(async () => removeResized),
    };
    windowApi.getCurrentWindow.mockReturnValue(appWindow);
    const placement = new TauriDesktopLyricsWindowPlacement(memoryStorage());

    const stop = await placement.observe();
    stop();
    stop();

    expect(removeMoved).toHaveBeenCalledOnce();
    expect(removeResized).toHaveBeenCalledOnce();
  });
});

function memoryStorage(): Pick<Storage, "getItem" | "setItem"> {
  return { getItem: () => null, setItem: () => undefined };
}
