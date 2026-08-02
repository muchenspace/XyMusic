import type { DesktopLyricsWindowPlacement } from "../../application/ports/DesktopLyricsWindowPlacement";
import {
  captureDesktopLyricsPlacement,
  normalizeDesktopLyricsPlacement,
  restoreDesktopLyricsPlacement,
  selectDesktopLyricsMonitor,
  type DesktopLyricsMonitorGeometry,
  type PersistedDesktopLyricsPlacement,
} from "../../application/services/DesktopLyricsPlacement";

type PlacementStorage = Pick<Storage, "getItem" | "setItem">;

/** Persists desktop-lyrics placement behind the application placement port. */
export class TauriDesktopLyricsWindowPlacement implements DesktopLyricsWindowPlacement {
  constructor(private readonly storage: PlacementStorage | undefined = browserStorage()) {}

  async restore(): Promise<void> {
    if (!isTauriRuntime()) return;
    const stored = this.readPlacement();
    if (!stored) return;
    try {
      const [{ PhysicalPosition, PhysicalSize }, { availableMonitors, getCurrentWindow, primaryMonitor }] = await Promise.all([
        import("@tauri-apps/api/dpi"),
        import("@tauri-apps/api/window"),
      ]);
      const [available, primary] = await Promise.all([availableMonitors(), primaryMonitor()]);
      const monitors = available.map(toMonitorGeometry);
      const primaryGeometry = primary ? toMonitorGeometry(primary) : undefined;
      const monitor = (stored.monitorName ? monitors.find((candidate) => candidate.name === stored.monitorName) : undefined)
        ?? primaryGeometry
        ?? monitors[0];
      if (!monitor) return;
      const placement = restoreDesktopLyricsPlacement(stored, monitor);
      const appWindow = getCurrentWindow();
      await appWindow.setSize(new PhysicalSize(placement.width, placement.height));
      await appWindow.setPosition(new PhysicalPosition(placement.x, placement.y));
    } catch {
      // A configured center position remains a safe fallback.
    }
  }

  async observe(): Promise<() => void> {
    if (!isTauriRuntime()) return noop;
    const unlisteners: Array<() => void> = [];
    let timer: number | undefined;
    let disposed = false;
    let flush: (() => void) | undefined;
    const stopObserving = () => {
      if (disposed) return;
      disposed = true;
      if (timer !== undefined) window.clearTimeout(timer);
      timer = undefined;
      for (const unlisten of unlisteners.splice(0)) unlistenSafely(unlisten);
      if (flush) window.removeEventListener("pagehide", flush);
      flush = undefined;
    };

    try {
      const { availableMonitors, getCurrentWindow } = await import("@tauri-apps/api/window");
      const appWindow = getCurrentWindow();
      const persist = async () => {
        timer = undefined;
        if (disposed) return;
        try {
          const [position, size, available] = await Promise.all([
            appWindow.outerPosition(),
            appWindow.outerSize(),
            availableMonitors(),
          ]);
          const monitors = available.map(toMonitorGeometry);
          const windowRect = { x: position.x, y: position.y, width: size.width, height: size.height };
          const monitor = selectDesktopLyricsMonitor(windowRect, monitors);
          if (!monitor) return;
          if (disposed) return;
          this.writePlacement(captureDesktopLyricsPlacement(windowRect, monitor));
        } catch {
          // Position persistence is optional and must not affect window interaction.
        }
      };
      const schedule = () => {
        if (disposed) return;
        if (timer !== undefined) window.clearTimeout(timer);
        timer = window.setTimeout(() => { void persist(); }, SAVE_DEBOUNCE_MS);
      };
      const registrations = await Promise.allSettled([
        appWindow.onMoved(schedule),
        appWindow.onResized(schedule),
      ]);
      for (const registration of registrations) {
        if (registration.status === "fulfilled") unlisteners.push(registration.value);
      }
      if (registrations.some((registration) => registration.status === "rejected")) {
        stopObserving();
        return noop;
      }
      flush = () => { void persist(); };
      window.addEventListener("pagehide", flush);
      return stopObserving;
    } catch {
      stopObserving();
      return noop;
    }
  }

  private readPlacement(): PersistedDesktopLyricsPlacement | null {
    try {
      return normalizeDesktopLyricsPlacement(JSON.parse(this.storage?.getItem(STORAGE_KEY) ?? "null"));
    } catch {
      return null;
    }
  }

  private writePlacement(value: PersistedDesktopLyricsPlacement): void {
    try {
      this.storage?.setItem(STORAGE_KEY, JSON.stringify(value));
    } catch {
      // Placement restoration is optional when browser storage is unavailable.
    }
  }
}

function toMonitorGeometry(monitor: {
  name: string | null;
  scaleFactor: number;
  workArea: { position: { x: number; y: number }; size: { width: number; height: number } };
}): DesktopLyricsMonitorGeometry {
  return {
    name: monitor.name,
    scaleFactor: monitor.scaleFactor,
    workArea: {
      x: monitor.workArea.position.x,
      y: monitor.workArea.position.y,
      width: monitor.workArea.size.width,
      height: monitor.workArea.size.height,
    },
  };
}

function browserStorage(): PlacementStorage | undefined {
  try {
    return typeof localStorage === "undefined" ? undefined : localStorage;
  } catch {
    return undefined;
  }
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

function unlistenSafely(unlisten: () => void): void {
  try {
    unlisten();
  } catch {
    // A failed cleanup must not leave other native listeners active.
  }
}

const noop = () => undefined;
const STORAGE_KEY = "xymusic.desktop-lyrics.window-placement.v1";
const SAVE_DEBOUNCE_MS = 400;
