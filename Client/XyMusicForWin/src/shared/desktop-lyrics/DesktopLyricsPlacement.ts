export interface PersistedDesktopLyricsPlacement {
  version: 1;
  monitorName: string | null;
  xRatio: number;
  yRatio: number;
  widthLogical: number;
  heightLogical: number;
}

export interface DesktopLyricsPhysicalRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface DesktopLyricsMonitorGeometry {
  name: string | null;
  scaleFactor: number;
  workArea: DesktopLyricsPhysicalRect;
}

export function captureDesktopLyricsPlacement(
  windowRect: DesktopLyricsPhysicalRect,
  monitor: DesktopLyricsMonitorGeometry,
): PersistedDesktopLyricsPlacement {
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

export function restoreDesktopLyricsPlacement(
  stored: PersistedDesktopLyricsPlacement,
  monitor: DesktopLyricsMonitorGeometry,
): DesktopLyricsPhysicalRect {
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

export function selectDesktopLyricsMonitor(
  windowRect: DesktopLyricsPhysicalRect,
  monitors: readonly DesktopLyricsMonitorGeometry[],
): DesktopLyricsMonitorGeometry | undefined {
  return [...monitors].sort(
    (left, right) => intersectionArea(windowRect, right.workArea) - intersectionArea(windowRect, left.workArea),
  )[0];
}

export function normalizeDesktopLyricsPlacement(value: unknown): PersistedDesktopLyricsPlacement | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const parsed = value as Partial<PersistedDesktopLyricsPlacement>;
  if (parsed.version !== 1) return null;
  if (![parsed.xRatio, parsed.yRatio, parsed.widthLogical, parsed.heightLogical].every(
    (entry) => typeof entry === "number" && Number.isFinite(entry),
  )) return null;
  return {
    version: 1,
    monitorName: typeof parsed.monitorName === "string" ? parsed.monitorName : null,
    xRatio: clamp01(parsed.xRatio!),
    yRatio: clamp01(parsed.yRatio!),
    widthLogical: clamp(parsed.widthLogical!, MIN_WIDTH_LOGICAL, MAX_WIDTH_LOGICAL),
    heightLogical: clamp(parsed.heightLogical!, MIN_HEIGHT_LOGICAL, MAX_HEIGHT_LOGICAL),
  };
}

function intersectionArea(left: DesktopLyricsPhysicalRect, right: DesktopLyricsPhysicalRect): number {
  const width = Math.max(0, Math.min(left.x + left.width, right.x + right.width) - Math.max(left.x, right.x));
  const height = Math.max(0, Math.min(left.y + left.height, right.y + right.height) - Math.max(left.y, right.y));
  return width * height;
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

const MIN_WIDTH_LOGICAL = 480;
const MAX_WIDTH_LOGICAL = 1_600;
const MIN_HEIGHT_LOGICAL = 100;
const MAX_HEIGHT_LOGICAL = 420;
