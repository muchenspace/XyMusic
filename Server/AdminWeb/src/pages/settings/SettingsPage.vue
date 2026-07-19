<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Database, HardDrive, Info, Library, LockKeyhole, RefreshCw, Save, ServerCog, ShieldCheck, Wrench } from "lucide-vue-next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { computed, defineComponent, h, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import { ApiError } from "@/shared/application/api-error";
import AppButton from "@/components/AppButton.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import type { RuntimeSettings, RuntimeSettingsUpdate } from "@/features/settings/domain/models";
import { useSettingsAdmin } from "@/app/services/settings";
import { useUiStore } from "@/stores/ui";
import { formatBytes, formatDuration } from "@/utils/format";

type Tab = "database" | "storage" | "media" | "library" | "access" | "system";
const queryClient = useQueryClient();
const ui = useUiStore();
const settingsAdmin = useSettingsAdmin();
const tab = ref<Tab>("database");
const actionError = ref("");
const testMessage = ref("");
const baseline = ref("");
const databasePassword = ref("");
const storageSecret = ref("");
const proxies = ref("");
const includePatterns = ref("");
const excludePatterns = ref("");
const autoDetectMedia = ref(false);
const executableRelativePathHint = "支持相对或绝对路径；相对路径以服务端二进制文件所在目录为基准。";
const form = reactive({
  version: 0,
  database: { host: "", port: 0, database: "", username: "", sslMode: "prefer" as "disable" | "prefer" | "require" | "verify-full", maximumConnections: 0 },
  storage: { endpoint: "", publicBaseUrl: "", region: "", bucket: "", accessKeyId: "", forcePathStyle: false, signedUrlTtlSeconds: 0, maxUploadBytes: 0 },
  mediaTools: { directory: "", ffmpegPath: "", ffprobePath: "" },
  scraping: { fpcalcPath: "", acoustIdClient: "" },
  localLibrary: { name: "", directory: "", mode: "READ_ONLY" as "READ_ONLY" | "READ_WRITE", enabled: false, syncOnStartup: false, scanIntervalMinutes: null as number | null },
  registration: { enabled: false },
  security: { accessTokenTtlSeconds: 0, refreshTokenTtlSeconds: 0 },
  http: { ipv4Host: "", ipv4Port: 0, ipv6Host: "", ipv6Port: 0 },
});
const settingsQuery = useQuery({ queryKey: ["admin", "settings"], queryFn: ({ signal }) => settingsAdmin.settings(signal) });
const systemQuery = useQuery({ queryKey: ["admin", "system"], queryFn: ({ signal }) => settingsAdmin.systemInformation(signal), enabled: computed(() => tab.value === "system"), refetchInterval: computed(() => tab.value === "system" ? 5_000 : false) });

