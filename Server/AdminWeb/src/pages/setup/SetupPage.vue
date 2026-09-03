<script setup lang="ts">
import {
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  Circle,
  Database,
  Disc3,
  Eye,
  EyeOff,
  FolderOpen,
  HardDrive,
  ServerCog,
  ShieldCheck,
  Trash2,
  UserRoundCog,
  Wrench,
  XCircle,
} from "lucide-vue-next";
import { useMutation, useQuery } from "@tanstack/vue-query";
import { computed, defineComponent, h, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { z } from "zod";
import { ApiError, apiErrorMessage } from "@/shared/application/api-error";
import type { SetupCompleteInput, SetupValidationResult } from "@/features/setup/domain/models";
import {
  setupStepSchemas,
  validateSetupComplete,
} from "@/features/setup/application/setup-form";
import { useSetup } from "@/app/services/setup";
import { canAdvanceSetupStep } from "@/features/setup/application/setup-navigation";
import AppButton from "@/components/AppButton.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import StatePanel from "@/components/StatePanel.vue";
import { invalidateSetupState, SETUP_STATUS_STALE_MS } from "@/app/router";
import { useUiStore } from "@/stores/ui";
import xymusicIcon from "@/assets/xymusic.png";

const router = useRouter();
const setup = useSetup();
const ui = useUiStore();
const current = ref(0);
const reveal = reactive({ database: false, storage: false, admin: false });
const fieldErrors = ref<Record<string, string>>({});
const actionError = ref("");
const validation = reactive<Record<number, SetupValidationResult | undefined>>({});
const validationRevision = ref(0);
type DatabaseInspection = NonNullable<SetupValidationResult["databaseInspection"]>;
type StorageInspection = NonNullable<SetupValidationResult["storageInspection"]>;
const databaseInspection = ref<DatabaseInspection>();
const storageInspection = ref<StorageInspection>();
const storageDecisionOpen = ref(false);
const databaseDecisionOpen = ref(false);
const databaseResetConfirmation = ref("");


const form = reactive<SetupCompleteInput>({
  http: { ipv4Host: "0.0.0.0", ipv4Port: 3000, ipv6Host: "::", ipv6Port: 3000, trustedProxyAddresses: [] },
  paths: { migrationsDirectory: "migrations", adminWebDirectory: "admin" },
  database: { host: "", port: 5432, database: "", username: "", password: "", sslMode: "prefer", maxConnections: 10 },
  storage: { assetDirectory: "assets", transcodeDirectory: "transcode", maxUploadBytes: 1_073_741_824, transcodeCacheMaxBytes: 10_737_418_240, uploadTtlSeconds: 3600, streamTtlSeconds: 900, streamMaxConcurrent: 4, streamIdleTimeoutSeconds: 30, transcodeTimeoutSeconds: 30 },
  media: { mode: "DIRECTORY", directory: "", ffmpegPath: "", ffprobePath: "" },
  source: { name: "", directory: "music", mode: "READ_ONLY", enabled: true, syncOnStartup: true, scanIntervalMinutes: null, includePatterns: [], excludePatterns: [] },
  registration: { enabled: true },
  administrator: { username: "", displayName: "", password: "" },
});
const executableRelativePathHint = "支持相对或绝对路径；相对路径以服务端二进制文件所在目录为基准。";
const autoDetectMedia = computed({
  get: () => form.media.mode === "DIRECTORY",
  set: (enabled: boolean) => { form.media.mode = enabled ? "DIRECTORY" : "ADVANCED"; },
});


function linesModel(values: string[]) {
  return computed({
    get: () => values.join("\n"),
    set: (value: string) => values.splice(0, values.length, ...[...new Set(value.split(/\r?\n/).map((entry) => entry.trim()).filter(Boolean))]),
  });
}
const trustedProxiesText = linesModel(form.http.trustedProxyAddresses);
const includePatternsText = linesModel(form.source.includePatterns);
const excludePatternsText = linesModel(form.source.excludePatterns);

const statusQuery = useQuery({
  queryKey: ["setup", "status"],
  queryFn: ({ signal }) => setup.status(signal),
  staleTime: SETUP_STATUS_STALE_MS,
  refetchOnMount: false,
  refetchOnWindowFocus: true,
  refetchInterval: (query) => query.state.data?.setupRequired ? 3_000 : false,
});
const steps = computed(() => [
  { key: "http", label: "网络监听", icon: ServerCog },
  { key: "paths", label: "运行目录", icon: FolderOpen },
  { key: "database", label: "数据库", icon: Database },
  { key: "storage", label: "对象存储", icon: HardDrive },
  { key: "media", label: "媒体工具", icon: Wrench },
  { key: "source", label: "音乐音源", icon: Disc3 },
  { key: "admin", label: "管理员", icon: UserRoundCog },
  { key: "review", label: "确认配置", icon: CheckCircle2 },

]);
const isLast = computed(() => current.value === steps.value.length - 1);

function parsedStep<T>(result: z.SafeParseReturnType<unknown, T>): T | undefined {
  fieldErrors.value = {};
  if (result.success) return result.data;
  for (const issue of result.error.issues) fieldErrors.value[issue.path.join(".")] = issue.message;
  return undefined;
}

function setupErrorMessage(error: unknown, fallback: string): string {
  applyServerFieldErrors(error);
  return apiErrorMessage(error, fallback);
}

function applyServerFieldErrors(error: unknown): void {
  if (!(error instanceof ApiError) || !error.problem.fieldErrors) return;
  for (const [field, messages] of Object.entries(error.problem.fieldErrors)) {
    const localField = field.split(".").filter(Boolean).at(-1);
    const message = messages.find((entry) => entry.trim());
    if (localField && message) fieldErrors.value[localField] = message;
  }
}

function resetDatabaseDecisionForRetry(): void {
  invalidateValidation(2);
  databaseInspection.value = undefined;
  form.databaseAction = undefined;
  resetDatabaseDecisionUi();
  current.value = 2;
}

function focusFailedSetupStage(error: unknown): void {
  if (!(error instanceof ApiError)) return;
  if (error.problem.decisionResource === "database") {
    resetDatabaseDecisionForRetry();
    return;
  }
  if (error.problem.decisionResource === "storage") {
    requireStorageDecision();
    current.value = 3;
    return;
  }
  const stage = error.problem.setupStage ?? "";
  const target = stage.startsWith("listener") || stage.startsWith("http") ? 0
    : stage.startsWith("path") ? 1
      : stage.startsWith("database") ? 2
        : stage.startsWith("storage") ? 3
          : stage.startsWith("media") ? 4
            : stage.startsWith("source") ? 5
              : stage.startsWith("administrator") ? 6
                : undefined;
  if (target !== undefined) current.value = target;
}

const existingItemLabels: Record<string, string> = {
  administrator: "可登录的管理员账号",
  librarySource: "音乐音源配置",
  catalog: "曲目、专辑和艺术家数据",
  playlists: "用户歌单数据",
};
function existingItemLabel(value: string): string { return existingItemLabels[value] ?? value; }

function resetDatabaseDecisionUi(): void {
  databaseDecisionOpen.value = false;
  databaseResetConfirmation.value = "";
}

function requireDatabaseDecision(): void {
  form.databaseAction = undefined;
  resetDatabaseDecisionUi();
  databaseDecisionOpen.value = true;
}

function confirmDatabaseReset(): void {
  if (databaseResetConfirmation.value !== form.database.database) return;
  form.databaseAction = "reset";
  databaseDecisionOpen.value = false;
  current.value = 3;
}

function resetStorageDecisionUi(): void {
  storageDecisionOpen.value = false;
}

function requireStorageDecision(): void {
  form.storageAction = undefined;
  resetStorageDecisionUi();
  storageDecisionOpen.value = true;
}

function confirmStorageReset(): void {
  form.storageAction = "reset";
  storageDecisionOpen.value = false;
  current.value = 4;
}


async function validateCurrent(index: number): Promise<boolean> {
  actionError.value = "";
  fieldErrors.value = {};
  validation[index] = undefined;
  const key = steps.value[index]?.key;
  try {
    let result: SetupValidationResult | undefined;
    if (key === "http") {
      const input = parsedStep(setupStepSchemas.http.safeParse(form.http));
      if (!input) return false;
      result = await setup.testHttp(input);
    } else if (key === "paths") {
      const input = parsedStep(setupStepSchemas.paths.safeParse(form.paths));
      if (!input) return false;
      result = await setup.testPaths(input);
    } else if (key === "database") {
      const input = parsedStep(setupStepSchemas.database.safeParse(form.database));
      if (!input) return false;
      result = await setup.testDatabase({
        database: input,
        migrationsDirectory: form.paths.migrationsDirectory.trim(),
      });
      databaseInspection.value = result.databaseInspection;
      if (result.databaseInspection?.state === "EMPTY") {
        form.databaseAction = undefined;
        resetDatabaseDecisionUi();
      } else if (result.databaseInspection) {
        if (form.databaseAction !== "reset") {
          requireDatabaseDecision();
          validation[index] = result;
          return false;
        }
      }
    } else if (key === "storage") {
      const input = parsedStep(setupStepSchemas.storage.safeParse(form.storage));
      if (!input) return false;
      result = await setup.testStorage(input);
      storageInspection.value = result.storageInspection;
      const hasFiles = Boolean(
        result.storageInspection?.hasAssets || result.storageInspection?.hasTranscode
      );
      if (!hasFiles) {
        form.storageAction = undefined;
        resetStorageDecisionUi();
      } else if (form.storageAction !== "reset") {
        requireStorageDecision();
        validation[index] = result;
        return false;
      }
    } else if (key === "media") {

      const input = parsedStep(setupStepSchemas.media.safeParse(form.media));
      if (!input) return false;
      result = await setup.testMedia(input);
    } else if (key === "source") {
      const input = parsedStep(setupStepSchemas.source.safeParse(form.source));
      if (!input) return false;
      result = await setup.testSource(input);
    } else if (key === "admin") {
      const input = parsedStep(setupStepSchemas.administrator.safeParse(form.administrator));
      if (!input) return false;
      result = await setup.testAdministrator(input);
    } else return true;
    validation[index] = result;
    return true;
  } catch (error) {
    actionError.value = setupErrorMessage(error, "验证请求失败，请检查服务器日志");
    return false;
  }
}

const validating = ref(false);
async function next(): Promise<void> {
  if (validating.value) return;
  const expectedIndex = current.value;
  const expectedRevision = validationRevision.value;
  validating.value = true;
  try {
    const validated = await validateCurrent(expectedIndex);
    if (!canAdvanceSetupStep({
      validated,
      expectedIndex,
      currentIndex: current.value,
      expectedRevision,
      currentRevision: validationRevision.value,
    })) {
      if (validated) {
        validation[expectedIndex] = undefined;
        actionError.value = "输入或步骤已发生变化，请重新验证";
      }
      return;
    }
    current.value = expectedIndex + 1;
  } finally { validating.value = false; }
}
function previous(): void {
  if (validating.value || completeMutation.isPending.value) return;
  actionError.value = "";
  fieldErrors.value = {};
  current.value = Math.max(0, current.value - 1);
}

const completeMutation = useMutation({
  mutationFn: (input: SetupCompleteInput) => setup.complete(input),
  onSuccess: async (result, input) => {
    invalidateSetupState();
    if (result.restartRequiredFields.length) {
      ui.notify("warning", "XyMusic 配置完成，监听地址需重启生效", `当前仍监听 IPv4 ${result.actualListener.ipv4.host}:${result.actualListener.ipv4.port}，IPv6 [${result.actualListener.ipv6.host}]:${result.actualListener.ipv6.port}`);
    } else {
      ui.notify(
        "success",
        "XyMusic 配置完成",
        "请使用新建的管理员账户登录",
      );
    }
    await router.replace({ name: "login", query: { username: input.administrator.username } });
  },
  onError: (error) => {
    focusFailedSetupStage(error);
    actionError.value = setupErrorMessage(error, "保存配置失败，请检查服务器日志");
  },
});

function complete(): void {
  if (completeMutation.isPending.value || validating.value) return;
  actionError.value = "";
  fieldErrors.value = {};
  const normalized = validateSetupComplete(form);
  if (normalized.success) {
    completeMutation.mutate(normalized.data as SetupCompleteInput);
    return;
  }

  const section = String(normalized.error.issues[0]?.path[0] ?? "");
  const sectionSteps: Record<string, number> = {
    http: 0,
    paths: 1,
    database: 2,
    storage: 3,
    media: 4,
    source: 5,
    registration: 6,
    administrator: 6,
  };
  const targetStep = sectionSteps[section] ?? 0;
  for (const issue of normalized.error.issues) {
    if (String(issue.path[0] ?? "") !== section) continue;
    fieldErrors.value[issue.path.slice(1).join(".")] = issue.message;
  }
  current.value = targetStep;
  actionError.value = "配置内容已发生变化，请修正后重新验证";
}
function errorFor(name: string): string | undefined { return fieldErrors.value[name]; }

watch(() => statusQuery.data.value?.setupRequired, async (setupRequired) => {
  if (setupRequired !== false) return;
  if (completeMutation.isPending.value || completeMutation.isSuccess.value) return;
  invalidateSetupState();
  const redirect = router.currentRoute.value.query.redirect;
  await router.replace(
    typeof redirect === "string" && redirect.startsWith("/") && !redirect.startsWith("//")
      ? redirect
      : { name: "login" },
  );
}, { immediate: true });
watch(() => statusQuery.isError.value, async (isError) => {
  if (!isError) return;
  await router.replace({
    name: "service-unavailable",
    query: { redirect: router.currentRoute.value.fullPath },
  });
});
function invalidateValidation(index: number): void {
  validation[index] = undefined;
  validationRevision.value += 1;
}

watch(() => form.http, () => { invalidateValidation(0); }, { deep: true });
watch(() => form.paths, () => { invalidateValidation(1); }, { deep: true });
watch(() => form.database, () => {
  invalidateValidation(2);
  databaseInspection.value = undefined;
  form.databaseAction = undefined;
  resetDatabaseDecisionUi();
}, { deep: true });
watch(() => form.storage, () => {
  invalidateValidation(3);
  storageInspection.value = undefined;
  form.storageAction = undefined;
  resetStorageDecisionUi();
}, { deep: true });

watch(() => form.media, () => { invalidateValidation(4); }, { deep: true });
watch(() => form.source, () => { invalidateValidation(5); }, { deep: true });
watch(() => [form.registration, form.administrator], () => { invalidateValidation(6); }, { deep: true });

function validationSummary(index: number): string {
  const result = validation[index];
  if (!result) return "";
  if (result.serverTimeMs !== undefined) return `数据库连接正常 · ${result.serverTimeMs} ms`;
  if (result.ffmpeg && result.ffprobe) {

    return `${result.ffmpeg} · ${result.ffprobe}`;
  }
  if (result.resolvedPaths) return Object.values(result.resolvedPaths).join(" · ");
  if (result.directory) return `目录可读：${result.directory}`;
  return "连接与权限检查通过";
}

const StepTitle = defineComponent({
  props: { title: { type: String, required: true }, description: { type: String, required: true } },
  setup: (props) => () => h("div", [h("p", { class: "text-xs font-medium text-[var(--muted)]" }, "基础配置"), h("h2", { class: "mt-1 text-2xl font-bold" }, props.title), h("p", { class: "mt-2 max-w-2xl text-sm leading-6 text-[var(--muted)]" }, props.description)]),
});
const FieldWrap = defineComponent({
  inheritAttrs: false,
  props: { label: { type: String, required: true }, error: String, hint: String },
  setup: (props, { slots, attrs }) => () => h("div", attrs, [h("label", { class: "ui-label" }, props.label), slots.default?.(), props.error ? h("p", { class: "ui-error" }, props.error) : props.hint ? h("p", { class: "ui-hint" }, props.hint) : null]),
});
const PasswordInput = defineComponent({
  inheritAttrs: false,
  props: { modelValue: { type: String, required: true }, reveal: Boolean },
  emits: ["update:modelValue", "update:reveal"],
  setup: (props, { emit, attrs }) => () => h("div", { class: "relative" }, [h("input", { ...attrs, class: "ui-input !pr-11", type: props.reveal ? "text" : "password", value: props.modelValue, onInput: (event: Event) => emit("update:modelValue", (event.target as HTMLInputElement).value) }), h("button", { type: "button", class: "absolute right-1.5 top-1/2 grid h-8 w-8 -translate-y-1/2 place-items-center rounded text-[var(--muted)] hover:bg-[var(--surface-muted)]", "aria-label": props.reveal ? "隐藏密码" : "显示密码", onClick: () => emit("update:reveal", !props.reveal) }, [h(props.reveal ? EyeOff : Eye, { size: 17 })])]),
});
const ToggleRow = defineComponent({
  props: { modelValue: Boolean, label: { type: String, required: true }, detail: { type: String, required: true } },
  emits: ["update:modelValue"],
  setup: (props, { emit }) => () => h("label", { class: "flex items-center justify-between gap-4 rounded-md border border-[var(--border)] bg-[var(--surface-muted)] p-3" }, [h("span", [h("span", { class: "block font-medium" }, props.label), h("span", { class: "mt-1 block text-xs text-[var(--muted)]" }, props.detail)]), h("button", { type: "button", class: "switch", role: "switch", "aria-checked": String(props.modelValue), onClick: () => emit("update:modelValue", !props.modelValue) })]),
});
const ReviewRow = defineComponent({
  props: { icon: { type: String, required: true }, label: { type: String, required: true }, value: { type: String, required: true } },
  setup: (props) => () => {
    const icons: Record<string, typeof Database> = { database: Database, storage: HardDrive, tools: Wrench, source: FolderOpen, admin: UserRoundCog };
    return h("div", { class: "flex items-center gap-3 p-3 sm:px-4" }, [h("span", { class: "grid h-8 w-8 shrink-0 place-items-center rounded border border-[var(--border)] bg-[var(--surface-muted)] text-[var(--muted)]" }, [h(icons[props.icon] ?? Circle, { size: 16 })]), h("div", { class: "min-w-0" }, [h("p", { class: "text-xs font-medium text-[var(--muted)]" }, props.label), h("p", { class: "mt-0.5 break-all font-medium" }, props.value)])]);
  },
});
</script>

<template>
  <main class="min-h-screen bg-[var(--bg)]">
    <div class="mx-auto flex min-h-screen max-w-[1440px] flex-col border-x border-[var(--border)] bg-[var(--surface-solid)] lg:flex-row">
      <aside class="border-b border-[var(--border)] bg-[var(--surface-muted)] px-5 py-6 sm:px-8 lg:w-[320px] lg:shrink-0 lg:border-b-0 lg:border-r lg:px-7 lg:py-8">
        <div class="flex items-center gap-3">
          <img :src="xymusicIcon" class="h-9 w-9 shrink-0 object-contain" alt="" width="36" height="36" aria-hidden="true" />
          <div><p class="text-sm font-bold">XyMusic</p><p class="text-[11px] text-[var(--muted)]">首次配置</p></div>
        </div>
        <div class="mt-8 hidden border-l-2 border-[var(--primary)] pl-4 lg:block">
          <p class="text-sm font-semibold text-[var(--text)]">配置音乐资料库</p>
          <p class="mt-2 text-sm leading-6 text-[var(--muted)]">每一步都会由服务器验证；敏感信息不会返回到浏览器。</p>
        </div>
        <ol class="mt-7 flex gap-2 overflow-x-auto pb-1 lg:mt-9 lg:block lg:space-y-1">
          <li v-for="(step, index) in steps" :key="step.key" class="min-w-max">
            <button type="button" class="flex w-full items-center gap-3 border-l-2 px-3 py-2 text-left transition" :class="index === current ? 'border-[var(--primary)] bg-[var(--primary-soft)] text-[var(--primary)]' : index < current ? 'border-transparent text-[var(--text)]' : 'border-transparent text-[var(--muted)]'" :disabled="index > current || validating || completeMutation.isPending.value" @click="index < current && (current = index)">
              <span class="grid h-6 w-6 place-items-center rounded border" :class="index < current ? 'border-emerald-500/30 bg-emerald-500/10 text-[var(--success)]' : index === current ? 'border-[var(--primary)] bg-[var(--primary)] text-white' : 'border-[var(--border)] bg-[var(--surface-solid)]'">
                <Check v-if="index < current" :size="14" /><component :is="step.icon" v-else :size="14" />
              </span>
              <span class="text-sm font-semibold">{{ step.label }}</span>
            </button>
          </li>
        </ol>
        <div class="mt-7 hidden border border-[var(--border)] bg-[var(--surface-solid)] p-3 text-xs leading-5 text-[var(--muted)] lg:block">
          <div class="mb-1 flex items-center gap-2 font-semibold text-[var(--text)]"><ServerCog :size="15" />跨平台配置</div>
          Linux、Windows 与容器使用同一套服务端路径校验，不调用注册表、systemd 或本机目录选择器。
        </div>
      </aside>

      <section class="flex min-h-0 flex-1 flex-col">
        <div v-if="statusQuery.isPending.value" class="grid flex-1 place-items-center"><StatePanel state="loading" title="正在读取服务器状态" /></div>
        <div v-else-if="statusQuery.isError.value" class="grid flex-1 place-items-center"><StatePanel state="error" title="无法连接 XyMusic 服务" detail="确认后端服务已启动并可从当前地址访问。" @retry="statusQuery.refetch()" /></div>
        <template v-else>
          <header class="flex h-14 items-center justify-between border-b border-[var(--border)] px-5 sm:px-8">
            <div class="flex items-center gap-2 text-xs font-medium text-[var(--muted)]"><span class="h-1.5 w-1.5 rounded-full bg-emerald-500" />配置服务已连接 · {{ statusQuery.data.value?.runtime.phase }}</div>
            <span class="text-xs font-medium text-[var(--muted)]">步骤 {{ current + 1 }} / {{ steps.length }}</span>
          </header>
          <div class="flex-1 overflow-y-auto px-5 py-8 sm:px-8 lg:px-12 lg:py-11">
            <div class="mx-auto max-w-3xl page-enter" :key="current" :inert="validating || completeMutation.isPending.value">
              <div v-if="current > 0 && validation[current - 1]?.ok" class="mb-6 flex items-start gap-3 rounded-md border border-emerald-500/25 bg-emerald-500/8 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300">
                <CheckCircle2 :size="18" class="mt-0.5 shrink-0" />
                <div><p class="font-semibold">{{ steps[current - 1]?.label }}验证通过</p><p>{{ validationSummary(current - 1) }}</p></div>
              </div>
              <template v-if="steps[current]?.key === 'http'">
                <StepTitle title="配置服务监听" description="IPv4 与 IPv6 分别监听；默认开放全部本机 IP，并共同使用 3000 端口。修改后将在下次重启生效。" />
                <div class="mt-8 grid gap-5 sm:grid-cols-2">
                  <FieldWrap label="IPv4 监听 IP" :error="errorFor('ipv4Host')"><input v-model="form.http.ipv4Host" class="ui-input font-mono" placeholder="0.0.0.0" /></FieldWrap>
                  <FieldWrap label="IPv4 监听端口" :error="errorFor('ipv4Port')"><input v-model.number="form.http.ipv4Port" class="ui-input" type="number" min="1" max="65535" /></FieldWrap>
                  <FieldWrap label="IPv6 监听 IP" :error="errorFor('ipv6Host')"><input v-model="form.http.ipv6Host" class="ui-input font-mono" placeholder="::" /></FieldWrap>
                  <FieldWrap label="IPv6 监听端口" :error="errorFor('ipv6Port')"><input v-model.number="form.http.ipv6Port" class="ui-input" type="number" min="1" max="65535" /></FieldWrap>
                  <details class="sm:col-span-2 rounded-xl border border-[var(--border)] bg-[var(--surface)] px-4 py-3">
                    <summary class="cursor-pointer font-semibold">反向代理（可选）</summary>
                    <div class="mt-5 grid gap-5">
                      <FieldWrap label="反向代理 IP（每行一个）" :error="errorFor('trustedProxyAddresses')" hint="仅用于识别代理传入的真实客户端 IP，不是访问白名单；直接访问服务端时留空。"><textarea v-model="trustedProxiesText" class="ui-input min-h-24 font-mono" placeholder="127.0.0.1" /></FieldWrap>
                    </div>
                  </details>
                </div>
              </template>

              <template v-else-if="steps[current]?.key === 'paths'">
                <StepTitle title="配置运行目录" description="目录可以填写相对路径或绝对路径；相对路径统一以 XyMusic 可执行文件所在目录为基准。" />
                <div class="mt-8 grid gap-5">
                  <FieldWrap label="数据库迁移目录" :error="errorFor('migrationsDirectory')" :hint="`默认 migrations；目录中必须包含 SQL 文件和 meta/_journal.json。${executableRelativePathHint}`">
                    <input v-model="form.paths.migrationsDirectory" class="ui-input font-mono" placeholder="migrations" />
                  </FieldWrap>
                  <FieldWrap label="管理端资源目录" :error="errorFor('adminWebDirectory')" :hint="`默认 admin；目录中必须包含 index.html 和构建后的静态资源。${executableRelativePathHint}`">
                    <input v-model="form.paths.adminWebDirectory" class="ui-input font-mono" placeholder="admin" />
                  </FieldWrap>
                </div>
              </template>

              <template v-else-if="steps[current]?.key === 'database'">
                <StepTitle title="连接 PostgreSQL" description="连接成功后将检查权限并执行必要的数据库迁移。" />
                <div class="mt-8 grid gap-5 sm:grid-cols-3">
                  <FieldWrap class="sm:col-span-2" label="数据库 IP" :error="errorFor('host')"><input v-model="form.database.host" class="ui-input" placeholder="127.0.0.1" /></FieldWrap>
                  <FieldWrap label="端口" :error="errorFor('port')"><input v-model.number="form.database.port" class="ui-input" type="number" min="1" max="65535" /></FieldWrap>
                  <FieldWrap label="数据库名" :error="errorFor('database')"><input v-model="form.database.database" class="ui-input" /></FieldWrap>
                  <FieldWrap label="用户名" :error="errorFor('username')"><input v-model="form.database.username" class="ui-input" autocomplete="username" /></FieldWrap>
                  <FieldWrap label="密码" :error="errorFor('password')"><PasswordInput v-model="form.database.password" v-model:reveal="reveal.database" autocomplete="current-password" /></FieldWrap>
                  <details class="sm:col-span-3 rounded-xl border border-[var(--border)] bg-[var(--surface)] px-4 py-3">
                    <summary class="cursor-pointer font-semibold">高级选项</summary>
                    <div class="mt-5 grid gap-5 sm:grid-cols-2">
                      <FieldWrap label="SSL 模式" :error="errorFor('sslMode')"><select v-model="form.database.sslMode" class="ui-input"><option value="disable">不使用 SSL</option><option value="prefer">优先 SSL</option><option value="require">必须使用 SSL</option><option value="verify-full">验证证书</option></select></FieldWrap>
                      <FieldWrap label="最大连接数" :error="errorFor('maxConnections')"><input v-model.number="form.database.maxConnections" class="ui-input" type="number" min="1" max="100" /></FieldWrap>
                    </div>
                  </details>
                </div>
              </template>

              <template v-else-if="steps[current]?.key === 'storage'">
                <StepTitle title="配置本地资产与转码存储" description="指定媒体资产存储路径与实时转码临时目录，音频与封面将直接保存在服务端本地磁盘。" />
                <div class="mt-8 grid gap-5 sm:grid-cols-2">
                  <FieldWrap class="sm:col-span-2" label="媒体资产目录" :error="errorFor('assetDirectory')" hint="用于存储上传的音频文件、封面图片及头像"><input v-model="form.storage.assetDirectory" class="ui-input font-mono" placeholder="assets" /></FieldWrap>
                  <FieldWrap class="sm:col-span-2" label="转码临时目录" :error="errorFor('transcodeDirectory')" hint="用于存储即时转码的临时音频文件"><input v-model="form.storage.transcodeDirectory" class="ui-input font-mono" placeholder="transcode" /></FieldWrap>
                  <FieldWrap label="单文件最大上传字节数" :error="errorFor('maxUploadBytes')"><input v-model.number="form.storage.maxUploadBytes" class="ui-input" type="number" min="1" /></FieldWrap>
                  <FieldWrap label="转码缓存上限（字节）" :error="errorFor('transcodeCacheMaxBytes')" hint="已完成的转码版本会保存在转码目录，超过上限后按最近最少使用清理"><input v-model.number="form.storage.transcodeCacheMaxBytes" class="ui-input" type="number" min="134217728" /></FieldWrap>
                  <FieldWrap label="最大并发转码任务数" :error="errorFor('streamMaxConcurrent')"><input v-model.number="form.storage.streamMaxConcurrent" class="ui-input" type="number" min="1" max="100" /></FieldWrap>
                </div>
              </template>

              <template v-else-if="steps[current]?.key === 'media'">
                <StepTitle title="检测 FFmpeg" description="支持绝对路径、相对于服务端二进制文件所在目录的相对路径；留空时从系统 PATH 查找 ffmpeg 和 ffprobe。" />
                <div class="mt-8 space-y-5">
                  <label class="flex min-w-0 items-start gap-3 rounded-md border border-[var(--border)] bg-[var(--surface-muted)] p-4">
                    <input v-model="autoDetectMedia" class="mt-0.5 h-4 w-4 shrink-0 accent-[var(--primary)]" type="checkbox" />
                    <span class="min-w-0 break-words text-sm font-semibold">自动检测 FFmpeg，仅需输入所在目录即可</span>
                  </label>
                  <FieldWrap v-if="form.media.mode === 'DIRECTORY'" label="FFmpeg 工具目录" :error="errorFor('directory')" :hint="`填写后从该目录自动查找 ffmpeg 和 ffprobe；留空则从系统 PATH 查找。${executableRelativePathHint}`"><input v-model="form.media.directory" class="ui-input font-mono" placeholder="留空使用 PATH" /></FieldWrap>
                  <div v-else class="grid gap-5 sm:grid-cols-2">
                    <FieldWrap label="FFmpeg 路径" :error="errorFor('ffmpegPath')" :hint="`留空则从系统 PATH 查找。${executableRelativePathHint}`"><input v-model="form.media.ffmpegPath" class="ui-input font-mono" placeholder="留空使用 PATH" /></FieldWrap>
                    <FieldWrap label="FFprobe 路径" :error="errorFor('ffprobePath')" :hint="`留空则从系统 PATH 查找。${executableRelativePathHint}`"><input v-model="form.media.ffprobePath" class="ui-input font-mono" placeholder="留空使用 PATH" /></FieldWrap>
                  </div>
                </div>
              </template>

              <template v-else-if="steps[current]?.key === 'source'">
                <StepTitle title="添加第一个音乐音源" description="填写 XyMusic 服务进程能够访问的目录。完成配置后可继续添加更多音源。" />
                <div class="mt-8 grid gap-5 sm:grid-cols-2">
                  <FieldWrap label="音源名称" :error="errorFor('name')"><input v-model="form.source.name" class="ui-input" /></FieldWrap>
                  <FieldWrap label="访问模式" :error="errorFor('mode')"><select v-model="form.source.mode" class="ui-input"><option value="READ_ONLY">只读</option><option value="READ_WRITE">读写（Tag 修改或刮削时可选择写回）</option></select></FieldWrap>
                  <FieldWrap class="sm:col-span-2" label="服务端目录" :error="errorFor('directory')" :hint="`默认 music；${executableRelativePathHint}`"><input v-model="form.source.directory" class="ui-input font-mono" placeholder="music" /></FieldWrap>
                  <FieldWrap label="定时扫描间隔（分钟，可留空）" :error="errorFor('scanIntervalMinutes')"><input v-model.number="form.source.scanIntervalMinutes" class="ui-input" type="number" min="5" max="10080" /></FieldWrap>
                  <div class="space-y-3"><ToggleRow v-model="form.source.enabled" label="启用音源" detail="关闭时不会扫描或提供该音源内容" /><ToggleRow v-model="form.source.syncOnStartup" label="启动时同步" detail="服务启动后自动检查新增和变更的文件" /></div>
                  <FieldWrap label="包含规则（每行一个）" :error="errorFor('includePatterns')"><textarea v-model="includePatternsText" class="ui-input min-h-28 font-mono" placeholder="**/*.flac" /></FieldWrap>
                  <FieldWrap label="排除规则（每行一个）" :error="errorFor('excludePatterns')"><textarea v-model="excludePatternsText" class="ui-input min-h-28 font-mono" placeholder="**/tmp/**" /></FieldWrap>
                </div>
              </template>

              <template v-else-if="steps[current]?.key === 'admin'">
                <StepTitle
                  title="创建首位管理员"
                  description="该账户拥有完整管理权限。你可以选择是否允许其他用户自行注册。"
                />
                <div class="mt-8 space-y-5">
                  <FieldWrap label="管理员用户名" :error="errorFor('username')"><input v-model="form.administrator.username" class="ui-input" autocomplete="username" placeholder="admin" /></FieldWrap>
                  <FieldWrap label="显示名称" :error="errorFor('displayName')"><input v-model="form.administrator.displayName" class="ui-input" autocomplete="name" /></FieldWrap>
                  <FieldWrap label="管理员密码" :error="errorFor('password')" hint="6–128 个字符。"><PasswordInput v-model="form.administrator.password" v-model:reveal="reveal.admin" autocomplete="new-password" /></FieldWrap>
                  <ToggleRow v-model="form.registration.enabled" label="开放用户注册" detail="关闭后仅管理员可创建用户。" />
                </div>
              </template>

              <template v-else>
                <StepTitle title="确认并应用配置" description="服务会依次迁移数据库、保存 .env 配置、创建管理员和音源。候选运行验证失败时可修正后重试。" />
                <div class="mt-8 divide-y divide-[var(--border)] overflow-hidden rounded-md border border-[var(--border)] bg-[var(--surface)]">
                  <ReviewRow icon="source" label="数据库迁移" :value="form.paths.migrationsDirectory" />
                  <ReviewRow icon="source" label="管理端资源" :value="form.paths.adminWebDirectory" />
                  <ReviewRow icon="database" label="PostgreSQL" :value="`${form.database.host}:${form.database.port}/${form.database.database} · 最大 ${form.database.maxConnections} 个连接`" />
                  <ReviewRow icon="storage" label="资产与转码" :value="`资产：${form.storage.assetDirectory} · 转码：${form.storage.transcodeDirectory}`" />
                  <ReviewRow v-if="form.storageAction" icon="storage" label="存储处理" value="全部清空媒体资产与转码缓存目录" />

                  <ReviewRow icon="tools" label="FFmpeg" :value="form.media.mode === 'DIRECTORY' ? `自动检测：${form.media.directory || '系统 PATH'}` : `${form.media.ffmpegPath || '系统 PATH'} · ${form.media.ffprobePath || '系统 PATH'}`" />
                  <ReviewRow icon="source" label="音乐音源" :value="`${form.source.name} · ${form.source.directory} · ${form.source.mode}`" />
                  <ReviewRow icon="admin" label="管理员" :value="`${form.administrator.username} · ${form.administrator.displayName}`" />
                </div>
                <div class="mt-5 flex gap-3 rounded-md border border-amber-500/25 bg-amber-500/8 p-4 text-sm leading-6 text-[var(--muted)]"><ShieldCheck :size="20" class="mt-0.5 shrink-0 text-amber-500" /><p>配置将保存到后端 `.env` 文件，请限制该文件权限并做好持久化备份。</p></div>
              </template>

              <div v-if="validation[current]?.ok" class="mt-6 flex items-start gap-3 rounded-md border border-emerald-500/25 bg-emerald-500/8 px-4 py-3 text-sm text-emerald-700 dark:text-emerald-300"><CheckCircle2 :size="18" class="mt-0.5 shrink-0" /><div><p class="font-semibold">验证通过</p><p>{{ validationSummary(current) }}</p></div></div>
              <div v-if="actionError" role="alert" class="mt-6 flex items-start gap-3 rounded-md border border-rose-500/25 bg-rose-500/8 px-4 py-3 text-sm text-[var(--danger)]"><XCircle :size="18" class="mt-0.5 shrink-0" /><span class="whitespace-pre-line">{{ actionError }}</span></div>
            </div>
          </div>
          <footer class="border-t border-[var(--border)] bg-[var(--surface-solid)] px-5 py-3 sm:px-8 lg:px-12">
            <div class="mx-auto flex max-w-3xl items-center justify-between gap-3">
              <AppButton variant="ghost" :disabled="current === 0 || validating || completeMutation.isPending.value" @click="previous"><template #icon><ArrowLeft :size="16" /></template>上一步</AppButton>
              <AppButton v-if="!isLast" variant="primary" :loading="validating" @click="next">验证并继续<template #icon><ArrowRight :size="16" /></template></AppButton>
              <AppButton v-else variant="primary" :loading="completeMutation.isPending.value" @click="complete">应用配置并进入控制台<template #icon><Check :size="16" /></template></AppButton>
            </div>
          </footer>
        </template>
      </section>
    </div>

    <BaseDialog v-if="databaseInspection && databaseInspection.state !== 'EMPTY'" v-model="databaseDecisionOpen" title="检测到现有数据库不为空" description="本项目不支持复用旧数据库，继续初始化将会完全清空数据库中的所有旧数据。" prevent-close width="lg">
      <div class="min-w-0 space-y-6 overflow-x-hidden">
        <div class="flex min-w-0 items-start gap-3 border-b border-[var(--border)] pb-5">
          <span class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-rose-500/10 text-[var(--danger)]"><Database :size="18" /></span>
          <div class="min-w-0">
            <p class="font-semibold">{{ form.database.database }}</p>
            <p class="mt-1 break-words text-sm leading-6 text-[var(--muted)]">目标数据库已存在数据或历史表结构。由于本项目不支持复用旧数据库，继续初始化将会彻底清空该数据库内全部现有表与数据。</p>
          </div>
        </div>

        <section class="rounded-md border border-rose-500/25 bg-rose-500/8 p-4">
          <h3 class="flex items-center gap-2 text-sm font-semibold text-[var(--danger)]"><Trash2 :size="16" />清空数据库高危警告</h3>
          <p class="mt-2 text-sm leading-6 text-[var(--muted)]">
            初始化时将执行<strong>清空重置</strong>，删除当前数据库内全部旧数据并重新创建基础表（不会清除本地音频媒体源文件）。该操作不可逆，请务必确认是否继续。
          </p>
        </section>

        <div class="min-w-0 border-t border-[var(--border)] pt-5 text-sm">
          <label class="block break-words font-medium text-[var(--text)]">请输入数据库名“{{ form.database.database }}”确认清空并继续：</label>
          <input v-model="databaseResetConfirmation" class="ui-input mt-2 min-w-0 w-full" placeholder="输入数据库名确认" autocomplete="off" />
        </div>
      </div>
      <template #footer>
        <AppButton variant="ghost" @click="resetDatabaseDecisionUi">返回修改</AppButton>
        <AppButton variant="danger" :disabled="databaseResetConfirmation !== form.database.database" @click="confirmDatabaseReset">确认清空并继续</AppButton>
      </template>
    </BaseDialog>

    <BaseDialog v-if="storageInspection && (storageInspection.hasAssets || storageInspection.hasTranscode)" v-model="storageDecisionOpen" title="检测到存储目录不为空" description="本项目不支持复用旧媒体资产或转码缓存，继续初始化将会完全清空目录中的所有现有文件。" prevent-close width="lg">
      <div class="min-w-0 space-y-6 overflow-x-hidden">
        <div class="flex min-w-0 items-start gap-3 border-b border-[var(--border)] pb-5">
          <span class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-rose-500/10 text-[var(--danger)]"><HardDrive :size="18" /></span>
          <div class="min-w-0">
            <p class="font-semibold">存储目录包含历史文件</p>
            <p class="mt-1 break-words text-sm leading-6 text-[var(--muted)]">
              媒体资产目录包含 {{ storageInspection.assetCount }} 个文件/条目，转码临时目录包含 {{ storageInspection.transcodeCount }} 个文件/条目。由于转码缓存和未嵌入音频原文件的旧封面无法在新实例中直接复用，继续操作将会彻底清空这两个目录。
            </p>
          </div>
        </div>

        <section class="rounded-md border border-rose-500/25 bg-rose-500/8 p-4">
          <h3 class="flex items-center gap-2 text-sm font-semibold text-[var(--danger)]"><Trash2 :size="16" />清空存储目录高危警告</h3>
          <p class="mt-2 text-sm leading-6 text-[var(--muted)]">
            初始化时将<strong>清空媒体资产目录（{{ form.storage.assetDirectory }}）与转码缓存目录（{{ form.storage.transcodeDirectory }}）</strong>。注意：这<strong>不会</strong>影响您的音频源文件目录（{{ form.source.directory }}）。初始化完成后，系统将自动重新扫描音频并提取内嵌封面。
          </p>
        </section>
      </div>
      <template #footer>
        <AppButton variant="ghost" @click="resetStorageDecisionUi">返回修改</AppButton>
        <AppButton variant="danger" @click="confirmStorageReset">确认清空并继续</AppButton>
      </template>
    </BaseDialog>

  </main>
</template>

