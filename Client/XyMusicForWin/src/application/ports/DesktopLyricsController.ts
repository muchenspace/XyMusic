import type { DesktopLyricsClock, DesktopLyricsSnapshot } from "./DesktopLyrics";
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
 * Application-facing control boundary for desktop-lyrics window preferences
 * and native window state. Presentation observes this state and sends intent.
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
  sendSnapshot(snapshot: DesktopLyricsSnapshot): Promise<void>;
  sendClock(clock: DesktopLyricsClock): Promise<void>;
  dispose(): void;
}
