import type { PlaybackQuality, Track } from "./music";

export type RepeatMode = "off" | "all" | "one";

export interface PersistedPlaybackState {
  ownerKey: string;
  queue: Track[];
  currentIndex: number;
  position: number;
  shuffled: boolean;
  repeat: boolean;
  repeatMode: RepeatMode;
  quality: PlaybackQuality;
  crossfadeSeconds: number;
  savedAt: string;
}

export interface PlaybackProgressCheckpoint {
  ownerKey: string;
  currentIndex: number;
  trackId: string;
  position: number;
  savedAt: string;
  snapshotSavedAt: string;
}

export function normalizeResumePosition(position: number, duration: number, endGuardSeconds = 2): number {
  const safePosition = Number.isFinite(position) ? Math.max(0, position) : 0;
  const safeDuration = Number.isFinite(duration) ? Math.max(0, duration) : 0;
  if (safeDuration <= 0) return safePosition;

  const clamped = Math.min(safePosition, safeDuration);
  const endGuard = Number.isFinite(endGuardSeconds)
    ? Math.max(0, Math.min(safeDuration, endGuardSeconds))
    : 2;
  return clamped >= safeDuration - endGuard ? 0 : clamped;
}
