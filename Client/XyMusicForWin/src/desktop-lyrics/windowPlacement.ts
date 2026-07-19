interface PersistedDesktopLyricsPlacement {
  version: 1;
  monitorName: string | null;
  xRatio: number;
  yRatio: number;
  widthLogical: number;
  heightLogical: number;
}

interface PhysicalRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface MonitorGeometry {
  name: string | null;
  scaleFactor: number;
  workArea: PhysicalRect;
}

export async function restoreDesktopLyricsPlacement(): Promise<void> {
  if (!isTauriRuntime()) return;
  const stored = readPlacement();
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
    const placement = restorePlacement(stored, monitor);
    const appWindow = getCurrentWindow();
    await appWindow.setSize(new PhysicalSize(placement.width, placement.height));
    await appWindow.setPosition(new PhysicalPosition(placement.x, placement.y));
  } catch {
    // The configured center position remains a safe fallback.
  }
}

export async function observeDesktopLyricsPlacement(): Promise<() => void> {
  if (!isTauriRuntime()) return () => undefined;
  try {
    const { availableMonitors, getCurrentWindow } = await import("@tauri-apps/api/window");
    const appWindow = getCurrentWindow();
    let timer = 0;
    let disposed = false;
    const persist = async () => {
      timer = 0;
      if (disposed) return;
      try {
        const [position, size, available] = await Promise.all([
          appWindow.outerPosition(),
          appWindow.outerSize(),
          availableMonitors(),
        ]);
        const monitors = available.map(toMonitorGeometry);
        const windowRect = { x: position.x, y: position.y, width: size.width, height: size.height };
        const monitor = bestMonitorForWindow(windowRect, monitors);
        if (!monitor) return;
        writePlacement(capturePlacement(windowRect, monitor));
      } catch {
        // Position persistence is optional and should not affect window interaction.
      }
    };
    const schedule = () => {
      if (timer) window.clearTimeout(timer);
      timer = window.setTimeout(() => { void persist(); }, SAVE_DEBOUNCE_MS);
    };
    const [removeMoved, removeResized] = await Promise.all([
      appWindow.onMoved(schedule),
      appWindow.onResized(schedule),
    ]);
    const flush = () => { void persist(); };
    window.addEventListener("pagehide", flush);
    return () => {
      disposed = true;
      if (timer) window.clearTimeout(timer);
      removeMoved();
      removeResized();
      window.removeEventListener("pagehide", flush);
    };
  } catch {
    return () => undefined;
  }
}

export function capturePlacement(windowRect: PhysicalRect, monitor: MonitorGeometry): PersistedDesktopLyricsPlacement {
  const scale = validScaleFactor(monitor.scaleFactor);
  const availableWidth = Math.max(0, monitor.workArea.width - windowRect.width);
  const availableHeight = Math.max(0, monitor.workArea.height - windowRect.height);
  return {
    version: 1,
    monitorName: monitor.name,
    xRatio: availableWidth > 0 ? clamp01((windowRect.x - monitor.workArea.x) / availableWidth) : 0.5,
    yRatio: availableHeight > 0 ? clamp01((windowRect.y - monitor.workArea.y) / availableHeight) : 0.5,
    widthLogical: clamp(windowRect.width / scale, MIN_WIDTH_LOGICAL, MAX_WIDTH_LOGICAL),
    heightLogical: clamp(windowRect.height / scale, MIN_HEIGHT_LOGICAL, MAX_HEIGHT_LOGICAL),
  };
}

export function restorePlacement(stored: PersistedDesktopLyricsPlacement, monitor: MonitorGeometry): PhysicalRect {
  const scale = validScaleFactor(monitor.scaleFactor);
  const minimumWidth = Math.min(MIN_WIDTH_LOGICAL * scale, monitor.workArea.width);
  const minimumHeight = Math.min(MIN_HEIGHT_LOGICAL * scale, monitor.workArea.height);
  const width = Math.round(clamp(stored.widthLogical * scale, minimumWidth, monitor.workArea.width));
  const height = Math.round(clamp(stored.heightLogical * scale, minimumHeight, monitor.workArea.height));
  const availableWidth = Math.max(0, monitor.workArea.width - width);
  const availableHeight = Math.max(0, monitor.workArea.height - height);
  return {
    x: Math.round(monitor.workArea.x + clamp01(stored.xRatio) * availableWidth),
    y: Math.round(monitor.workArea.y + clamp01(stored.yRatio) * availableHeight),
    width,
    height,
  };
}

function bestMonitorForWindow(windowRect: PhysicalRect, monitors: readonly MonitorGeometry[]): MonitorGeometry | undefined {
  return [...monitors].sort((left, right) => intersectionArea(windowRect, right.workArea) - intersectionArea(windowRect, left.workArea))[0];
}

function intersectionArea(left: PhysicalRect, right: PhysicalRect): number {
  const width = Math.max(0, Math.min(left.x + left.width, right.x + right.width) - Math.max(left.x, right.x));
  const height = Math.max(0, Math.min(left.y + left.height, right.y + right.height) - Math.max(left.y, right.y));
  return width * height;
}

function toMonitorGeometry(monitor: {
  name: string | null;
  scaleFactor: number;
  workArea: { position: { x: number; y: number }; size: { width: number; height: number } };
}): MonitorGeometry {
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

function readPlacement(): PersistedDesktopLyricsPlacement | null {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? "null") as Partial<PersistedDesktopLyricsPlacement> | null;
    if (!parsed || parsed.version !== 1) return null;
    if (![parsed.xRatio, parsed.yRatio, parsed.widthLogical, parsed.heightLogical].every((value) => typeof value === "number" && Number.isFinite(value))) return null;
    return {
      version: 1,
      monitorName: typeof parsed.monitorName === "string" ? parsed.monitorName : null,
      xRatio: clamp01(parsed.xRatio!),
      yRatio: clamp01(parsed.yRatio!),
      widthLogical: clamp(parsed.widthLogical!, MIN_WIDTH_LOGICAL, MAX_WIDTH_LOGICAL),
      heightLogical: clamp(parsed.heightLogical!, MIN_HEIGHT_LOGICAL, MAX_HEIGHT_LOGICAL),
    };
  } catch {
    return null;
  }
}

function writePlacement(value: PersistedDesktopLyricsPlacement): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(value));
  } catch {
    // Storage failures only disable placement restoration.
  }
}

function validScaleFactor(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 1;
}

function clamp01(value: number): number {
  return clamp(value, 0, 1);
}

function clamp(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return minimum;
  return Math.max(minimum, Math.min(maximum, value));
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

const STORAGE_KEY = "xymusic.desktop-lyrics.window-placement.v1";
const SAVE_DEBOUNCE_MS = 400;
const MIN_WIDTH_LOGICAL = 480;
const MAX_WIDTH_LOGICAL = 1_600;
const MIN_HEIGHT_LOGICAL = 100;
const MAX_HEIGHT_LOGICAL = 420;
