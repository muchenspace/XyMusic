import type { PlaybackQuality, Track } from "./music";

export type RepeatMode = "off" | "all" | "one";

/**
 * 播放模式：UI 层的统一概念，由 RepeatMode 与 shuffled 组合派生。
 * 用于将"随机"与"循环"两个独立维度合并为单一互斥按钮。
 * 三态：列表循环（顺序播放完整队列）/ 单曲循环 / 随机播放。
 */
export type PlayMode = "repeat-all" | "repeat-one" | "shuffle";

export const PLAY_MODE_ORDER: readonly PlayMode[] = ["repeat-all", "repeat-one", "shuffle"];

/** 根据 RepeatMode 与 shuffled 派生当前 PlayMode。 */
export function derivePlayMode(repeatMode: RepeatMode, shuffled: boolean): PlayMode {
  if (shuffled) return "shuffle";
  if (repeatMode === "one") return "repeat-one";
  return "repeat-all";
}

/** 将 PlayMode 拆解为 RepeatMode 与 shuffled 两个底层状态。 */
export function splitPlayMode(mode: PlayMode): { repeatMode: RepeatMode; shuffled: boolean } {
  switch (mode) {
    case "repeat-one": return { repeatMode: "one", shuffled: false };
    case "shuffle": return { repeatMode: "off", shuffled: true };
    default: return { repeatMode: "all", shuffled: false };
  }
}

/** 在 PLAY_MODE_ORDER 中循环到下一个模式。 */
export function cyclePlayMode(current: PlayMode): PlayMode {
  const index = PLAY_MODE_ORDER.indexOf(current);
  return PLAY_MODE_ORDER[(index + 1) % PLAY_MODE_ORDER.length]!;
}

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
