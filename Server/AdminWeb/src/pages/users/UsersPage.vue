<script setup lang="ts">
import { Camera, KeyRound, Laptop, Pencil, Plus, RefreshCw, RotateCcw, Search, Shield, Trash2, UserRound, X } from "lucide-vue-next";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { refDebounced } from "@vueuse/core";
import { computed, reactive, ref, watch } from "vue";
import { z } from "zod";
import { ApiError, apiErrorMessage } from "@/shared/application/api-error";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import ArtworkUploadField from "@/components/ArtworkUploadField.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import type { CreateUserInput, UpdateUserInput, UserRole, UserSessionSummary, UserStatus, UserSummary } from "@/features/users/domain/models";
import { useUserAdmin } from "@/app/services/users";
import { useAuthStore } from "@/stores/auth";
import { useUiStore } from "@/stores/ui";
import { formatDate, formatRelative } from "@/utils/format";

const queryClient = useQueryClient();
const auth = useAuthStore();
const ui = useUiStore();
const userAdmin = useUserAdmin();
const search = ref("");
const debouncedSearch = refDebounced(search, 300);
const status = ref("");
const role = ref("");
const page = ref(1);
const pageSize = ref(20);
const sessionPage = ref(1);
const sessionPageSize = ref(10);
const editorOpen = ref(false);
const detailOpen = ref(false);
const avatarOpen = ref(false);
const passwordOpen = ref(false);
const confirmOpen = ref(false);
const sessionOpen = ref(false);
const confirmAction = ref<"delete" | "restore">("delete");
const selected = ref<UserSummary>();
const selectedSession = ref<UserSessionSummary>();
const fieldErrors = ref<Record<string, string>>({});
const actionError = ref("");
const form = reactive({ id: "", username: "", displayName: "", bio: "", role: "USER" as UserRole, status: "ACTIVE" as UserStatus, password: "", reason: "", version: 0 });
const passwordForm = reactive({ password: "", reason: "" });
const operationReason = ref("");
let allowEditorClose = false;
let allowPasswordClose = false;
let allowConfirmClose = false;
let allowSessionClose = false;

const usersQuery = useQuery({
  queryKey: computed(() => ["admin", "users", { page: page.value, pageSize: pageSize.value, query: debouncedSearch.value, status: status.value, role: role.value }]),
  queryFn: ({ signal }) => userAdmin.list({ page: page.value, pageSize: pageSize.value, search: debouncedSearch.value, status: status.value, role: role.value }, signal),
  placeholderData: keepPreviousData,
});
const detailQuery = useQuery({
  queryKey: computed(() => ["admin", "users", selected.value?.id, "sessions", sessionPage.value, sessionPageSize.value]),
  queryFn: ({ signal }) => userAdmin.detail(selected.value!.id, sessionPage.value, sessionPageSize.value, signal),
  enabled: computed(() => detailOpen.value && Boolean(selected.value)),
});

function resetFilters(): void { search.value = ""; status.value = ""; role.value = ""; page.value = 1; }
function changePageSize(value: number): void { pageSize.value = value; page.value = 1; }
function changeSessionPageSize(value: number): void { sessionPageSize.value = value; sessionPage.value = 1; }
function openCreate(): void {
  Object.assign(form, { id: "", username: "", displayName: "", bio: "", role: "USER", status: "ACTIVE", password: "", reason: "", version: 0 });
  fieldErrors.value = {}; actionError.value = ""; editorOpen.value = true;
}
function openEdit(user: UserSummary): void {
  selected.value = user;
  Object.assign(form, { id: user.id, username: user.username, displayName: user.displayName, bio: user.bio ?? "", role: user.role, status: user.status, password: "", reason: "", version: user.version });
  fieldErrors.value = {}; actionError.value = ""; editorOpen.value = true;
}
function openDetail(user: UserSummary): void { selected.value = user; sessionPage.value = 1; detailOpen.value = true; }
function openAvatar(user: UserSummary): void { selected.value = user; avatarOpen.value = true; }
function openPassword(user: UserSummary): void { selected.value = user; Object.assign(passwordForm, { password: "", reason: "" }); actionError.value = ""; passwordOpen.value = true; }
function askUserAction(user: UserSummary, action: "delete" | "restore"): void { selected.value = user; confirmAction.value = action; operationReason.value = ""; actionError.value = ""; confirmOpen.value = true; }
function askRevoke(session: UserSessionSummary): void { selectedSession.value = session; operationReason.value = ""; actionError.value = ""; sessionOpen.value = true; }

