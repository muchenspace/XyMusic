import type { RepeatMode } from "../domain/playbackState";

/**
 * 播放模式：由 RepeatMode 与 shuffled 组合派生的统一展示概念。
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
