<script setup lang="ts">
import { CheckCircle2, UploadCloud } from "lucide-vue-next";
import { computed, onBeforeUnmount, ref } from "vue";
import { ApiError } from "@/shared/application/api-error";
import { ARTWORK_ACCEPT, type ArtworkUploadPhase } from "@/features/music/application/artwork-upload-use-case";
import { useArtworkUpload } from "@/app/services/music";
import type { MediaUploadCompletion, MediaUploadPurpose } from "@/shared/domain/media-upload";

const props = withDefaults(defineProps<{
  targetId: string;
  purpose: MediaUploadPurpose;
  imageUrl?: string | null;
  alt: string;
  shape?: "rounded" | "circle";
  noun?: string;
}>(), {
  imageUrl: null,
  shape: "rounded",
  noun: "封面",
});
const emit = defineEmits<{ completed: [completion: MediaUploadCompletion] }>();
const artworkUpload = useArtworkUpload();

const input = ref<HTMLInputElement>();
const uploading = ref(false);
const completed = ref(false);
const progress = ref(0);
const phase = ref<ArtworkUploadPhase>("validating");
const error = ref("");
let controller: AbortController | undefined;

const phaseLabel = computed(() => ({
  validating: "校验图片",
  hashing: "计算 SHA-256",
  reserving: "预约上传",
  uploading: `上传 ${progress.value}%`,
  completing: "服务端校验",
}[phase.value]));
const overallProgress = computed(() => {
  if (phase.value === "validating") return 5;
  if (phase.value === "hashing") return 15;
  if (phase.value === "reserving") return 25;
  if (phase.value === "uploading") return 25 + progress.value * 0.65;
  return 95;
});

function choose(): void {
  if (!uploading.value) input.value?.click();
}

async function selected(event: Event): Promise<void> {
  const element = event.target as HTMLInputElement;
  const file = element.files?.[0];
  element.value = "";
  if (!file) return;
  controller?.abort();
  controller = new AbortController();
  uploading.value = true;
  completed.value = false;
  progress.value = 0;
  error.value = "";
  try {
    const result = await artworkUpload.execute(props.purpose, props.targetId, file, {
      signal: controller.signal,
      onPhase: (value) => { phase.value = value; },
      onProgress: (value) => { progress.value = value; },
    });
    completed.value = true;
    emit("completed", result);
  } catch (cause) {
    if (!(cause instanceof DOMException && cause.name === "AbortError")) {
      error.value = cause instanceof ApiError || cause instanceof Error ? cause.message : `${props.noun}上传失败`;
    }
  } finally {
    uploading.value = false;
    controller = undefined;
  }
}

onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <div class="w-[120px] space-y-2">
    <button
      type="button"
      class="group relative grid h-[120px] w-[120px] place-items-center overflow-hidden bg-[var(--surface-muted)] text-[var(--primary)] ring-offset-2 ring-offset-[var(--surface-solid)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--primary)]"
      :class="shape === 'circle' ? 'rounded-full' : 'rounded-2xl'"
      :disabled="uploading"
      :aria-label="`上传${noun}`"
      @click="choose"
    >
      <img v-if="imageUrl" :src="imageUrl" :alt="alt" class="h-full w-full object-cover" width="120" height="120" decoding="async" />
      <slot v-else />
      <span class="absolute inset-0 grid place-items-center bg-slate-950/60 text-white opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100" :class="{ '!opacity-100': uploading }">
        <UploadCloud v-if="!completed" :size="24" :class="{ 'animate-pulse': uploading }" />
        <CheckCircle2 v-else :size="24" />
      </span>
    </button>
    <input ref="input" class="sr-only" type="file" :accept="ARTWORK_ACCEPT" @change="selected" />
    <div v-if="uploading" class="space-y-1.5" aria-live="polite">
      <div class="h-1 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="progress-fill h-full bg-[var(--primary)]" :style="{ width: `${overallProgress}%` }" /></div>
      <p class="text-center text-[10px] font-semibold text-[var(--muted)]">{{ phaseLabel }}</p>
    </div>
    <button v-else type="button" class="w-full text-center text-xs font-semibold text-[var(--primary)] hover:underline" @click="choose">{{ imageUrl ? `更换${noun}` : `上传${noun}` }}</button>
    <p v-if="error" class="text-center text-[10px] leading-4 text-[var(--danger)]" role="alert">{{ error }}</p>
  </div>
</template>
