<script setup lang="ts">
import { computed } from "vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { AudioStatus } from "@/shared/domain/audio-status";
import { trackAudioStatusPresentation } from "@/shared/presentation/audio-status";

const props = defineProps<{
  status: AudioStatus;
  sourceStatus?: string | null;
  dot?: boolean;
}>();
const statusValue = computed(() => typeof props.status === "string" ? props.status : "");
const presentation = computed(() => trackAudioStatusPresentation(
  statusValue.value,
  props.sourceStatus,
));
</script>

<template>
  <StatusBadge :status="statusValue" :label="presentation.label" :tone="presentation.tone" :dot="dot" />
</template>
