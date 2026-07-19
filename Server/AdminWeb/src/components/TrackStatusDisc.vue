<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref } from "vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { TrackSummary } from "@/features/music/domain/models";
import {
  audioTechnicalStagePresentation,
  mediaProcessingStatusPresentation,
  metadataStatusPresentation,
  sourceFileStatusPresentation,
  trackAudioStatusPresentation,
  variantStatusPresentation,
} from "@/shared/presentation/audio-status";

const props = defineProps<{ track: TrackSummary }>();
const trigger = ref<HTMLElement>();
const open = ref(false);
const position = reactive({ left: 0, top: 0 });
const tooltipId = `track-status-${Math.random().toString(36).slice(2)}`;

const statusValue = computed(() => typeof props.track.audioStatus === "string" ? props.track.audioStatus : "");
const presentation = computed(() => trackAudioStatusPresentation(
  statusValue.value,
  props.track.source?.status,
));
const discState = computed(() => {
  if (presentation.value.tone === "danger") return "error";
  const normalized = statusValue.value.toLowerCase();
  return ["ready", "processing", "archived"].includes(normalized) ? normalized : "error";
});
const technicalStage = computed(() => audioTechnicalStagePresentation(
  statusValue.value,
  props.track.source?.status,
  props.track.mediaProcessing?.status,
));
const sourceStage = computed(() => sourceFileStatusPresentation(props.track.source?.status));
const mediaStage = computed(() => mediaProcessingStatusPresentation(props.track.mediaProcessing?.status));
const metadataStage = computed(() => metadataStatusPresentation(props.track.metadataStatus));
const writebackErrorDetail = computed(() => props.track.metadataStatus === "WRITE_FAILED"
  ? props.track.latestWritebackError?.trim() || null
  : null);

function place(): void {
  const rect = trigger.value?.getBoundingClientRect();
  if (!rect) return;
  const width = 320;
  position.left = Math.max(12, Math.min(window.innerWidth - width - 12, rect.left + rect.width / 2 - width / 2));
  position.top = rect.bottom + 10;
  if (position.top + 360 > window.innerHeight) position.top = Math.max(12, rect.top - 370);
}
let listening = false;
let closeTimer: number | undefined;
function startViewportListeners(): void {
  if (listening) return;
  listening = true;
  window.addEventListener("resize", closeOnViewportChange);
  window.addEventListener("scroll", closeOnViewportChange, true);
}
function stopViewportListeners(): void {
  if (!listening) return;
  listening = false;
  window.removeEventListener("resize", closeOnViewportChange);
  window.removeEventListener("scroll", closeOnViewportChange, true);
}
function cancelScheduledHide(): void {
  if (closeTimer === undefined) return;
  window.clearTimeout(closeTimer);
  closeTimer = undefined;
}
async function show(): Promise<void> {
  cancelScheduledHide();
  if (!open.value) {
    open.value = true;
    startViewportListeners();
  }
  await nextTick();
  place();
}
function hide(): void {
  cancelScheduledHide();
  open.value = false;
  stopViewportListeners();
}
function scheduleHide(): void {
  cancelScheduledHide();
  closeTimer = window.setTimeout(() => {
    closeTimer = undefined;
    hide();
  }, 100);
}
function toggle(): void { if (open.value) hide(); else void show(); }
function closeOnViewportChange(): void { hide(); }
onBeforeUnmount(() => {
  cancelScheduledHide();
  stopViewportListeners();
});

function bitrate(value: number | null): string { return value ? `${Math.round(value / 1_000)} kbps` : "—"; }
function sampleRate(value: number | null): string { return value ? `${(value / 1_000).toFixed(value % 1_000 ? 1 : 0)} kHz` : "—"; }
</script>