const userSchema = z.object({
  username: z.string().trim().regex(/^[A-Za-z0-9_]{3,32}$/, "用户名须为 3–32 位字母、数字或下划线"),
  displayName: z.string().trim().min(1, "请输入显示名称").max(100),
  bio: z.string().max(500, "简介最多 500 个字符"),
  role: z.enum(["ADMIN", "USER"]),
  status: z.enum(["ACTIVE", "SUSPENDED", "DELETED"]),
});
function validateEditor(): boolean {
  const result = userSchema.safeParse(form);
  fieldErrors.value = {};
  if (!result.success) for (const issue of result.error.issues) fieldErrors.value[issue.path.join(".")] = issue.message;
  if (!form.id && form.password.length < 6) fieldErrors.value.password = "初始密码至少 6 个字符";
  if (form.id && !form.reason.trim()) fieldErrors.value.reason = "请填写本次修改原因";
  return Object.keys(fieldErrors.value).length === 0;
}

async function refreshUsers(): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "users"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "audit"] }),
  ]);
}
async function avatarCompleted(): Promise<void> {
  const userId = selected.value?.id;
  if (!userId) return;
  const [usersResult, detailResult] = await Promise.all([
    usersQuery.refetch(),
    detailOpen.value ? detailQuery.refetch() : Promise.resolve(undefined),
  ]);
  const updated = detailResult?.data ?? usersResult.data?.items.find((user) => user.id === userId);
  if (updated) selected.value = updated;
  if (userId === auth.profile?.id) await auth.ensureSession(true);
  ui.notify("success", "用户头像已更新");
}
const saveMutation = useMutation({
  mutationFn: async () => {
    if (!form.id) {
      const input: CreateUserInput = { username: form.username.trim(), displayName: form.displayName.trim(), role: form.role, password: form.password };
      return userAdmin.create(input);
    }
    const original = selected.value!;
    const input: UpdateUserInput = { expectedVersion: form.version, reason: form.reason.trim() };
    if (form.username.trim() !== original.username) input.username = form.username.trim();
    if (form.displayName.trim() !== original.displayName) input.displayName = form.displayName.trim();
    if (form.bio.trim() !== (original.bio ?? "")) input.bio = form.bio.trim() || null;
    if (form.role !== original.role) input.role = form.role;
    if (form.status !== original.status) input.status = form.status;
    if (Object.keys(input).length === 2) throw new Error("没有需要保存的用户字段");
    return userAdmin.update(form.id, input);
  },
  onSuccess: async (saved) => {
    allowEditorClose = true;
    editorOpen.value = false;
    ui.notify("success", form.id ? "用户信息已更新" : "用户已创建");
    await refreshUsers();
    if (saved.id === auth.profile?.id) await auth.ensureSession(true);
  },
  onError: (error) => { actionError.value = error instanceof Error ? error.message : "保存用户失败"; },
});
function save(): void { if (validateEditor()) { actionError.value = ""; saveMutation.mutate(); } }

