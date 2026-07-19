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
  isReachableStorageHost,
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
const databaseDecisionOpen = ref(false);
const storageDecisionOpen = ref(false);
const databaseDecision = ref<"reuse_partial" | "migrate" | "reset">();
const storageDecision = ref<"reuse" | "reset">();
const databaseResetConfirmation = ref("");
const storageResetConfirmation = ref("");
const submittedWithReusedAdministrator = ref(false);

const form = reactive<SetupCompleteInput>({
  http: { ipv4Host: "0.0.0.0", ipv4Port: 3000, ipv6Host: "::", ipv6Port: 3000, trustedProxyAddresses: [] },
  paths: { migrationsDirectory: "migrations", adminWebDirectory: "admin" },
  database: { host: "", port: 5432, database: "", username: "", password: "", sslMode: "prefer", maxConnections: 10 },
  storage: { endpoint: "", region: "us-east-1", bucket: "xymusic", accessKeyId: "", secretAccessKey: "", forcePathStyle: true, publicBaseUrl: "", signedUrlTtlSeconds: 300, maxUploadBytes: 1_073_741_824 },
  media: { mode: "DIRECTORY", directory: "tools", ffmpegPath: "", ffprobePath: "", fpcalcPath: "", acoustIdClient: "" },
  source: { name: "", directory: "music", mode: "READ_ONLY", enabled: true, syncOnStartup: true, scanIntervalMinutes: null, includePatterns: [], excludePatterns: [] },
  registration: { enabled: true },
  administrator: { username: "", displayName: "", password: "" },
});
const executableRelativePathHint = "支持相对或绝对路径；相对路径以服务端二进制文件所在目录为基准。";
const fingerprintConfigured = computed(() => Boolean(
  form.media.fpcalcPath.trim() || form.media.acoustIdClient.trim(),
));
const autoDetectMedia = computed({
  get: () => form.media.mode === "DIRECTORY",
  set: (enabled: boolean) => { form.media.mode = enabled ? "DIRECTORY" : "ADVANCED"; },
});
const reusesDatabase = computed(() => (
  form.databaseAction === "reuse_partial" || form.databaseAction === "migrate"
));
const reusesActiveAdministrator = computed(() => (
  reusesDatabase.value && Boolean(databaseInspection.value?.hasActiveAdministrator)
));

type EndpointProtocol = "http" | "https";
const storageConnection = reactive<{ protocol: EndpointProtocol; host: string; port: number }>({
  protocol: "http",
  host: "",
  port: 9000,
});
const storagePublicConnection = reactive<{ protocol: EndpointProtocol; host: string; port: number }>({
  protocol: "http",
  host: "",
  port: 9000,
});
const storageConnectionSchema = z.object({
  protocol: z.enum(["http", "https"]),
  host: z.string().trim().min(1, "请输入对象存储 IP").max(255).refine(
    isReachableStorageHost,
    "不能使用 localhost、127.0.0.0/8、0.0.0.0 或 IPv6 回环地址",
  ),
  port: z.coerce.number().int().min(1, "端口必须在 1–65535 之间").max(65_535, "端口必须在 1–65535 之间"),
});

function endpointUrl(protocol: EndpointProtocol, host: string, port: number): string {
  const normalizedHost = host.trim();
  if (!normalizedHost) return "";
  const authority = normalizedHost.includes(":")
    && !(normalizedHost.startsWith("[") && normalizedHost.endsWith("]"))
    ? `[${normalizedHost}]`
    : normalizedHost;
  return `${protocol}://${authority}:${port}`;
}
function syncStorageEndpoints(): void {
  form.storage.endpoint = endpointUrl(
    storageConnection.protocol,
    storageConnection.host,
    storageConnection.port,
  );
  form.storage.publicBaseUrl = endpointUrl(
    storagePublicConnection.protocol,
    storagePublicConnection.host,
    storagePublicConnection.port,
  ) || undefined;
}
function validateStorageConnection(): boolean {
  const result = storageConnectionSchema.safeParse(storageConnection);
  if (!result.success) {
    fieldErrors.value.endpoint = result.error.issues[0]?.message ?? "对象存储地址无效";
    return false;
  }
  if (storagePublicConnection.host.trim() && !isReachableStorageHost(storagePublicConnection.host)) {
    fieldErrors.value.publicBaseUrl = "公开地址不能使用 localhost、127.0.0.0/8、0.0.0.0 或 IPv6 回环地址";
    return false;
  }
  return true;
}

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
  { key: "admin", label: reusesActiveAdministrator.value ? "访问设置" : "管理员", icon: UserRoundCog },
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
    current.value = 3;
    return;
  }
  const stage = error.problem.setupStage ?? "";
  if (stage === "installation_provision" && reusesActiveAdministrator.value) {
    resetDatabaseDecisionForRetry();
    return;
  }
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
  databaseDecision.value = undefined;
  databaseResetConfirmation.value = "";
}