<template>
  <span class="inline-flex items-center gap-2" @mouseenter="show" @mouseleave="scheduleHide">
    <button ref="trigger" type="button" class="track-disc" :class="`track-disc--${discState}`" :aria-label="`曲目状态：${presentation.label}`" :aria-describedby="open ? tooltipId : undefined" :aria-expanded="open" @focus="show" @blur="hide" @click.stop="toggle">
      <span class="track-disc__groove" /><span class="track-disc__hub" />
    </button>
    <StatusBadge :status="statusValue" :label="presentation.label" :tone="presentation.tone" />
    <Teleport to="body">
      <Transition name="disc-tooltip">
        <div v-if="open" :id="tooltipId" class="fixed z-[100] w-80 rounded-2xl border border-[var(--border)] bg-[var(--surface-solid)] p-4 text-left shadow-2xl" :style="{ left: `${position.left}px`, top: `${position.top}px` }" role="tooltip" @mouseenter="show" @mouseleave="scheduleHide">
          <div class="flex items-center justify-between gap-3"><div><p class="text-xs font-semibold text-[var(--muted)]">曲目状态</p><p class="mt-1 font-bold">{{ presentation.label }}</p></div><span class="h-3 w-3 rounded-full" :class="discState === 'ready' ? 'bg-emerald-500' : discState === 'processing' ? 'animate-pulse bg-blue-500' : discState === 'error' ? 'bg-rose-500' : 'bg-slate-400'" /></div>
          <dl class="mt-4 grid grid-cols-[88px_1fr] gap-x-3 gap-y-2 text-xs">
            <dt class="text-[var(--muted)]">当前阶段</dt><dd class="font-semibold">{{ technicalStage.label }}</dd>
            <dt class="text-[var(--muted)]">源文件分析</dt><dd>{{ sourceStage.label }}</dd>
            <dt class="text-[var(--muted)]">媒体处理</dt><dd>{{ mediaStage.label }}</dd>
            <dt class="text-[var(--muted)]">Tag</dt><dd>{{ metadataStage.label }}</dd>
            <dt class="text-[var(--muted)]">音源</dt><dd class="truncate" :title="track.source?.relativePath">{{ track.source?.rootName ?? '—' }}</dd>
          </dl>
          <div class="mt-4 border-t border-[var(--border)] pt-3"><div class="flex items-center justify-between"><p class="text-xs font-bold">转码输出</p><span class="text-[10px] text-[var(--muted)]">{{ track.variantSummary.length }} 个</span></div><div v-if="track.variantSummary.length" class="mt-2 space-y-2"><div v-for="variant in track.variantSummary" :key="`${variant.quality}-${variant.codec}`" class="rounded-xl bg-[var(--surface-muted)] px-3 py-2"><div class="flex items-center justify-between gap-2"><span class="font-mono text-xs font-bold">{{ variant.quality }}</span><StatusBadge :status="variant.status" :label="variantStatusPresentation(variant.status).label" :tone="variantStatusPresentation(variant.status).tone" /></div><p class="mt-1 text-[10px] text-[var(--muted)]">{{ variant.codec.toUpperCase() }} / {{ variant.container.toUpperCase() }} · {{ bitrate(variant.bitrate) }} · {{ sampleRate(variant.sampleRate) }}</p></div></div><p v-else class="mt-2 text-xs text-[var(--muted)]">尚未生成转码变体</p></div>
          <p v-if="writebackErrorDetail" class="mt-3 rounded-xl bg-rose-500/10 p-3 text-xs leading-5 text-[var(--danger)]"><span class="font-semibold">Tag 写回：</span>{{ writebackErrorDetail }}</p>
          <p v-if="track.mediaProcessing?.lastError" class="mt-3 rounded-xl bg-rose-500/10 p-3 text-xs leading-5 text-[var(--danger)]">{{ track.mediaProcessing.lastError }}</p>
        </div>
      </Transition>
    </Teleport>
  </span>
</template>

<style scoped>
.track-disc { position: relative; width: 34px; height: 34px; flex: none; border-radius: 9999px; background: repeating-radial-gradient(circle, transparent 0 2px, rgb(255 255 255 / .12) 3px 4px), var(--disc-color); box-shadow: inset 0 0 0 1px rgb(255 255 255 / .18), 0 2px 8px rgb(15 23 42 / .18); transition: transform var(--motion-fast) var(--motion-ease-out), box-shadow var(--motion-fast) ease; }
.track-disc:hover, .track-disc:focus-visible { transform: rotate(6deg) scale(1.04); box-shadow: inset 0 0 0 1px rgb(255 255 255 / .23), 0 3px 11px rgb(15 23 42 / .24); outline: none; }
.track-disc--ready { --disc-color: #10b981; }
.track-disc--processing { --disc-color: #3b82f6; animation: disc-pulse 2s ease-in-out infinite; }
.track-disc--error { --disc-color: #f43f5e; }
.track-disc--archived { --disc-color: #64748b; }
.track-disc__groove { position: absolute; inset: 6px; border: 1px solid rgb(255 255 255 / .28); border-radius: 9999px; }
.track-disc__hub { position: absolute; inset: 12px; border: 2px solid rgb(255 255 255 / .8); border-radius: 9999px; background: var(--surface-solid); }
.disc-tooltip-enter-active,
.disc-tooltip-leave-active { transition: opacity var(--motion-fast) ease, transform var(--motion-fast) var(--motion-ease-out); transform-origin: top center; }
.disc-tooltip-enter-from,
.disc-tooltip-leave-to { opacity: 0; transform: translate3d(0, -4px, 0) scale(.985); }
@keyframes disc-pulse { 50% { filter: brightness(1.12); } }
@media (prefers-reduced-motion: reduce) { .track-disc { animation: none; transition: none; } }
</style>
