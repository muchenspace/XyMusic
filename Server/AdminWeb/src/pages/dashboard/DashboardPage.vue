<script setup lang="ts">
import { Activity, Album, Disc3, ListMusic, RefreshCw, Shield, Users } from "lucide-vue-next";
import { useQuery } from "@tanstack/vue-query";
import { computed } from "vue";
import { apiErrorMessage } from "@/shared/application/api-error";
import { dashboardAudioRefetchInterval } from "@/shared/application/audio-status-refresh";
import AppButton from "@/components/AppButton.vue";
import AnimatedNumber from "@/components/AnimatedNumber.vue";
import AudioStatusBadge from "@/components/AudioStatusBadge.vue";
import DistributionChart from "@/components/DistributionChart.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import { useDashboard } from "@/app/services/dashboard";
import type { AudioStatus } from "@/shared/domain/audio-status";
import {
  audioStatuses,
  audioStatusPresentation,
  mediaProcessingStatusPresentation,
  sourceFileStatusPresentation,
} from "@/shared/presentation/audio-status";

const dashboard = useDashboard();
const query = useQuery({
  queryKey: ["admin", "dashboard"],
  queryFn: ({ signal }) => dashboard.execute(signal),
  refetchInterval: (state) => dashboardAudioRefetchInterval(state.state.data),
});
const trackStates = audioStatuses;
const trackDistribution = computed<Partial<Record<AudioStatus, number>>>(() => Object.fromEntries(
  trackStates.map((state) => [state, query.data.value?.catalog.tracks[state] ?? 0]),
));
const trackTotal = computed(() => Object.values(trackDistribution.value).reduce((sum, value) => sum + (value ?? 0), 0));
const metrics = computed(() => {
  const data = query.data.value;
  if (!data) return [];
  return [
    { key: "users", label: "用户总数", value: data.users.total, icon: Users },
    { key: "active", label: "活跃用户", value: data.users.active, icon: Activity },
    { key: "admins", label: "管理员", value: data.users.administrators, icon: Shield },
    { key: "tracks", label: "曲目总数", value: trackTotal.value, icon: ListMusic },
    { key: "albums", label: "专辑", value: data.catalog.albums, icon: Album },
    { key: "artists", label: "艺术家", value: data.catalog.artists, icon: Disc3 },
  ];
});
const trackStateLabels = Object.fromEntries(trackStates.map((state) => [state, audioStatusPresentation(state).label]));
const trackStateColors: Record<AudioStatus, string> = { PROCESSING: "bg-blue-500", READY: "bg-emerald-500", ERROR: "bg-rose-500", ARCHIVED: "bg-slate-500" };
function trackPercentage(state: typeof trackStates[number]): number {
  if (!trackTotal.value) return 0;
  return (trackDistribution.value[state] ?? 0) / trackTotal.value * 100;
}
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="仪表盘" description="用户、资料库、音源和后台任务的实时数据汇总。"><template #eyebrow>运营概览</template><template #actions><AppButton :loading="query.isFetching.value" @click="query.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新数据</AppButton></template></PageHeader>
    <StatePanel v-if="query.isError.value" state="error" :detail="apiErrorMessage(query.error.value, '无法读取仪表盘数据。')" @retry="query.refetch()" />
    <template v-else>
      <section class="ui-card grid grid-cols-2 gap-px overflow-hidden bg-[var(--border)] sm:grid-cols-3 xl:grid-cols-6" :class="{ 'data-refreshing': query.isFetching.value && !query.isPending.value }" :aria-busy="query.isFetching.value"><template v-if="query.isPending.value"><div v-for="index in 6" :key="index" class="bg-[var(--surface-solid)] p-4"><div class="skeleton h-5 w-20" /><div class="skeleton mt-3 h-7 w-24" /></div></template><article v-for="(metric, index) in metrics" v-else :key="metric.key" class="motion-item flex min-w-0 items-center gap-3 bg-[var(--surface-solid)] p-4" :style="{ '--motion-index': index }"><component :is="metric.icon" :size="18" class="shrink-0 text-[var(--muted)]" /><div class="min-w-0"><p class="text-xl font-bold"><AnimatedNumber :value="metric.value" /></p><p class="truncate text-xs font-medium text-[var(--muted)]">{{ metric.label }}</p></div></article></section>
      <section v-if="query.isPending.value" class="grid gap-6 xl:grid-cols-3" aria-hidden="true">
        <article class="ui-card min-h-[420px] p-6 xl:col-span-2"><div class="skeleton h-5 w-36" /><div class="skeleton mx-auto mt-10 h-36 w-36 rounded-full" /><div class="mt-10 space-y-4"><div v-for="index in 4" :key="index" class="flex items-center justify-between gap-5"><div class="skeleton h-5 w-24" /><div class="skeleton h-4 w-20" /></div></div></article>
        <div class="grid gap-6"><article v-for="index in 2" :key="index" class="ui-card min-h-48 p-6"><div class="skeleton h-5 w-32" /><div class="mt-7 space-y-4"><div v-for="row in 3" :key="row" class="flex justify-between gap-4"><div class="skeleton h-5 w-24" /><div class="skeleton h-5 w-10" /></div></div></article></div>
      </section>
      <section v-else class="motion-item grid gap-6 xl:grid-cols-3" style="--motion-index: 2">
        <article class="ui-card overflow-hidden xl:col-span-2">
          <div class="flex items-start justify-between gap-4 px-5 pt-5 sm:px-6 sm:pt-6">
            <div><h2 class="font-bold">曲目处理状态</h2><p class="mt-1 text-xs text-[var(--muted)]">数据库内全部曲目</p></div>
            <div class="text-right"><p class="text-2xl font-black"><AnimatedNumber :value="trackTotal" /></p><p class="text-[10px] font-semibold text-[var(--muted)]">总曲目</p></div>
          </div>
          <DistributionChart :values="trackDistribution" :labels="trackStateLabels" />
          <div class="divide-y divide-[var(--border)] border-t border-[var(--border)] px-5 sm:px-6">
            <div v-for="state in trackStates" :key="state" class="py-3">
              <div class="flex items-center justify-between gap-4">
                <div class="flex items-center gap-2"><span class="h-2 w-2 rounded-full" :class="trackStateColors[state]" /><AudioStatusBadge :status="state" /></div>
                <div class="text-right"><span class="font-bold"><AnimatedNumber :value="trackDistribution[state] ?? 0" /></span><span class="ml-2 text-xs text-[var(--muted)]">{{ trackPercentage(state).toFixed(1) }}%</span></div>
              </div>
              <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div class="progress-fill h-full rounded-full" :class="trackStateColors[state]" :style="{ width: `${trackPercentage(state)}%` }" /></div>
            </div>
          </div>
        </article>
        <div class="grid gap-6">
          <article class="ui-card p-5 sm:p-6"><h2 class="font-bold">音源文件状态</h2><p class="mt-1 text-xs text-[var(--muted)]">所有音源的文件汇总</p><StatePanel v-if="!Object.keys(query.data.value?.sources ?? {}).length" state="empty" compact title="暂无音源文件" /><div v-else class="mt-5 space-y-3"><div v-for="(count, state) in query.data.value?.sources" :key="state" class="flex items-center justify-between"><StatusBadge :status="state" :label="sourceFileStatusPresentation(state).label" :tone="sourceFileStatusPresentation(state).tone" /><span class="font-bold"><AnimatedNumber :value="count" /></span></div></div></article>
          <article class="ui-card p-5 sm:p-6"><h2 class="font-bold">媒体任务状态</h2><p class="mt-1 text-xs text-[var(--muted)]">转码、封面和媒体处理任务</p><StatePanel v-if="!Object.keys(query.data.value?.jobs ?? {}).length" state="empty" compact title="暂无媒体任务" /><div v-else class="mt-5 space-y-3"><div v-for="(count, state) in query.data.value?.jobs" :key="state" class="flex items-center justify-between"><StatusBadge :status="state" :label="mediaProcessingStatusPresentation(state).label" :tone="mediaProcessingStatusPresentation(state).tone" /><span class="font-bold"><AnimatedNumber :value="count" /></span></div></div></article>
        </div>
      </section>
    </template>
  </div>
</template>
