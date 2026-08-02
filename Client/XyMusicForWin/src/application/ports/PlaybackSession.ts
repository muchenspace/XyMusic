import type { PlaybackQuality, ReadonlyTrack } from "../../domain/music";
import type { PlayMode, RepeatMode } from "../../domain/playbackState";

export type PlaybackTerminalEvent = "PAUSED" | "COMPLETED";
export type PlaybackQueue = readonly ReadonlyTrack[];

export interface PlaybackSessionState {
  readonly queue: PlaybackQueue;
  readonly queueVersion: number;
  readonly playbackIntentVersion: number;
  readonly currentIndex: number;
  readonly isPlaying: boolean;
  readonly loading: boolean;
  readonly progress: number;
  readonly currentTime: number;
  readonly duration: number;
  readonly volume: number;
  readonly shuffled: boolean;
  readonly repeatMode: RepeatMode;
  readonly quality: PlaybackQuality;
  readonly crossfadeSeconds: number;
  readonly notificationsEnabled: boolean;
  readonly miniMode: boolean;
  readonly error: string;
}

export interface QueueStart {
  revision: number;
  playback: Promise<boolean>;
}

/**
 * Application-facing playback boundary. Presentation observes state snapshots
 * and invokes commands without coordinating audio or persistence.
 */
export interface PlaybackSession {
  state(): PlaybackSessionState;
  subscribe(listener: (state: PlaybackSessionState) => void): () => void;
  seed(tracks: readonly ReadonlyTrack[]): void;
  restoreState(ownerKey: string): boolean;
  clearPersistedState(): void;
  clearGrants(): void;
  flushPlayerPreferences(): void;
  play(track: ReadonlyTrack, tracks?: readonly ReadonlyTrack[]): Promise<void>;
  playAt(index: number): Promise<void>;
  playFromIndex(tracks: readonly ReadonlyTrack[], index: number, terminalEvent?: PlaybackTerminalEvent | null): Promise<void>;
  startQueue(tracks: readonly ReadonlyTrack[], index: number, terminalEvent?: PlaybackTerminalEvent | null): QueueStart | null;
  appendToQueue(revision: number, tracks: readonly ReadonlyTrack[]): boolean;
  setQueueExtending(revision: number, extending: boolean): void;
  toggle(): Promise<void>;
  next(): Promise<void>;
  previous(): Promise<void>;
  seek(percent: number): void;
  seekTo(seconds: number): void;
  removeFromQueue(trackId: string): void;
  removeFromQueueAt(index: number): void;
  clearQueue(): void;
  stopPlayback(terminalEvent?: PlaybackTerminalEvent | null): void;
  reset(): void;
  setMiniMode(enabled: boolean): Promise<void>;
  setPlayMode(mode: PlayMode): void;
  cyclePlayMode(): void;
  setVolume(value: number): void;
  setQuality(value: PlaybackQuality): void;
  setCrossfadeSeconds(value: number): void;
  setNotificationsEnabled(value: boolean): void;
  dispose(): void;
}
