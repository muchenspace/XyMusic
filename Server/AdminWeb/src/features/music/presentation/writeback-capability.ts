import { ref, toValue, watch, type MaybeRefOrGetter, type Ref } from "vue";
import type { WritebackCapability } from "@/features/music/domain/writeback-capability";

export {
  assertWritebackAllowed,
  batchWritebackCapability,
  batchWritebackHint,
  sourceWritebackCapability,
  writebackBlockedMessage,
  type BatchWritebackCapability,
  type WritebackCapability,
} from "@/features/music/domain/writeback-capability";

export function useWritebackSelection(capability: MaybeRefOrGetter<WritebackCapability>): Ref<boolean> {
  const selected = ref(false);
  watch(() => toValue(capability).canWriteBack, (canWriteBack) => {
    if (!canWriteBack) selected.value = false;
  }, { immediate: true });
  return selected;
}
