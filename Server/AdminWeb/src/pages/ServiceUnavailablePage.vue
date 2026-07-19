<script setup lang="ts">
import { CloudOff, RefreshCw } from "lucide-vue-next";
import { useQuery } from "@tanstack/vue-query";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { cacheSetupState, SETUP_STATUS_STALE_MS } from "@/app/router";
import AppButton from "@/components/AppButton.vue";
import { useSetup } from "@/app/services/setup";
import xymusicIcon from "@/assets/xymusic.png";

const route = useRoute();
const router = useRouter();
const setup = useSetup();
const navigating = ref(false);

const statusQuery = useQuery({
  queryKey: ["setup", "status"],
  queryFn: ({ signal }) => setup.status(signal),
  staleTime: SETUP_STATUS_STALE_MS,
  refetchOnMount: "always",
  refetchOnWindowFocus: true,
  refetchInterval: 5_000,
});

const redirectTarget = computed(() => {
  const redirect = route.query.redirect;
  return typeof redirect === "string" && redirect.startsWith("/") && !redirect.startsWith("//")
    ? redirect
    : "/dashboard";
});

watch(() => statusQuery.dataUpdatedAt.value, async (updatedAt) => {
  const status = statusQuery.data.value;
  if (updatedAt <= 0 || !status || navigating.value) return;
  navigating.value = true;
  cacheSetupState(status);
  try {
    await router.replace(status.setupRequired ? { name: "setup" } : redirectTarget.value);
  } finally {
    navigating.value = false;
  }
}, { immediate: true });
</script>

<template>
  <main class="flex min-h-screen items-center justify-center bg-[var(--bg)] px-5 py-10">
    <section class="page-enter w-full max-w-xl">
      <header class="mb-8 flex items-center gap-3">
        <img :src="xymusicIcon" class="h-9 w-9 shrink-0 object-contain" alt="" width="36" height="36" aria-hidden="true" />
        <div><p class="font-bold">XyMusic</p><p class="text-xs text-[var(--muted)]">管理后台</p></div>
      </header>
      <div class="border-t border-[var(--border)] pt-8">
        <span class="grid h-10 w-10 place-items-center rounded-md bg-rose-500/10 text-[var(--danger)]"><CloudOff :size="21" aria-hidden="true" /></span>
        <h1 class="mt-5 text-2xl font-bold">管理服务暂时不可用</h1>
        <p class="mt-3 max-w-lg text-sm leading-6 text-[var(--muted)]">无法读取后端服务状态。请确认 XyMusic 服务仍在运行，并检查管理端使用的服务地址。</p>
        <div class="mt-6 border-l-2 border-[var(--border-strong)] pl-4 text-sm text-[var(--muted)]" role="status" aria-live="polite">
          <p v-if="statusQuery.isFetching.value" class="flex items-center gap-2"><RefreshCw :size="16" class="animate-spin" aria-hidden="true" />正在重新连接</p>
          <p v-else>页面会每 5 秒自动重试</p>
        </div>
        <AppButton class="mt-7" variant="primary" :loading="statusQuery.isFetching.value || navigating" @click="statusQuery.refetch()">
          <template #icon><RefreshCw :size="16" /></template>
          立即重试
        </AppButton>
      </div>
    </section>
  </main>
</template>