function editableState(): object { return { database: form.database, storage: form.storage, autoDetectMedia: autoDetectMedia.value, mediaTools: form.mediaTools, scraping: form.scraping, localLibrary: form.localLibrary, registration: form.registration, security: form.security, http: form.http, proxies: proxies.value, includePatterns: includePatterns.value, excludePatterns: excludePatterns.value, databasePassword: databasePassword.value, storageSecret: storageSecret.value }; }
function applySettings(settings: RuntimeSettings): void {
  form.version = settings.version;
  Object.assign(form.database, { host: settings.database.host ?? "", port: settings.database.port ?? 5432, database: settings.database.database ?? "", username: settings.database.username ?? "", sslMode: settings.database.sslMode ?? "prefer", maximumConnections: settings.database.maximumConnections ?? 10 });
  Object.assign(form.storage, { endpoint: settings.storage.endpoint ?? "", publicBaseUrl: settings.storage.publicBaseUrl ?? "", region: settings.storage.region, bucket: settings.storage.bucket, accessKeyId: settings.storage.accessKeyId, forcePathStyle: settings.storage.forcePathStyle, signedUrlTtlSeconds: settings.storage.signedUrlTtlSeconds, maxUploadBytes: settings.storage.maxUploadBytes });
  autoDetectMedia.value = Boolean(settings.mediaTools.directory);
  Object.assign(form.mediaTools, { directory: settings.mediaTools.directory ?? "", ffmpegPath: settings.mediaTools.ffmpegPath, ffprobePath: settings.mediaTools.ffprobePath });
  Object.assign(form.scraping, { fpcalcPath: settings.scraping.fpcalcPath, acoustIdClient: settings.scraping.acoustIdClient });
  Object.assign(form.localLibrary, { name: settings.localLibrary.name, directory: settings.localLibrary.directory, mode: settings.localLibrary.mode, enabled: settings.localLibrary.enabled, syncOnStartup: settings.localLibrary.syncOnStartup, scanIntervalMinutes: settings.localLibrary.scanIntervalMinutes });
  Object.assign(form.registration, { enabled: settings.registration.enabled });
  Object.assign(form.security, { accessTokenTtlSeconds: settings.security.accessTokenTtlSeconds, refreshTokenTtlSeconds: settings.security.refreshTokenTtlSeconds });
  Object.assign(form.http, {
    ipv4Host: settings.http.ipv4Host,
    ipv4Port: settings.http.ipv4Port,
    ipv6Host: settings.http.ipv6Host,
    ipv6Port: settings.http.ipv6Port,
  });
  proxies.value = settings.http.trustedProxyAddresses.join("\n");
  includePatterns.value = settings.localLibrary.includePatterns.join("\n");
  excludePatterns.value = settings.localLibrary.excludePatterns.join("\n");
  databasePassword.value = "";
  storageSecret.value = "";
  baseline.value = JSON.stringify(editableState());
}
watch(() => settingsQuery.data.value, (settings) => {
  if (!settings) return;
  if (baseline.value && JSON.stringify(editableState()) !== baseline.value) {
    ui.notify("warning", "检测到远端设置变化", "当前未保存内容已保留；重新读取前请先保存或放弃修改。");
    return;
  }
  applySettings(settings);
}, { immediate: true });
const dirty = computed(() => Boolean(settingsQuery.data.value) && JSON.stringify(editableState()) !== baseline.value);
function beforeUnload(event: BeforeUnloadEvent): void {
  if (!dirty.value) return;
  event.preventDefault();
  event.returnValue = "";
}
onMounted(() => window.addEventListener("beforeunload", beforeUnload));
onBeforeUnmount(() => window.removeEventListener("beforeunload", beforeUnload));
onBeforeRouteLeave(() => !dirty.value || window.confirm("系统设置尚未保存，确定离开吗？"));
const tabs = [
  { key: "database", label: "数据库", icon: Database }, { key: "storage", label: "对象存储", icon: HardDrive }, { key: "media", label: "媒体工具", icon: Wrench },
  { key: "library", label: "本地资料库", icon: Library }, { key: "access", label: "注册与安全", icon: ShieldCheck }, { key: "system", label: "系统信息", icon: Info },
] as const;
function locked(section: "database" | "storage" | "mediaTools" | "scraping" | "localLibrary" | "registration" | "security" | "http", field: string): boolean { return settingsQuery.data.value?.[section].lockedFields.includes(field) ?? false; }
function resetMessages(): void { actionError.value = ""; testMessage.value = ""; }
function detailedApiError(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) return fallback;
  const detail = error.problem.detail?.trim();
  const message = detail || error.message;
  return error.problem.traceId ? `${message}（追踪 ID：${error.problem.traceId}）` : message;
}
async function reloadSettings(): Promise<void> {
  if (dirty.value && !window.confirm("重新读取会放弃当前未保存设置，确定继续吗？")) return;
  baseline.value = "";
  databasePassword.value = "";
  storageSecret.value = "";
  const result = await settingsQuery.refetch();
  if (result.data) applySettings(result.data);
}
function lines(value: string): string[] { return [...new Set(value.split(/[,，\r\n]+/).map((item) => item.trim()).filter(Boolean))]; }