const passwordMutation = useMutation({
  mutationFn: () => userAdmin.resetPassword(selected.value!.id, selected.value!.version, passwordForm.password, passwordForm.reason.trim()),
  onSuccess: async () => { allowPasswordClose = true; passwordOpen.value = false; ui.notify("success", "用户密码已重置", "该用户的已有会话已被撤销"); await refreshUsers(); },
  onError: (error) => { actionError.value = error instanceof ApiError ? error.message : "密码重置失败"; },
});
function resetPassword(): void {
  actionError.value = passwordForm.password.length < 6 ? "新密码至少 6 个字符" : !passwordForm.reason.trim() ? "请填写重置原因" : "";
  if (!actionError.value) passwordMutation.mutate();
}
const confirmMutation = useMutation({
  mutationFn: async () => {
    if (!operationReason.value.trim()) throw new Error("请填写操作原因");
    await userAdmin.setDeleted(
      selected.value!.id,
      selected.value!.version,
      confirmAction.value === "delete",
      operationReason.value.trim(),
    );
  },
  onSuccess: async () => { allowConfirmClose = true; confirmOpen.value = false; ui.notify("success", confirmAction.value === "delete" ? "用户已删除" : "用户已恢复"); await refreshUsers(); },
  onError: (error) => { actionError.value = error instanceof Error ? error.message : "操作失败"; },
});
const sessionMutation = useMutation({
  mutationFn: () => {
    if (!operationReason.value.trim()) throw new Error("请填写撤销原因");
    return userAdmin.revokeSession(selected.value!.id, selectedSession.value!.id, operationReason.value.trim());
  },
  onSuccess: async () => { allowSessionClose = true; sessionOpen.value = false; ui.notify("success", "会话已撤销"); await detailQuery.refetch(); },
  onError: (error) => { actionError.value = error instanceof Error ? error.message : "撤销会话失败"; },
});
watch(editorOpen, (value) => { if (!value && saveMutation.isPending.value && !allowEditorClose) editorOpen.value = true; allowEditorClose = false; });
watch(passwordOpen, (value) => { if (!value && passwordMutation.isPending.value && !allowPasswordClose) passwordOpen.value = true; allowPasswordClose = false; });
watch(confirmOpen, (value) => { if (!value && confirmMutation.isPending.value && !allowConfirmClose) confirmOpen.value = true; allowConfirmClose = false; });
watch(sessionOpen, (value) => { if (!value && sessionMutation.isPending.value && !allowSessionClose) sessionOpen.value = true; allowSessionClose = false; });
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="用户管理" description="管理用户名、角色、状态与登录会话。"><template #eyebrow>身份与访问</template><template #actions><AppButton variant="primary" @click="openCreate"><template #icon><Plus :size="16" /></template>创建用户</AppButton></template></PageHeader>
    <section class="ui-card overflow-hidden">
      <div class="flex flex-col gap-3 border-b border-[var(--border)] p-4 lg:flex-row"><div class="relative flex-1"><Search :size="16" class="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" /><input v-model="search" class="ui-input !pl-10" type="search" placeholder="搜索用户名或显示名称" @input="page = 1" /></div><select v-model="status" class="ui-select lg:w-40" @change="page = 1"><option value="">全部状态</option><option value="ACTIVE">正常</option><option value="SUSPENDED">已停用</option><option value="DELETED">已删除</option></select><select v-model="role" class="ui-select lg:w-36" @change="page = 1"><option value="">全部角色</option><option value="ADMIN">管理员</option><option value="USER">普通用户</option></select><AppButton variant="ghost" :disabled="!search && !status && !role" @click="resetFilters"><template #icon><X :size="15" /></template>清除</AppButton><AppButton icon-only :loading="usersQuery.isFetching.value" @click="usersQuery.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></div>
      <StatePanel v-if="usersQuery.isPending.value" state="loading" /><StatePanel v-else-if="usersQuery.isError.value" state="error" :detail="apiErrorMessage(usersQuery.error.value, '无法读取用户列表。')" @retry="usersQuery.refetch()" /><StatePanel v-else-if="!usersQuery.data.value?.items.length" state="empty" title="没有符合条件的用户" />
      <template v-else><div class="overflow-x-auto"><table class="data-table min-w-[800px]"><thead><tr><th>用户</th><th>角色</th><th>状态</th><th>最近更新</th><th>创建时间</th><th>操作</th></tr></thead><tbody><tr v-for="user in usersQuery.data.value.items" :key="user.id" class="cursor-pointer" tabindex="0" :aria-label="`查看用户：${user.displayName}`" @click="openDetail(user)" @keydown.enter="openDetail(user)" @keydown.space.prevent="openDetail(user)"><td><div class="flex items-center gap-3"><span class="grid h-10 w-10 shrink-0 place-items-center overflow-hidden rounded-full bg-[var(--primary-soft)] text-sm font-extrabold text-[var(--primary)]"><img v-if="user.avatar" :src="user.avatar.url" :alt="`${user.displayName}的头像`" class="h-full w-full object-cover" width="40" height="40" loading="lazy" decoding="async" /><span v-else>{{ (user.displayName || user.username).slice(0, 2).toUpperCase() }}</span></span><div><p class="font-semibold">{{ user.displayName }}</p><p class="mt-0.5 text-xs text-[var(--muted)]">@{{ user.username }}</p></div></div></td><td><span class="inline-flex items-center gap-1.5 font-semibold"><Shield v-if="user.role === 'ADMIN'" :size="14" class="text-[var(--primary)]" /><UserRound v-else :size="14" />{{ user.role === 'ADMIN' ? '管理员' : '普通用户' }}</span></td><td><StatusBadge :status="user.status" dot /></td><td class="text-xs text-[var(--muted)]">{{ formatDate(user.updatedAt) }}</td><td class="text-xs text-[var(--muted)]">{{ formatDate(user.createdAt) }}</td><td @click.stop @keydown.stop><div class="flex gap-1"><button class="btn btn-ghost btn-icon" type="button" :aria-label="`修改用户头像：${user.displayName}`" @click="openAvatar(user)"><Camera :size="15" /></button><button class="btn btn-ghost btn-icon" type="button" :aria-label="`编辑用户：${user.displayName}`" @click="openEdit(user)"><Pencil :size="15" /></button><button class="btn btn-ghost btn-icon" type="button" :aria-label="`重置密码：${user.displayName}`" @click="openPassword(user)"><KeyRound :size="15" /></button><button v-if="user.status === 'DELETED'" class="btn btn-ghost btn-icon" type="button" :aria-label="`恢复用户：${user.displayName}`" @click="askUserAction(user, 'restore')"><RotateCcw :size="15" /></button><button v-else class="btn btn-ghost btn-icon text-[var(--danger)]" type="button" :aria-label="`删除用户：${user.displayName}`" :disabled="user.id === auth.profile?.id" @click="askUserAction(user, 'delete')"><Trash2 :size="15" /></button></div></td></tr></tbody></table></div><AppPagination :page="page" :page-size="pageSize" :total="usersQuery.data.value.total" @change="page = $event" @page-size-change="changePageSize" /></template>
    </section>

    <BaseDialog v-model="editorOpen" :title="form.id ? '编辑用户' : '创建用户'" :description="form.id ? '修改内容会记录操作原因。' : '创建可立即登录的新账户。'">
      <div class="space-y-5"><div><label class="ui-label">用户名</label><input v-model="form.username" class="ui-input" autocomplete="username" /><p v-if="fieldErrors.username" class="ui-error">{{ fieldErrors.username }}</p></div><div><label class="ui-label">显示名称</label><input v-model="form.displayName" class="ui-input" /><p v-if="fieldErrors.displayName" class="ui-error">{{ fieldErrors.displayName }}</p></div><div><label class="ui-label">个人简介</label><textarea v-model="form.bio" class="ui-textarea" /><p v-if="fieldErrors.bio" class="ui-error">{{ fieldErrors.bio }}</p></div><div class="grid gap-5 sm:grid-cols-2"><div><label class="ui-label">角色</label><select v-model="form.role" class="ui-select"><option value="USER">普通用户</option><option value="ADMIN">管理员</option></select></div><div v-if="form.id"><label class="ui-label">状态</label><select v-model="form.status" class="ui-select"><option value="ACTIVE">正常</option><option value="SUSPENDED">停用</option><option value="DELETED">删除</option></select></div></div><div v-if="!form.id"><label class="ui-label">初始密码</label><input v-model="form.password" class="ui-input" type="password" autocomplete="new-password" /><p v-if="fieldErrors.password" class="ui-error">{{ fieldErrors.password }}</p></div><div v-else><label class="ui-label">修改原因</label><input v-model="form.reason" class="ui-input" placeholder="例如：根据用户申请更新资料" /><p v-if="fieldErrors.reason" class="ui-error">{{ fieldErrors.reason }}</p></div><p v-if="actionError" class="rounded-xl bg-rose-500/10 px-4 py-3 text-sm text-[var(--danger)]">{{ actionError }}</p></div>
      <template #footer><AppButton @click="editorOpen = false">取消</AppButton><AppButton variant="primary" :loading="saveMutation.isPending.value" @click="save">保存</AppButton></template>
    </BaseDialog>

    <BaseDialog v-model="detailOpen" title="用户详情" description="账户资料与登录会话。" side="right">
      <StatePanel v-if="detailQuery.isPending.value" state="loading" compact /><StatePanel v-else-if="detailQuery.isError.value" state="error" compact :detail="apiErrorMessage(detailQuery.error.value, '无法读取用户详情。')" @retry="detailQuery.refetch()" />
      <template v-else-if="detailQuery.data.value"><div class="flex items-center gap-4 rounded-2xl bg-[var(--surface-muted)] p-4"><span class="grid h-12 w-12 shrink-0 place-items-center overflow-hidden rounded-full bg-[var(--primary-soft)] font-extrabold text-[var(--primary)]"><img v-if="detailQuery.data.value.avatar" :src="detailQuery.data.value.avatar.url" :alt="`${detailQuery.data.value.displayName}的头像`" class="h-full w-full object-cover" width="48" height="48" decoding="async" /><span v-else>{{ detailQuery.data.value.displayName.slice(0, 2).toUpperCase() }}</span></span><div class="min-w-0 flex-1"><p class="truncate text-lg font-bold">{{ detailQuery.data.value.displayName }}</p><p class="truncate text-sm text-[var(--muted)]">@{{ detailQuery.data.value.username }}</p></div><button class="btn btn-ghost btn-icon" type="button" aria-label="修改用户头像" @click="openAvatar(detailQuery.data.value)"><Camera :size="15" /></button><StatusBadge :status="detailQuery.data.value.status" /></div><p v-if="detailQuery.data.value.bio" class="mt-4 text-sm leading-6 text-[var(--muted)]">{{ detailQuery.data.value.bio }}</p><h3 class="mt-6 font-bold">登录会话</h3><div v-if="detailQuery.data.value.sessions.length" class="mt-3 space-y-3"><article v-for="session in detailQuery.data.value.sessions" :key="session.id" class="rounded-xl border border-[var(--border)] p-4"><div class="flex items-start gap-3"><Laptop :size="18" class="mt-0.5 text-[var(--primary)]" /><div class="min-w-0 flex-1"><p class="font-semibold">{{ session.deviceName }}</p><p class="mt-1 text-xs text-[var(--muted)]">{{ session.platform }} · {{ session.appVersion }}</p><p class="mt-2 text-xs text-[var(--muted)]">最后活动 {{ formatRelative(session.lastSeenAt) }}</p></div><StatusBadge :status="session.active ? 'ACTIVE' : 'DELETED'" :label="session.active ? '有效' : '已撤销'" /></div><button v-if="session.active" class="btn btn-danger mt-3 w-full" type="button" @click="askRevoke(session)">撤销此会话</button></article><AppPagination :page="sessionPage" :page-size="sessionPageSize" :total="detailQuery.data.value.sessionTotal" :total-pages="detailQuery.data.value.sessionTotalPages" @change="sessionPage = $event" @page-size-change="changeSessionPageSize" /></div><StatePanel v-else state="empty" compact title="没有登录会话" /></template>
    </BaseDialog>

    <BaseDialog v-model="avatarOpen" title="用户头像" :description="selected ? `为 ${selected.displayName} 上传或更换头像。` : ''">
      <div v-if="selected" class="flex flex-col items-center gap-4 py-3">
        <ArtworkUploadField :target-id="selected.id" purpose="USER_AVATAR" :image-url="selected.avatar?.url" :alt="`${selected.displayName}的头像`" noun="头像" shape="circle" @completed="avatarCompleted">
          <span class="text-3xl font-extrabold">{{ (selected.displayName || selected.username).slice(0, 2).toUpperCase() }}</span>
        </ArtworkUploadField>
        <p class="max-w-sm text-center text-xs leading-5 text-[var(--muted)]">支持 JPEG、PNG、WebP；上传完成后用户列表和当前会话会立即刷新。</p>
      </div>
      <template #footer><AppButton @click="avatarOpen = false">关闭</AppButton></template>
    </BaseDialog>

    <BaseDialog v-model="passwordOpen" title="重置用户密码" :description="`为 ${selected?.displayName ?? ''} 设置新密码；保存后会撤销其所有会话。`"><div class="space-y-4"><div><label class="ui-label">新密码</label><input v-model="passwordForm.password" class="ui-input" type="password" autocomplete="new-password" /></div><div><label class="ui-label">重置原因</label><input v-model="passwordForm.reason" class="ui-input" /></div><p v-if="actionError" class="rounded-xl bg-rose-500/10 px-4 py-3 text-sm text-[var(--danger)]">{{ actionError }}</p></div><template #footer><AppButton @click="passwordOpen = false">取消</AppButton><AppButton variant="primary" :loading="passwordMutation.isPending.value" @click="resetPassword">重置密码</AppButton></template></BaseDialog>

    <BaseDialog v-model="confirmOpen" :title="confirmAction === 'delete' ? '删除用户' : '恢复用户'" :description="confirmAction === 'delete' ? '用户将标记为已删除，所有会话会被撤销。' : '用户将恢复为正常状态。'"><div class="rounded-xl bg-[var(--surface-muted)] p-4"><p class="font-semibold">{{ selected?.displayName }}</p><p class="text-xs text-[var(--muted)]">@{{ selected?.username }}</p></div><div class="mt-4"><label class="ui-label">操作原因</label><input v-model="operationReason" class="ui-input" /></div><p v-if="actionError" class="mt-4 rounded-xl bg-rose-500/10 px-4 py-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton @click="confirmOpen = false">取消</AppButton><AppButton :variant="confirmAction === 'delete' ? 'danger' : 'primary'" :loading="confirmMutation.isPending.value" @click="confirmMutation.mutate()">{{ confirmAction === 'delete' ? '删除用户' : '恢复用户' }}</AppButton></template></BaseDialog>

    <BaseDialog v-model="sessionOpen" title="撤销登录会话" :description="selectedSession?.deviceName"><div><label class="ui-label">撤销原因</label><input v-model="operationReason" class="ui-input" placeholder="例如：设备已遗失" /></div><p v-if="actionError" class="mt-4 rounded-xl bg-rose-500/10 px-4 py-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton @click="sessionOpen = false">取消</AppButton><AppButton variant="danger" :loading="sessionMutation.isPending.value" @click="sessionMutation.mutate()">撤销会话</AppButton></template></BaseDialog>
  </div>
</template>
