<script setup lang="ts">
import { ArrowLeft, FileQuestion } from "lucide-vue-next";
</script>

<template>
  <main class="grid min-h-screen place-items-center px-5 text-center">
    <div class="page-enter max-w-md">
      <div class="mx-auto grid h-11 w-11 place-items-center rounded-md bg-[var(--surface-muted)] text-[var(--muted)]"><FileQuestion :size="22" aria-hidden="true" /></div>
      <p class="mt-6 text-sm font-semibold text-[var(--muted)]">错误 404</p>
      <h1 class="mt-2 text-2xl font-bold">页面不存在</h1>
      <p class="mt-3 text-sm text-[var(--muted)]">请求的管理页面已移动或不存在。</p>
      <RouterLink class="btn btn-primary mt-7" to="/dashboard"><ArrowLeft :size="16" />返回仪表盘</RouterLink>
    </div>
  </main>
</template>