function requireDatabaseDecision(): void {
  form.databaseAction = undefined;
  resetDatabaseDecisionUi();
  databaseDecisionOpen.value = true;
}

function selectDatabaseDecision(action: "reuse_partial" | "migrate" | "reset"): void {
  if (action !== "reset") databaseResetConfirmation.value = "";
  databaseDecision.value = action;
}

function confirmDatabaseAction(expectedState: "PARTIAL" | "COMPLETE"): void {
  if (databaseInspection.value?.state !== expectedState) return;
  const reuseAction = expectedState === "COMPLETE" ? "migrate" : "reuse_partial";
  if (databaseDecision.value !== reuseAction && databaseDecision.value !== "reset") return;
  if (databaseDecision.value === "reset" && databaseResetConfirmation.value !== form.database.database) return;
  if (databaseDecision.value === reuseAction && databaseInspection.value.hasActiveAdministrator) {
    Object.assign(form.administrator, { username: "", displayName: "", password: "" });
  }
  form.databaseAction = databaseDecision.value;
  databaseDecisionOpen.value = false;
  current.value = 3;
}
function confirmStorageAction(): void {
  if (!storageDecision.value) return;
  if (storageDecision.value === "reset" && storageResetConfirmation.value !== form.storage.bucket) return;
  form.storageAction = storageDecision.value;
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
        const decisionMatches = result.databaseInspection.state === "PARTIAL"
          ? form.databaseAction === "reuse_partial" || form.databaseAction === "reset"
          : form.databaseAction === "migrate" || form.databaseAction === "reset";
        if (!decisionMatches) {
          requireDatabaseDecision();
          validation[index] = result;
          return false;
        }
      }
    } else if (key === "storage") {
      syncStorageEndpoints();
      if (!validateStorageConnection()) return false;
      const input = parsedStep(setupStepSchemas.storage.safeParse(form.storage));
      if (!input) return false;
      result = await setup.testStorage(input);
      storageInspection.value = result.storageInspection;
      if (!result.storageInspection?.hasObjects) {
        form.storageAction = undefined;
      } else if (form.storageAction !== "reuse" && form.storageAction !== "reset") {
        form.storageAction = undefined;
        storageDecision.value = undefined;
        storageResetConfirmation.value = "";
        storageDecisionOpen.value = true;
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
      if (reusesActiveAdministrator.value) {
        result = { ok: true };
      } else {
        const input = parsedStep(setupStepSchemas.administrator.safeParse(form.administrator));
        if (!input) return false;
        result = await setup.testAdministrator(input);
      }
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
        submittedWithReusedAdministrator.value ? "请使用现有管理员账户登录" : "请使用新建的管理员账户登录",
      );
    }
    if (submittedWithReusedAdministrator.value) {
      await router.replace({ name: "login" });
    } else {
      await router.replace({ name: "login", query: { username: input.administrator.username } });
    }
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
  syncStorageEndpoints();
  if (reusesActiveAdministrator.value) {
    Object.assign(form.administrator, { username: "", displayName: "", password: "" });
  }
  const normalized = validateSetupComplete(form, {
    reusesActiveAdministrator: reusesActiveAdministrator.value,
  });
  if (normalized.success) {
    submittedWithReusedAdministrator.value = reusesActiveAdministrator.value;
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
watch(() => form.paths, () => { invalidateValidation(1); invalidateValidation(2); }, { deep: true });
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
}, { deep: true });
watch(
  () => [storageConnection.protocol, storageConnection.host, storageConnection.port],
  () => { syncStorageEndpoints(); invalidateValidation(3); },
  { immediate: true },
);
watch(
  () => [storagePublicConnection.protocol, storagePublicConnection.host, storagePublicConnection.port],
  syncStorageEndpoints,
);
watch(() => form.media, () => { invalidateValidation(4); }, { deep: true });
watch(() => form.source, () => { invalidateValidation(5); }, { deep: true });
watch(() => [form.registration, form.administrator], () => { invalidateValidation(6); }, { deep: true });

