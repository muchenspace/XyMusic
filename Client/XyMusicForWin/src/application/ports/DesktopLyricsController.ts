import type { Lyrics } from "../../domain/music";
import type { DesktopLyricsTrack } from "./DesktopLyrics";
import type { DesktopLyricsFullscreenBehavior } from "./UserInterfacePreferences";

export type DesktopLyricsPlaybackRequest = "ready" | "previous" | "toggle-playback" | "next";

export interface DesktopLyricsControllerState {
  readonly visible: boolean;
  readonly actuallyVisible: boolean;
  readonly locked: boolean;
  readonly hiddenForFullscreen: boolean;
  readonly fullscreenBehavior: DesktopLyricsFullscreenBehavior;
  readonly fontScale: number;
  readonly textColor: string;
  readonly highlightColor: string;
}

/**
 * Presentation-supplied projection of the current playback/lyrics state. The
 * controller owns the transport envelope (epoch/revision) and send policy, so
 * callers only describe what should be rendered.
 */
export interface DesktopLyricsSnapshotInput {
  track: DesktopLyricsTrack | null;
  lyrics: Lyrics | null;
  isPlaying: boolean;
  renderActive: boolean;
  positionSeconds: number;
  anchoredAtMs: number;
  positionDiscontinuityVersion?: number;
  offsetSeconds: number;
  showTranslation: boolean;
  locked: boolean;
  fontScale: number;
  textColor: string;
  highlightColor: string;
}

/** Presentation-supplied clock sample without the transport envelope. */
export interface DesktopLyricsClockInput {
  trackId: string | null;
  isPlaying: boolean;
  positionSeconds: number;
  anchoredAtMs: number;
  positionDiscontinuityVersion?: number;
}

/**
 * Application-facing control boundary for desktop-lyrics window preferences
 * and native window state. Presentation observes this state and sends intent.
 * Snapshot/clock delivery is coalesced inside the controller so presentation
 * only has to watch its own state and forward it.
 */
export interface DesktopLyricsController {
  state(): DesktopLyricsControllerState;
  subscribe(listener: (state: DesktopLyricsControllerState) => void): () => void;
  initialize(): Promise<void>;
  setVisible(value: boolean): Promise<void>;
  toggleVisible(): Promise<void>;
  setLocked(value: boolean): Promise<void>;
  setFullscreenBehavior(value: DesktopLyricsFullscreenBehavior): Promise<void>;
  setFontScale(value: number): void;
  setTextColor(value: string): void;
  setHighlightColor(value: string): void;
  subscribePlaybackRequests(listener: (request: DesktopLyricsPlaybackRequest) => void): () => void;
  /** Enqueues a snapshot, coalescing overlapping requests into the latest one. */
  requestSnapshot(create: () => DesktopLyricsSnapshotInput, force?: boolean): void;
  /** Debounces style-only snapshot updates. */
  scheduleSnapshot(create: () => DesktopLyricsSnapshotInput): void;
  /** Applies the periodic clock send policy and then enqueues the clock. */
  offerClock(create: () => DesktopLyricsClockInput | null): void;
  /** Enqueues a clock immediately, coalescing with an in-flight send. */
  sendClock(create: () => DesktopLyricsClockInput | null): void;
  /** Drops a queued clock without sending it. */
  discardPendingClock(): void;
  /** Cancels debounced work and stops draining queued sends. */
  cancelPendingSends(): void;
  dispose(): void;
}