const testDatabase = useMutation({ mutationFn: () => settingsAdmin.testDatabase({ ...form.database, password: databasePassword.value || undefined }), onSuccess: (result) => { testMessage.value = `${result.message}${result.latencyMs ? ` · ${result.latencyMs} ms` : ""}`; }, onError: (error) => { actionError.value = detailedApiError(error, "数据库测试失败"); } });
const testStorage = useMutation({ mutationFn: () => settingsAdmin.testStorage({ ...form.storage, endpoint: form.storage.endpoint || null, publicBaseUrl: form.storage.publicBaseUrl || null, secretAccessKey: storageSecret.value || undefined }), onSuccess: (result) => { testMessage.value = `${result.message}${result.latencyMs ? ` · ${result.latencyMs} ms` : ""}`; }, onError: (error) => { actionError.value = detailedApiError(error, "对象存储测试失败"); } });
function mediaToolsPayload() {
  return autoDetectMedia.value
    ? { directory: form.mediaTools.directory.trim() }
    : { ffmpegPath: form.mediaTools.ffmpegPath.trim(), ffprobePath: form.mediaTools.ffprobePath.trim() };
}
const testMedia = useMutation({ mutationFn: () => settingsAdmin.testMediaTools(mediaToolsPayload()), onSuccess: (result) => { testMessage.value = [result.message, ...(result.details ?? [])].join(" · "); }, onError: (error) => { actionError.value = detailedApiError(error, "FFmpeg 测试失败"); } });
const testLibrary = useMutation({ mutationFn: () => settingsAdmin.testLocalLibrary(form.localLibrary.directory), onSuccess: (result) => { testMessage.value = `${result.message}${result.normalizedPath ? ` · ${result.normalizedPath}` : ""}`; }, onError: (error) => { actionError.value = detailedApiError(error, "资料库目录测试失败"); } });
const testing = computed(() => testDatabase.isPending.value || testStorage.isPending.value || testMedia.isPending.value || testLibrary.isPending.value);
function testCurrent(): void { resetMessages(); if (tab.value === "database") testDatabase.mutate(); else if (tab.value === "storage") testStorage.mutate(); else if (tab.value === "media") testMedia.mutate(); else if (tab.value === "library") testLibrary.mutate(); }

function payload(): RuntimeSettingsUpdate {
  return {
    expectedVersion: form.version,
    database: { ...form.database, password: databasePassword.value || undefined },
    storage: { ...form.storage, endpoint: form.storage.endpoint || null, publicBaseUrl: form.storage.publicBaseUrl || null, secretAccessKey: storageSecret.value || undefined },
    mediaTools: mediaToolsPayload(),
    scraping: {
      fpcalcPath: form.scraping.fpcalcPath.trim(),
      acoustIdClient: form.scraping.acoustIdClient.trim(),
    },
    localLibrary: { ...form.localLibrary, includePatterns: lines(includePatterns.value), excludePatterns: lines(excludePatterns.value) },
    registration: { enabled: form.registration.enabled },
    security: { ...form.security },
    http: { ...form.http, trustedProxyAddresses: lines(proxies.value) },
  };
}
const saveMutation = useMutation({ mutationFn: () => settingsAdmin.update(payload()), onSuccess: async (settings) => { applySettings(settings); queryClient.setQueryData(["admin", "settings"], settings); if (settings.restartRequiredFields.length) ui.notify("warning", "设置已保存，监听地址需重启生效", `当前仍监听 IPv4 ${settings.actualListener.ipv4.host}:${settings.actualListener.ipv4.port}，IPv6 [${settings.actualListener.ipv6.host}]:${settings.actualListener.ipv6.port}`); else ui.notify("success", "系统设置已应用", "Server 已切换配置，Worker 将自动安全重载"); await Promise.all([queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] === "admin" && query.queryKey[1] !== "settings" }), queryClient.invalidateQueries({ queryKey: ["service", "readiness"] })]); }, onError: (error) => { actionError.value = detailedApiError(error, "设置保存失败，服务继续使用原配置"); } });

function save(): void {
  resetMessages();
  if (saveMutation.isPending.value) return;
  const hasFpcalc = Boolean(form.scraping.fpcalcPath.trim());
  const hasClientId = Boolean(form.scraping.acoustIdClient.trim());
  if (hasFpcalc !== hasClientId) {
    tab.value = "media";
    actionError.value = hasFpcalc
      ? "启用音频指纹时还需要填写 AcoustID Client ID"
      : "启用音频指纹时还需要填写 fpcalc 路径";
    return;
  }
  saveMutation.mutate();
}

