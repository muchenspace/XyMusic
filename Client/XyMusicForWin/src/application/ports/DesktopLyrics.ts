import type { Lyrics } from "../../domain/music";
import type { DesktopLyricsFullscreenBehavior } from "./UserInterfacePreferences";

// Version 4 adds a per-main-window transport epoch. A revision only orders
// messages within that epoch, so a restarted main window cannot be confused
// with delayed messages from the previous renderer process.
export const DESKTOP_LYRICS_PROTOCOL_VERSION = 4 as const;

export interface DesktopLyricsTrack {
  id: string;
  title: string;
  artist: string;
}

export interface DesktopLyricsSnapshot {
  version: typeof DESKTOP_LYRICS_PROTOCOL_VERSION;
  /** Stable for one main-window renderer lifetime; changes after a restart. */
  transportEpoch: string;
  revision?: number;
  track: DesktopLyricsTrack | null;
  lyrics: Lyrics | null;
  isPlaying: boolean;
  /** False when the lyric window is hidden and should not keep a render loop alive. */
  renderActive?: boolean;
  positionSeconds: number;
  anchoredAtMs: number;
  offsetSeconds: number;
  showTranslation: boolean;
  locked: boolean;
  fontScale: number;
  textColor?: string;
  highlightColor?: string;
}

export interface DesktopLyricsClock {
  version: typeof DESKTOP_LYRICS_PROTOCOL_VERSION;
  /** Matches the snapshot transport epoch that established this clock's state. */
  transportEpoch: string;
  /** Shared transport order with snapshots; used to reject delayed IPC samples. */
  revision?: number;
  trackId: string | null;
  isPlaying: boolean;
  positionSeconds: number;
  anchoredAtMs: number;
}

interface DesktopLyricsActionBase {
  version: typeof DESKTOP_LYRICS_PROTOCOL_VERSION;
  issuedAtMs: number;
}

export type DesktopLyricsAction =
  | (DesktopLyricsActionBase & { action: "ready" })
  | (DesktopLyricsActionBase & { action: "previous" })
  | (DesktopLyricsActionBase & { action: "toggle-playback" })
  | (DesktopLyricsActionBase & { action: "next" })
  | (DesktopLyricsActionBase & { action: "set-font-scale"; value: number })
  | (DesktopLyricsActionBase & { action: "set-text-color"; value: string })
  | (DesktopLyricsActionBase & { action: "set-highlight-color"; value: string })
  | (DesktopLyricsActionBase & { action: "lock"; locked: true })
  | (DesktopLyricsActionBase & { action: "close" });

export interface DesktopLyricsWindowState {
  /** Monotonic native state order for rejecting delayed window events. */
  revision: number;
  requestedVisible: boolean;
  visible: boolean;
  locked: boolean;
  hiddenForFullscreen: boolean;
  fullscreenBehavior: DesktopLyricsFullscreenBehavior;
}

export interface DesktopLyrics {
  getWindowState(): Promise<DesktopLyricsWindowState>;
  setVisible(visible: boolean): Promise<DesktopLyricsWindowState>;
  toggleVisible(): Promise<DesktopLyricsWindowState>;
  setLocked(locked: boolean): Promise<DesktopLyricsWindowState>;
  setFullscreenBehavior(value: DesktopLyricsFullscreenBehavior): Promise<DesktopLyricsWindowState>;
  sendSnapshot(snapshot: DesktopLyricsSnapshot): Promise<void>;
  sendClock(clock: DesktopLyricsClock): Promise<void>;
  onAction(listener: (action: DesktopLyricsAction) => void): Promise<() => void>;
  onWindowState(listener: (state: DesktopLyricsWindowState) => void): Promise<() => void>;
}