function validationSummary(index: number): string {
  const result = validation[index];
  if (!result) return "";
  if (steps.value[index]?.key === "admin" && reusesActiveAdministrator.value) return "已确认复用数据库中的现有管理员";
  if (result.serverTimeMs !== undefined) return `数据库连接正常 · ${result.serverTimeMs} ms`;
  if (result.ffmpeg && result.ffprobe) {
    return [result.ffmpeg, result.ffprobe, result.fpcalc ? `音频指纹：${result.fpcalc}` : undefined]
      .filter(Boolean)
      .join(" · ");
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
                <StepTitle title="配置 S3 兼容存储" description="支持 Amazon S3、MinIO 及其他标准 S3 兼容服务，不依赖厂商专有 API。" />
                <div class="mt-8 grid gap-5 sm:grid-cols-2">
                  <FieldWrap label="协议"><select v-model="storageConnection.protocol" class="ui-input"><option value="http">HTTP</option><option value="https">HTTPS</option></select></FieldWrap>
                  <FieldWrap label="对象存储 IP 或域名" :error="errorFor('endpoint')"><input v-model="storageConnection.host" class="ui-input" placeholder="minio.example.com" /></FieldWrap>
                  <FieldWrap label="端口"><input v-model.number="storageConnection.port" class="ui-input" type="number" min="1" max="65535" /></FieldWrap>
                  <FieldWrap label="Access Key" :error="errorFor('accessKeyId')"><input v-model="form.storage.accessKeyId" class="ui-input" autocomplete="username" /></FieldWrap>
                  <FieldWrap label="Secret Key" :error="errorFor('secretAccessKey')"><PasswordInput v-model="form.storage.secretAccessKey" v-model:reveal="reveal.storage" autocomplete="current-password" /></FieldWrap>
                  <details class="sm:col-span-2 rounded-xl border border-[var(--border)] bg-[var(--surface)] px-4 py-3">
                    <summary class="cursor-pointer font-semibold">高级选项</summary>
                    <div class="mt-5 grid gap-5 sm:grid-cols-2">
                      <FieldWrap label="区域" :error="errorFor('region')"><input v-model="form.storage.region" class="ui-input" /></FieldWrap>
                      <FieldWrap label="Bucket" :error="errorFor('bucket')"><input v-model="form.storage.bucket" class="ui-input" /></FieldWrap>
                      <FieldWrap label="公开地址协议"><select v-model="storagePublicConnection.protocol" class="ui-input"><option value="http">HTTP</option><option value="https">HTTPS</option></select></FieldWrap>
                      <FieldWrap label="公开地址 IP 或域名" :error="errorFor('publicBaseUrl')"><input v-model="storagePublicConnection.host" class="ui-input" placeholder="客户端可直接访问的地址" /></FieldWrap>
                      <FieldWrap label="公开地址端口"><input v-model.number="storagePublicConnection.port" class="ui-input" type="number" min="1" max="65535" /></FieldWrap>
                      <FieldWrap label="签名 URL 有效秒数" :error="errorFor('signedUrlTtlSeconds')"><input v-model.number="form.storage.signedUrlTtlSeconds" class="ui-input" type="number" min="30" max="3600" /></FieldWrap>
                      <FieldWrap label="最大上传字节数" :error="errorFor('maxUploadBytes')"><input v-model.number="form.storage.maxUploadBytes" class="ui-input" type="number" min="1" /></FieldWrap>
                      <label class="flex items-center justify-between gap-4 rounded-xl border border-[var(--border)] p-4"><span><span class="block font-semibold">Path-style 访问</span><span class="mt-1 block text-xs text-[var(--muted)]">MinIO 与多数自托管存储建议开启</span></span><button type="button" class="switch" role="switch" :aria-checked="form.storage.forcePathStyle" @click="form.storage.forcePathStyle = !form.storage.forcePathStyle" /></label>
                    </div>
                  </details>
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
                  <div class="rounded-xl border border-violet-500/20 bg-violet-500/8 p-4">
                    <div class="mb-4">
                      <p class="font-semibold text-[var(--text)]">音频指纹识别（可选）</p>
                      <p class="mt-1 text-sm leading-6 text-[var(--muted)]">fpcalc 来自 Chromaprint，不属于 FFmpeg。它会计算音频指纹，再通过 AcoustID 识别曲目；两项都留空即禁用，不影响初始化、扫描或转码。</p>
                    </div>
                    <div class="grid gap-5 sm:grid-cols-2">
                      <FieldWrap label="fpcalc 路径" :error="errorFor('fpcalcPath')" :hint="executableRelativePathHint"><input v-model="form.media.fpcalcPath" class="ui-input font-mono" placeholder="tools\\fpcalc.exe" /></FieldWrap>
                      <FieldWrap label="AcoustID Client ID" :error="errorFor('acoustIdClient')" hint="仅启用音频指纹识别时填写。"><input v-model="form.media.acoustIdClient" class="ui-input font-mono" /></FieldWrap>
                    </div>
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
                  :title="reusesActiveAdministrator ? '复用现有管理员' : '创建首位管理员'"
                  :description="reusesActiveAdministrator ? '数据库中的活跃管理员将直接复用，现有用户名和密码不会被修改。' : '该账户拥有完整管理权限。你可以选择是否允许其他用户自行注册。'"
                />
                <div class="mt-8 space-y-5">
                  <div v-if="reusesActiveAdministrator" class="flex items-start gap-3 rounded-md border border-emerald-500/25 bg-emerald-500/8 px-4 py-4 text-sm leading-6 text-emerald-700 dark:text-emerald-300">
                    <CheckCircle2 :size="19" class="mt-0.5 shrink-0" />
                    <div><p class="font-semibold">无需创建新管理员</p><p>初始化完成后，请直接使用数据库中已有的管理员账户登录。</p></div>
                  </div>
                  <template v-else>
                    <FieldWrap label="管理员用户名" :error="errorFor('username')"><input v-model="form.administrator.username" class="ui-input" autocomplete="username" placeholder="admin" /></FieldWrap>
                    <FieldWrap label="显示名称" :error="errorFor('displayName')"><input v-model="form.administrator.displayName" class="ui-input" autocomplete="name" /></FieldWrap>
                    <FieldWrap label="管理员密码" :error="errorFor('password')" hint="6–128 个字符。"><PasswordInput v-model="form.administrator.password" v-model:reveal="reveal.admin" autocomplete="new-password" /></FieldWrap>
                  </template>
                  <ToggleRow v-model="form.registration.enabled" label="开放用户注册" detail="关闭后仅管理员可创建用户。" />
                </div>
              </template>

              <template v-else>
                <StepTitle title="确认并应用配置" :description="reusesActiveAdministrator ? '服务会依次迁移并复用数据库、保存 .env 配置和补齐缺失内容。候选运行验证失败时可修正后重试。' : '服务会依次迁移数据库、保存 .env 配置、创建管理员和音源。候选运行验证失败时可修正后重试。'" />
                <div class="mt-8 divide-y divide-[var(--border)] overflow-hidden rounded-md border border-[var(--border)] bg-[var(--surface)]">
                  <ReviewRow icon="source" label="数据库迁移" :value="form.paths.migrationsDirectory" />
                  <ReviewRow icon="source" label="管理端资源" :value="form.paths.adminWebDirectory" />
                  <ReviewRow icon="database" label="PostgreSQL" :value="`${form.database.host}:${form.database.port}/${form.database.database} · 最大 ${form.database.maxConnections} 个连接`" />
                  <ReviewRow v-if="form.databaseAction" icon="database" label="数据库处理" :value="form.databaseAction === 'reset' ? '全部清除数据库后重新初始化' : form.databaseAction === 'migrate' ? '迁移并复用数据库内所有配置' : '复用数据库内可用的部分配置'" />
                  <ReviewRow icon="storage" label="对象存储" :value="`${form.storage.endpoint} / ${form.storage.bucket}`" />
                  <ReviewRow v-if="form.storageAction" icon="storage" label="Bucket 处理" :value="form.storageAction === 'reset' ? '全部清除 Bucket 后继续' : '继续复用 Bucket 内现有对象'" />
                  <ReviewRow icon="tools" label="FFmpeg" :value="form.media.mode === 'DIRECTORY' ? `自动检测：${form.media.directory || '系统 PATH'}` : `${form.media.ffmpegPath || '系统 PATH'} · ${form.media.ffprobePath || '系统 PATH'}`" />
                  <ReviewRow v-if="fingerprintConfigured" icon="tools" label="音频指纹" :value="`${form.media.fpcalcPath} · AcoustID ${form.media.acoustIdClient}`" />
                  <ReviewRow icon="source" label="音乐音源" :value="`${form.source.name} · ${form.source.directory} · ${form.source.mode}`" />
                  <ReviewRow icon="admin" label="管理员" :value="reusesActiveAdministrator ? '复用数据库中的现有管理员' : `${form.administrator.username} · ${form.administrator.displayName}`" />
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

    <BaseDialog v-if="databaseInspection?.state === 'COMPLETE'" v-model="databaseDecisionOpen" title="可复用所有配置" description="检测到完整的 XyMusic 数据库，数据库内现有配置可以全部复用。" prevent-close width="md">
      <div class="min-w-0 space-y-6 overflow-x-hidden">
        <div class="flex min-w-0 items-start gap-3 border-b border-[var(--border)] pb-5">
          <span class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[var(--primary-soft)] text-[var(--primary)]"><Database :size="18" /></span>
          <div class="min-w-0"><p class="font-semibold">{{ form.database.database }}</p><p class="mt-1 break-words text-sm leading-6 text-[var(--muted)]">活跃管理员、音乐音源及现有业务数据将保持不变，必要的数据库迁移会在初始化时执行。</p></div>
        </div>

        <section class="rounded-md border border-emerald-500/25 bg-emerald-500/8 p-4"><h3 class="flex items-center gap-2 text-sm font-semibold text-emerald-700 dark:text-emerald-300"><CheckCircle2 :size="17" />可复用所有配置</h3><ul class="mt-3 space-y-2 break-words text-sm text-[var(--muted)]"><li v-for="item in databaseInspection.reusable" :key="item">{{ existingItemLabel(item) }}</li><li v-if="!databaseInspection.reusable.length">数据库核心配置完整，可直接复用。</li></ul></section>

        <fieldset class="space-y-3">
          <legend class="mb-3 text-sm font-semibold">是否复用数据库中的所有配置？</legend>
          <button type="button" class="flex min-w-0 w-full items-start gap-3 rounded-md border p-4 text-left transition" :class="databaseDecision === 'migrate' ? 'border-[var(--primary)] bg-[var(--primary-soft)]' : 'border-[var(--border)] hover:border-[var(--border-strong)]'" @click="selectDatabaseDecision('migrate')">
            <span class="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded border" :class="databaseDecision === 'migrate' ? 'border-[var(--primary)] bg-[var(--primary)] text-white' : 'border-[var(--border-strong)]'"><Check v-if="databaseDecision === 'migrate'" :size="13" /></span>
            <span class="min-w-0"><span class="flex flex-wrap items-center gap-2 font-semibold"><span>是，复用所有配置</span><span class="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-700 dark:text-emerald-300">推荐</span></span><span class="mt-1 block break-words text-sm leading-6 text-[var(--muted)]">保留现有管理员、音源及业务数据，并执行必要迁移。</span></span>
          </button>
          <button type="button" class="flex min-w-0 w-full items-start gap-3 rounded-md border p-4 text-left transition" :class="databaseDecision === 'reset' ? 'border-rose-500 bg-rose-500/8' : 'border-[var(--border)] hover:border-rose-500/50'" @click="selectDatabaseDecision('reset')">
            <span class="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded border" :class="databaseDecision === 'reset' ? 'border-rose-500 bg-rose-500 text-white' : 'border-[var(--border-strong)]'"><XCircle v-if="databaseDecision === 'reset'" :size="13" /></span>
            <span class="min-w-0"><span class="flex items-center gap-2 font-semibold text-[var(--danger)]"><Trash2 :size="15" />否，清空数据库</span><span class="mt-1 block break-words text-sm leading-6 text-[var(--muted)]">永久删除当前数据库内全部 XyMusic 业务数据，不会清除 MinIO Bucket。</span></span>
          </button>
        </fieldset>

        <div v-if="databaseDecision === 'reset'" class="min-w-0 border-t border-rose-500/25 pt-5 text-sm">
          <label class="block break-words font-medium text-[var(--text)]">输入数据库名“{{ form.database.database }}”确认清除</label>
          <input v-model="databaseResetConfirmation" class="ui-input mt-2 min-w-0 w-full" autocomplete="off" />
        </div>
      </div>
      <template #footer><AppButton :variant="databaseDecision === 'reset' ? 'danger' : 'primary'" :disabled="(databaseDecision !== 'migrate' && databaseDecision !== 'reset') || (databaseDecision === 'reset' && databaseResetConfirmation !== form.database.database)" @click="confirmDatabaseAction('COMPLETE')">确认并继续</AppButton></template>
    </BaseDialog>

    <BaseDialog v-else-if="databaseInspection?.state === 'PARTIAL'" v-model="databaseDecisionOpen" title="可复用部分配置" description="检测到部分可用的 XyMusic 配置，可选择复用有效内容并在后续步骤补齐缺失项。" prevent-close width="lg">
      <div class="min-w-0 space-y-6 overflow-x-hidden">
        <div class="flex min-w-0 items-start gap-3 border-b border-[var(--border)] pb-5">
          <span class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[var(--primary-soft)] text-[var(--primary)]"><Database :size="18" /></span>
          <div class="min-w-0"><p class="font-semibold">{{ form.database.database }}</p><p class="mt-1 break-words text-sm leading-6 text-[var(--muted)]">仅保留检测到的有效配置和业务数据，缺失配置将在后续步骤补齐。</p></div>
        </div>

        <div class="grid grid-cols-1 gap-5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <section class="min-w-0"><h3 class="flex items-center gap-2 text-sm font-semibold"><CheckCircle2 :size="16" class="text-emerald-500" />可复用配置</h3><ul class="mt-3 space-y-2 break-words text-sm text-[var(--muted)]"><li v-for="item in databaseInspection.reusable" :key="item">{{ existingItemLabel(item) }}</li><li v-if="!databaseInspection.reusable.length">没有可复用的业务配置</li></ul></section>
          <section class="min-w-0"><h3 class="flex items-center gap-2 text-sm font-semibold"><Circle :size="16" class="text-amber-500" />需要补齐</h3><ul class="mt-3 space-y-2 break-words text-sm text-[var(--muted)]"><li v-for="item in databaseInspection.missing" :key="item">{{ existingItemLabel(item) }}</li><li v-if="!databaseInspection.missing.length">无需补齐核心配置</li></ul></section>
        </div>

        <fieldset class="space-y-3">
          <legend class="mb-3 text-sm font-semibold">是否复用检测到的部分配置？</legend>
          <button type="button" class="flex min-w-0 w-full items-start gap-3 rounded-md border p-4 text-left transition" :class="databaseDecision === 'reuse_partial' ? 'border-[var(--primary)] bg-[var(--primary-soft)]' : 'border-[var(--border)] hover:border-[var(--border-strong)]'" @click="selectDatabaseDecision('reuse_partial')">
            <span class="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded border" :class="databaseDecision === 'reuse_partial' ? 'border-[var(--primary)] bg-[var(--primary)] text-white' : 'border-[var(--border-strong)]'"><Check v-if="databaseDecision === 'reuse_partial'" :size="13" /></span>
            <span class="min-w-0"><span class="flex flex-wrap items-center gap-2 font-semibold"><span>是，复用部分配置</span><span class="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-700 dark:text-emerald-300">推荐</span></span><span class="mt-1 block break-words text-sm leading-6 text-[var(--muted)]">保留现有有效内容，仅创建或补齐缺失配置。</span></span>
          </button>
          <button type="button" class="flex min-w-0 w-full items-start gap-3 rounded-md border p-4 text-left transition" :class="databaseDecision === 'reset' ? 'border-rose-500 bg-rose-500/8' : 'border-[var(--border)] hover:border-rose-500/50'" @click="selectDatabaseDecision('reset')">
            <span class="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded border" :class="databaseDecision === 'reset' ? 'border-rose-500 bg-rose-500 text-white' : 'border-[var(--border-strong)]'"><XCircle v-if="databaseDecision === 'reset'" :size="13" /></span>
            <span class="min-w-0"><span class="flex items-center gap-2 font-semibold text-[var(--danger)]"><Trash2 :size="15" />否，清空数据库</span><span class="mt-1 block break-words text-sm leading-6 text-[var(--muted)]">永久删除当前数据库内全部 XyMusic 业务数据，不会清除 MinIO Bucket。</span></span>
          </button>
        </fieldset>

        <div v-if="databaseDecision === 'reset'" class="min-w-0 border-t border-rose-500/25 pt-5 text-sm">
          <label class="block break-words font-medium text-[var(--text)]">输入数据库名“{{ form.database.database }}”确认清除</label>
          <input v-model="databaseResetConfirmation" class="ui-input mt-2 min-w-0 w-full" autocomplete="off" />
        </div>
      </div>
      <template #footer><AppButton :variant="databaseDecision === 'reset' ? 'danger' : 'primary'" :disabled="(databaseDecision !== 'reuse_partial' && databaseDecision !== 'reset') || (databaseDecision === 'reset' && databaseResetConfirmation !== form.database.database)" @click="confirmDatabaseAction('PARTIAL')">确认并继续</AppButton></template>
    </BaseDialog>

    <BaseDialog v-model="storageDecisionOpen" title="选择 Bucket 处理方式" description="检测到当前 MinIO Bucket 已包含对象。" prevent-close width="md">
      <div v-if="storageInspection" class="min-w-0 space-y-6 overflow-x-hidden">
        <div class="flex min-w-0 items-start gap-3 border-b border-[var(--border)] pb-5">
          <span class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[var(--primary-soft)] text-[var(--primary)]"><HardDrive :size="18" /></span>
          <div class="min-w-0"><p class="break-words font-semibold">{{ form.storage.bucket }}</p><p class="mt-1 text-sm text-[var(--muted)]">检测到 {{ storageInspection.objectCount }}{{ storageInspection.countLimited ? '+' : '' }} 个对象。此选择不会影响数据库。</p></div>
        </div>
        <fieldset class="space-y-3">
          <legend class="mb-3 text-sm font-semibold">请选择一种处理方式</legend>
          <button type="button" class="flex min-w-0 w-full items-start gap-3 rounded-md border p-4 text-left transition" :class="storageDecision === 'reuse' ? 'border-[var(--primary)] bg-[var(--primary-soft)]' : 'border-[var(--border)] hover:border-[var(--border-strong)]'" @click="storageDecision = 'reuse'">
            <span class="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-full border" :class="storageDecision === 'reuse' ? 'border-[var(--primary)] bg-[var(--primary)] text-white' : 'border-[var(--border-strong)]'"><Check v-if="storageDecision === 'reuse'" :size="13" /></span>
            <span class="min-w-0"><span class="flex flex-wrap items-center gap-2 font-semibold"><span>继续复用 Bucket</span><span class="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-700 dark:text-emerald-300">推荐</span></span><span class="mt-1 block break-words text-sm leading-6 text-[var(--muted)]">保留 Bucket 中的全部现有对象，初始化只写入后续新增内容。</span></span>
          </button>
          <button type="button" class="flex min-w-0 w-full items-start gap-3 rounded-md border p-4 text-left transition" :class="storageDecision === 'reset' ? 'border-rose-500 bg-rose-500/8' : 'border-[var(--border)] hover:border-rose-500/50'" @click="storageDecision = 'reset'">
            <span class="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-full border" :class="storageDecision === 'reset' ? 'border-rose-500 bg-rose-500 text-white' : 'border-[var(--border-strong)]'"><Check v-if="storageDecision === 'reset'" :size="13" /></span>
            <span class="min-w-0"><span class="flex items-center gap-2 font-semibold text-[var(--danger)]"><Trash2 :size="15" />全部清除 Bucket</span><span class="mt-1 block break-words text-sm leading-6 text-[var(--muted)]">永久删除 Bucket 中的全部对象，不会修改或清除数据库。</span></span>
          </button>
        </fieldset>
        <div v-if="storageDecision === 'reset'" class="min-w-0 border-t border-rose-500/25 pt-5 text-sm">
          <label class="block break-words font-medium text-[var(--text)]">输入 Bucket 名“{{ form.storage.bucket }}”确认清除</label>
          <input v-model="storageResetConfirmation" class="ui-input mt-2 min-w-0 w-full" autocomplete="off" />
        </div>
      </div>
      <template #footer><AppButton :variant="storageDecision === 'reset' ? 'danger' : 'primary'" :disabled="!storageDecision || (storageDecision === 'reset' && storageResetConfirmation !== form.storage.bucket)" @click="confirmStorageAction">确认并继续</AppButton></template>
    </BaseDialog>
  </main>
</template>