const SettingField = defineComponent({ inheritAttrs: false, props: { label: { type: String, required: true }, hint: String, locked: Boolean }, setup: (props, { slots, attrs }) => () => h("label", { ...attrs, class: [attrs.class, "block"] }, [h("span", { class: "mb-1.5 flex items-center gap-2" }, [h("span", { class: "text-[13px] font-semibold" }, props.label), props.locked ? h("span", { class: "inline-flex items-center gap-1 rounded-md bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-bold text-amber-700 dark:text-amber-300" }, [h(LockKeyhole, { size: 10 }), "环境变量锁定"]) : null]), slots.default?.(), props.hint ? h("span", { class: "ui-hint block" }, props.hint) : null]) });
const ToggleSetting = defineComponent({ props: { modelValue: Boolean, label: { type: String, required: true }, detail: { type: String, required: true }, disabled: Boolean }, emits: ["update:modelValue"], setup: (props, { emit }) => () => h("label", { class: "flex items-center justify-between gap-4 rounded-xl border border-[var(--border)] p-4", "aria-disabled": props.disabled }, [h("span", [h("span", { class: "block font-semibold" }, props.label), h("span", { class: "mt-1 block text-xs text-[var(--muted)]" }, props.detail)]), h("button", { type: "button", class: "switch", disabled: props.disabled, role: "switch", "aria-label": props.label, "aria-checked": String(props.modelValue), onClick: () => emit("update:modelValue", !props.modelValue) })]) });
const SystemItem = defineComponent({ props: { label: { type: String, required: true }, value: { type: String, required: true } }, setup: (props) => () => h("div", { class: "min-w-0 bg-[var(--surface-solid)] p-4" }, [h("dt", { class: "text-xs font-semibold text-[var(--muted)]" }, props.label), h("dd", { class: "mt-1.5 break-all font-mono text-sm font-semibold" }, props.value)]) });
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="系统设置" description="测试并原子应用真实运行配置；验证失败时自动保留原实例。"><template #eyebrow>配置中心</template><template #actions><AppButton :loading="settingsQuery.isFetching.value" @click="reloadSettings"><template #icon><RefreshCw :size="16" /></template>重新读取</AppButton><AppButton variant="primary" :loading="saveMutation.isPending.value" :disabled="!dirty" @click="save"><template #icon><Save :size="16" /></template>保存并应用</AppButton></template></PageHeader>
    <div v-if="settingsQuery.data.value?.restartRequiredFields.length" class="motion-item flex gap-3 rounded-xl border border-amber-500/20 bg-amber-500/10 p-4 text-sm text-[var(--muted)]"><AlertTriangle :size="18" class="shrink-0 text-amber-500" /><span>监听地址配置将在下次重启生效；当前仍监听 <strong>IPv4 {{ settingsQuery.data.value.actualListener.ipv4.host }}:{{ settingsQuery.data.value.actualListener.ipv4.port }}</strong>，<strong>IPv6 [{{ settingsQuery.data.value.actualListener.ipv6.host }}]:{{ settingsQuery.data.value.actualListener.ipv6.port }}</strong>。</span></div>
    <StatePanel v-if="settingsQuery.isPending.value" state="loading" /><StatePanel v-else-if="settingsQuery.isError.value" state="error" @retry="settingsQuery.refetch()" />
    <template v-else>
      <div class="grid gap-6 xl:grid-cols-[240px_minmax(0,1fr)]"><nav class="ui-card p-2"><button v-for="item in tabs" :key="item.key" type="button" class="pressable flex w-full items-center gap-3 rounded-xl px-3 py-3 text-left text-sm font-semibold" :class="tab === item.key ? 'bg-[var(--primary-soft)] text-[var(--primary)]' : 'text-[var(--muted)] hover:bg-[var(--surface-muted)]'" @click="tab = item.key; resetMessages()"><component :is="item.icon" :size="17" />{{ item.label }}</button></nav>
        <section class="ui-card overflow-hidden"><div class="border-b border-[var(--border)] px-5 py-5 sm:px-7"><h2 class="text-lg font-bold">{{ tabs.find((item) => item.key === tab)?.label }}</h2><p class="mt-1 text-xs text-[var(--muted)]">配置版本 {{ form.version }} · {{ settingsQuery.data.value?.environment }} · {{ settingsQuery.data.value?.configurationSource }}</p></div><div class="p-5 sm:p-7">
          <Transition name="content-swap" mode="out-in">
          <div v-if="tab === 'database'" key="database" class="grid gap-5 sm:grid-cols-3"><SettingField class="sm:col-span-2" label="服务器地址" :locked="locked('database','host')"><input v-model="form.database.host" class="ui-input" :disabled="locked('database','host')" /></SettingField><SettingField label="端口" :locked="locked('database','port')"><input v-model.number="form.database.port" class="ui-input" type="number" min="1" max="65535" :disabled="locked('database','port')" /></SettingField><SettingField label="数据库名" :locked="locked('database','database')"><input v-model="form.database.database" class="ui-input" :disabled="locked('database','database')" /></SettingField><SettingField label="用户名" :locked="locked('database','username')"><input v-model="form.database.username" class="ui-input" autocomplete="username" :disabled="locked('database','username')" /></SettingField><SettingField label="替换密码" :locked="locked('database','password')"><input v-model="databasePassword" class="ui-input" type="password" autocomplete="new-password" :disabled="locked('database','password')" :placeholder="settingsQuery.data.value?.database.passwordConfigured ? '留空保持不变' : '尚未配置'" /></SettingField><SettingField class="sm:col-span-2" label="TLS 模式" :locked="locked('database','sslMode')"><select v-model="form.database.sslMode" class="ui-select" :disabled="locked('database','sslMode')"><option value="disable">关闭</option><option value="prefer">优先使用</option><option value="require">强制加密</option><option value="verify-full">验证证书与主机</option></select></SettingField><SettingField label="最大连接数" :locked="locked('database','maximumConnections')"><input v-model.number="form.database.maximumConnections" class="ui-input" type="number" min="1" max="100" :disabled="locked('database','maximumConnections')" /></SettingField><div class="sm:col-span-3 flex gap-3 rounded-xl border border-amber-500/20 bg-amber-500/8 p-4 text-sm text-[var(--muted)]"><AlertTriangle :size="18" class="shrink-0 text-amber-500" />切换数据库不会复制旧数据；候选库通过连接、迁移和管理员校验后才会切换。</div></div>
          <div v-else-if="tab === 'storage'" key="storage" class="grid gap-5 sm:grid-cols-2"><SettingField class="sm:col-span-2" label="S3 端点" :locked="locked('storage','endpoint')"><input v-model="form.storage.endpoint" class="ui-input" :disabled="locked('storage','endpoint')" /></SettingField><SettingField label="区域" :locked="locked('storage','region')"><input v-model="form.storage.region" class="ui-input" :disabled="locked('storage','region')" /></SettingField><SettingField label="Bucket" :locked="locked('storage','bucket')"><input v-model="form.storage.bucket" class="ui-input" :disabled="locked('storage','bucket')" /></SettingField><SettingField label="Access Key" :locked="locked('storage','accessKeyId')"><input v-model="form.storage.accessKeyId" class="ui-input" :disabled="locked('storage','accessKeyId')" /></SettingField><SettingField label="替换 Secret Key" :locked="locked('storage','secretAccessKey')"><input v-model="storageSecret" class="ui-input" type="password" autocomplete="new-password" :disabled="locked('storage','secretAccessKey')" :placeholder="settingsQuery.data.value?.storage.secretAccessKeyConfigured ? '留空保持不变' : '尚未配置'" /></SettingField><SettingField class="sm:col-span-2" label="公开访问地址" :locked="locked('storage','publicBaseUrl')"><input v-model="form.storage.publicBaseUrl" class="ui-input" :disabled="locked('storage','publicBaseUrl')" /></SettingField><SettingField label="签名 URL 有效期（秒）" :locked="locked('storage','signedUrlTtlSeconds')"><input v-model.number="form.storage.signedUrlTtlSeconds" class="ui-input" type="number" min="30" max="3600" :disabled="locked('storage','signedUrlTtlSeconds')" /></SettingField><SettingField label="最大上传字节数" :locked="locked('storage','maxUploadBytes')" :hint="formatBytes(form.storage.maxUploadBytes)"><input v-model.number="form.storage.maxUploadBytes" class="ui-input" type="number" min="1" :disabled="locked('storage','maxUploadBytes')" /></SettingField><ToggleSetting v-model="form.storage.forcePathStyle" label="Path-style 访问" detail="MinIO 等自托管服务通常需要开启" :disabled="locked('storage','forcePathStyle')" /></div>
          <div v-else-if="tab === 'media'" key="media" class="space-y-5">
            <div>
              <h3 class="text-sm font-bold">FFmpeg（必需）</h3>
              <p class="mt-1 text-xs leading-5 text-[var(--muted)]">转码和媒体探测只使用 ffmpeg 与 ffprobe。</p>
            </div>
            <label class="flex min-w-0 items-start gap-3 rounded-md border border-[var(--border)] bg-[var(--surface-muted)] p-4">
              <input v-model="autoDetectMedia" class="mt-0.5 h-4 w-4 shrink-0 accent-[var(--primary)]" type="checkbox" />
              <span class="min-w-0 break-words text-sm font-semibold">自动检测 FFmpeg，仅需输入所在目录即可</span>
            </label>
            <SettingField v-if="autoDetectMedia" label="FFmpeg 工具目录" :locked="locked('mediaTools','directory')" :hint="`填写后从该目录自动查找 ffmpeg 和 ffprobe；留空则从系统 PATH 查找。${executableRelativePathHint}`"><input v-model="form.mediaTools.directory" class="ui-input font-mono" :disabled="locked('mediaTools','directory')" placeholder="留空使用 PATH" /></SettingField>
            <div v-else class="grid gap-5 sm:grid-cols-2">
              <SettingField label="FFmpeg 路径" :locked="locked('mediaTools','ffmpegPath')" :hint="`支持绝对路径或相对路径；留空则从系统 PATH 查找。${executableRelativePathHint}`"><input v-model="form.mediaTools.ffmpegPath" class="ui-input font-mono" :disabled="locked('mediaTools','ffmpegPath')" placeholder="留空使用 PATH" /></SettingField>
              <SettingField label="FFprobe 路径" :locked="locked('mediaTools','ffprobePath')" :hint="`支持绝对路径或相对路径；留空则从系统 PATH 查找。${executableRelativePathHint}`"><input v-model="form.mediaTools.ffprobePath" class="ui-input font-mono" :disabled="locked('mediaTools','ffprobePath')" placeholder="留空使用 PATH" /></SettingField>
            </div>
            <div class="rounded-xl border border-violet-500/20 bg-violet-500/8 p-4">
              <div class="mb-4 flex gap-3">
                <ServerCog :size="18" class="mt-0.5 shrink-0 text-violet-500" />
                <div><h3 class="text-sm font-bold">音频指纹识别（可选）</h3><p class="mt-1 text-xs leading-5 text-[var(--muted)]">fpcalc 来自 Chromaprint，不属于 FFmpeg。它计算音频指纹并交给 AcoustID 识曲；两项都留空即禁用，不影响扫描或转码。</p></div>
              </div>
              <div class="grid gap-5 sm:grid-cols-2">
                <SettingField label="fpcalc 路径" :locked="locked('scraping','fpcalcPath')" :hint="executableRelativePathHint"><input v-model="form.scraping.fpcalcPath" class="ui-input font-mono" :disabled="locked('scraping','fpcalcPath')" placeholder="tools\\fpcalc.exe" /></SettingField>
                <SettingField label="AcoustID Client ID" :locked="locked('scraping','acoustIdClient')" hint="仅启用音频指纹识别时填写。"><input v-model="form.scraping.acoustIdClient" class="ui-input font-mono" :disabled="locked('scraping','acoustIdClient')" /></SettingField>
              </div>
            </div>
          </div>
          <div v-else-if="tab === 'library'" key="library" class="space-y-5"><div class="grid gap-5 sm:grid-cols-2"><SettingField label="默认音源名称" :locked="locked('localLibrary','name')"><input v-model="form.localLibrary.name" class="ui-input" :disabled="locked('localLibrary','name')" /></SettingField><SettingField label="默认访问模式" :locked="locked('localLibrary','mode')"><select v-model="form.localLibrary.mode" class="ui-select" :disabled="locked('localLibrary','mode')"><option value="READ_ONLY">只读</option><option value="READ_WRITE">读写（Tag 修改或刮削时可选择写回）</option></select></SettingField><SettingField class="sm:col-span-2" label="默认本地音乐目录" :locked="locked('localLibrary','directory')" :hint="executableRelativePathHint"><input v-model="form.localLibrary.directory" class="ui-input font-mono" :disabled="locked('localLibrary','directory')" placeholder="music" /></SettingField><SettingField label="定时扫描间隔（分钟，可留空）" :locked="locked('localLibrary','scanIntervalMinutes')"><input v-model.number="form.localLibrary.scanIntervalMinutes" class="ui-input" type="number" min="5" max="10080" :disabled="locked('localLibrary','scanIntervalMinutes')" /></SettingField></div><p class="rounded-xl bg-[var(--surface-muted)] p-4 text-xs leading-5 text-[var(--muted)]">只读模式不会修改音源；读写模式允许在 Tag 修改或刮削操作中按次选择写回。</p><ToggleSetting v-model="form.localLibrary.enabled" label="启用默认音源" detail="仅用于数据库尚无音源时的初始化默认值" :disabled="locked('localLibrary','enabled')" /><ToggleSetting v-model="form.localLibrary.syncOnStartup" label="启动时同步默认目录" detail="多音源的扫描策略请在音源页面管理" :disabled="locked('localLibrary','syncOnStartup')" /><div class="grid gap-5 sm:grid-cols-2"><SettingField label="默认包含规则" :locked="locked('localLibrary','includePatterns')" hint="每行一个 glob。"><textarea v-model="includePatterns" class="ui-textarea font-mono" :disabled="locked('localLibrary','includePatterns')" /></SettingField><SettingField label="默认排除规则" :locked="locked('localLibrary','excludePatterns')" hint="每行一个 glob。"><textarea v-model="excludePatterns" class="ui-textarea font-mono" :disabled="locked('localLibrary','excludePatterns')" /></SettingField></div></div>
          <div v-else-if="tab === 'access'" key="access" class="space-y-5"><ToggleSetting v-model="form.registration.enabled" label="开放用户注册" detail="关闭时只能由管理员创建账户" :disabled="locked('registration','enabled')" /><div class="grid gap-5 sm:grid-cols-2"><SettingField label="访问令牌 TTL（秒）" :locked="locked('security','accessTokenTtlSeconds')"><input v-model.number="form.security.accessTokenTtlSeconds" class="ui-input" type="number" min="60" max="86400" :disabled="locked('security','accessTokenTtlSeconds')" /></SettingField><SettingField label="刷新令牌 TTL（秒）" :locked="locked('security','refreshTokenTtlSeconds')"><input v-model.number="form.security.refreshTokenTtlSeconds" class="ui-input" type="number" min="3600" max="31536000" :disabled="locked('security','refreshTokenTtlSeconds')" /></SettingField></div><div class="grid gap-5 sm:grid-cols-2"><SettingField label="IPv4 监听 IP" :locked="locked('http','ipv4Host')" hint="修改后下次重启生效。"><input v-model="form.http.ipv4Host" class="ui-input font-mono" :disabled="locked('http','ipv4Host')" /></SettingField><SettingField label="IPv4 监听端口" :locked="locked('http','ipv4Port')" hint="修改后下次重启生效。"><input v-model.number="form.http.ipv4Port" class="ui-input" type="number" min="1" max="65535" :disabled="locked('http','ipv4Port')" /></SettingField><SettingField label="IPv6 监听 IP" :locked="locked('http','ipv6Host')" hint="修改后下次重启生效。"><input v-model="form.http.ipv6Host" class="ui-input font-mono" :disabled="locked('http','ipv6Host')" /></SettingField><SettingField label="IPv6 监听端口" :locked="locked('http','ipv6Port')" hint="修改后下次重启生效。"><input v-model.number="form.http.ipv6Port" class="ui-input" type="number" min="1" max="65535" :disabled="locked('http','ipv6Port')" /></SettingField></div><SettingField label="反向代理 IP（每行一个）" :locked="locked('http','trustedProxyAddresses')" hint="仅用于识别代理传入的真实客户端 IP，不是访问白名单；直接访问服务端时留空。"><textarea v-model="proxies" class="ui-textarea font-mono" :disabled="locked('http','trustedProxyAddresses')" /></SettingField></div>
          <div v-else key="system"><StatePanel v-if="systemQuery.isPending.value" state="loading" compact /><StatePanel v-else-if="systemQuery.isError.value" state="error" compact @retry="systemQuery.refetch()" /><div v-else-if="systemQuery.data.value" class="space-y-5"><dl class="grid gap-px overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--border)] sm:grid-cols-2"><SystemItem label="XyMusic 版本" :value="systemQuery.data.value.applicationVersion" /><SystemItem label="运行时" :value="systemQuery.data.value.runtimeVersion" /><SystemItem label="操作系统" :value="`${systemQuery.data.value.platform} · ${systemQuery.data.value.architecture}`" /><SystemItem label="运行时长" :value="formatDuration(systemQuery.data.value.uptimeSeconds * 1000)" /><SystemItem label="PostgreSQL" :value="systemQuery.data.value.databaseVersion" /><SystemItem label="迁移版本" :value="systemQuery.data.value.migrationVersion" /><SystemItem label="FFmpeg" :value="systemQuery.data.value.ffmpegVersion ?? '未检测到'" /><SystemItem label="Worker" :value="systemQuery.data.value.worker.synchronized ? '运行中 · 配置已同步' : systemQuery.data.value.worker.responsive ? '运行中 · 正在同步配置' : '不可用或未启动'" /><SystemItem label="数据目录" :value="systemQuery.data.value.dataDirectory" /><SystemItem label="配置文件" :value="systemQuery.data.value.configurationFile" /><SystemItem label="配置来源" :value="systemQuery.data.value.configurationSource" /></dl><div><h3 class="mb-3 text-sm font-bold">实时运行指标</h3><dl class="grid gap-px overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--border)] sm:grid-cols-2 xl:grid-cols-4"><SystemItem label="请求总数 / 错误率" :value="systemQuery.data.value.metrics ? `${systemQuery.data.value.metrics.requests.total} / ${(systemQuery.data.value.metrics.requests.errorRate * 100).toFixed(2)}%` : '暂无数据'" /><SystemItem label="平均 / P95 延迟" :value="systemQuery.data.value.metrics ? `${systemQuery.data.value.metrics.requests.averageLatencyMs} / ${systemQuery.data.value.metrics.requests.p95LatencyMs} ms` : '暂无数据'" /><SystemItem label="事件循环延迟" :value="systemQuery.data.value.metrics ? `${systemQuery.data.value.metrics.eventLoop.lagMs} ms` : '暂无数据'" /><SystemItem label="进程内存 RSS" :value="systemQuery.data.value.metrics ? formatBytes(systemQuery.data.value.metrics.memory.rssBytes) : '暂无数据'" /></dl></div><div><h3 class="mb-3 text-sm font-bold">后台队列积压</h3><dl class="grid gap-px overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--border)] sm:grid-cols-3 xl:grid-cols-6"><SystemItem label="总计" :value="String(systemQuery.data.value.queues.total)" /><SystemItem label="转码" :value="String(systemQuery.data.value.queues.media)" /><SystemItem label="扫描" :value="String(systemQuery.data.value.queues.scans)" /><SystemItem label="Tag 写回" :value="String(systemQuery.data.value.queues.writeback)" /><SystemItem label="Tag 抓取" :value="String(systemQuery.data.value.queues.scraping)" /><SystemItem label="清理" :value="String(systemQuery.data.value.queues.cleanup)" /></dl></div></div></div>
          </Transition>
          <div v-if="['database','storage','media','library'].includes(tab)" class="mt-6 flex items-center gap-3 border-t border-[var(--border)] pt-5"><AppButton :loading="testing" @click="testCurrent"><template #icon><CheckCircle2 :size="15" /></template>{{ tab === 'media' ? '测试 FFmpeg' : '测试当前配置' }}</AppButton><span v-if="testMessage && !actionError" class="text-sm font-semibold text-emerald-600 dark:text-emerald-400">{{ testMessage }}</span></div><p v-if="actionError" class="mt-5 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p>
        </div></section>
      </div>
    </template>
  </div>
</template>
