import { ref, toValue, watch, type MaybeRefOrGetter, type Ref } from "vue";
import type { TrackSummary } from "@/features/music/domain/models";

export interface WritebackCapability {
  canWriteBack: boolean;
  blockReason: string | null;
}

export interface BatchWritebackCapability extends WritebackCapability {
  total: number;
  blockedCount: number;
  blockedReasons: Array<{ reason: string; count: number }>;
}

interface SourceCapability {
  canWriteBack: boolean;
  writebackBlockReason: string | null;
}

const NO_LOCAL_SOURCE_REASON = "无可写本地源";
const UNKNOWN_BLOCK_REASON = "当前源文件不可写";

export function sourceWritebackCapability(source: SourceCapability | null | undefined): WritebackCapability {
  if (source?.canWriteBack === true) return { canWriteBack: true, blockReason: null };
  const reason = source?.writebackBlockReason?.trim();
  return { canWriteBack: false, blockReason: reason || (source ? UNKNOWN_BLOCK_REASON : NO_LOCAL_SOURCE_REASON) };
}

export function batchWritebackCapability(tracks: readonly TrackSummary[]): BatchWritebackCapability {
  const reasonCounts = new Map<string, number>();
  let blockedCount = 0;
  for (const track of tracks) {
    const capability = sourceWritebackCapability(track.source);
    if (capability.canWriteBack) continue;
    blockedCount += 1;
    const reason = capability.blockReason ?? UNKNOWN_BLOCK_REASON;
    reasonCounts.set(reason, (reasonCounts.get(reason) ?? 0) + 1);
  }
  return {
    canWriteBack: tracks.length > 0 && blockedCount === 0,
    blockReason: blockedCount > 0 ? `${blockedCount} 首曲目不可写回` : tracks.length ? null : "未选择曲目",
    total: tracks.length,
    blockedCount,
    blockedReasons: [...reasonCounts].map(([reason, count]) => ({ reason, count })),
  };
}

export function batchWritebackHint(capability: BatchWritebackCapability): string {
  if (capability.canWriteBack) return `全部 ${capability.total} 首曲目均可创建安全写回任务。`;
  if (!capability.total) return "未选择曲目，不能写回源文件 Tag。";
  const reasons = capability.blockedReasons.map(({ reason, count }) => `${reason}（${count} 首）`).join("；");
  return `已选择 ${capability.total} 首，其中 ${capability.blockedCount} 首不能写回：${reasons}`;
}

export function writebackBlockedMessage(capability: WritebackCapability): string {
  return capability.blockReason ? `当前不能写回源文件 Tag：${capability.blockReason}` : "当前不能写回源文件 Tag";
}

export function assertWritebackAllowed(requested: boolean, capability: WritebackCapability): void {
  if (requested && !capability.canWriteBack) throw new Error(writebackBlockedMessage(capability));
}

export function useWritebackSelection(capability: MaybeRefOrGetter<WritebackCapability>): Ref<boolean> {
  const selected = ref(false);
  watch(() => toValue(capability).canWriteBack, (canWriteBack) => {
    if (!canWriteBack) selected.value = false;
  }, { immediate: true });
  return selected;
}
