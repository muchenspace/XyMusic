<script setup lang="ts">
import { AlertTriangle, Archive, Check, Disc3, FileAudio, ListFilter, Pencil, RefreshCw, RotateCcw, Save, Search, Sparkles, Tags, Trash2, Upload, X } from "lucide-vue-next";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { refDebounced } from "@vueuse/core";
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { onBeforeRouteLeave, useRoute } from "vue-router";
import { ApiError } from "@/shared/application/api-error";
import { trackListAudioRefetchInterval } from "@/shared/application/audio-status-refresh";
import AppButton from "@/components/AppButton.vue";
import AppPagination from "@/components/AppPagination.vue";
import AudioStatusBadge from "@/components/AudioStatusBadge.vue";
import BaseDialog from "@/components/BaseDialog.vue";
import BatchTagScrapeDialog from "@/components/BatchTagScrapeDialog.vue";
import PageHeader from "@/components/PageHeader.vue";
import StatePanel from "@/components/StatePanel.vue";
import StatusBadge from "@/components/StatusBadge.vue";
import TagScrapeDialog from "@/components/TagScrapeDialog.vue";
import TrackStatusDisc from "@/components/TrackStatusDisc.vue";
import type { CreditRole, PermanentDeleteTrackJobItem, PermanentDeleteTracksJob, TrackMetadataRecord, TrackSummary, TrackTagRevision, TrackTagValues } from "@/features/music/domain/models";
import { useMusicAdmin } from "@/app/services/music";
import { normalizeTrackTagScalars } from "@/features/music/presentation/track-tag-form";
import { assertWritebackAllowed, sourceWritebackCapability, writebackBlockedMessage } from "@/features/music/presentation/writeback-capability";
import { useUiStore } from "@/stores/ui";
import { formatDate, formatDuration } from "@/utils/format";

type EditorTab = "metadata" | "lyrics" | "original" | "history";
type EditableField = Exclude<keyof TrackTagValues, "hasArtwork">;
type TrackStateAction = "publish" | "archive" | "restore";
type TrackSelectionKind = "ACTIVE" | "ARCHIVED" | "MIXED" | null;
interface TagForm { title: string; primary: string; albumArtists: string; featured: string; composers: string; lyricists: string; producers: string; album: string; releaseDate: string; trackNumber: string; trackTotal: string; discNumber: string; discTotal: string; genres: string; bpm: string; isrc: string; copyright: string; comment: string; lyrics: string; lyricsFormat: "PLAIN" | "LRC"; lyricsLanguage: string; reason: string }

const route = useRoute();
const queryClient = useQueryClient();
const ui = useUiStore();
const musicAdmin = useMusicAdmin();
const search = ref(typeof route.query.search === "string" ? route.query.search : "");
const debouncedSearch = refDebounced(search, 300);
const status = ref("READY");
const metadataStatus = ref("");
const page = ref(1);
const pageSize = ref(25);
const selectedTracks = ref(new Map<string, TrackSummary>());
const selectedIds = computed(() => new Set(selectedTracks.value.keys()));
const selectedTrack = ref<TrackSummary>();
const editorOpen = ref(false);
const editorTab = ref<EditorTab>("metadata");
const writeBackAfterSave = ref(false);
const savingMetadata = ref(false);
const bulkOpen = ref(false);
const scrapeOpen = ref(false);
const batchScrapeOpen = ref(false);
const restoreOpen = ref(false);
const archiveOpen = ref(false);
const batchRestoreOpen = ref(false);
const permanentDeleteOpen = ref(false);
const deletionTrack = ref<TrackSummary>();
const batchRestoreTargets = ref<TrackSummary[]>([]);
const permanentDeleteTargets = ref<TrackSummary[]>([]);
const permanentDeleteConfirmation = ref("");
const permanentDeleteJob = ref<PermanentDeleteTracksJob>();
const batchRestoreError = ref("");
const permanentDeleteError = ref("");
const revisionToRestore = ref<TrackTagRevision>();
const restoreReason = ref("");
const actionError = ref("");
const resetAllOverrides = ref(false);
const editorDirty = ref(false);
const remoteChanged = ref(false);
const historyPage = ref(1);
const historyPageSize = ref(20);
let populating = false;
let allowArchiveClose = false;
let allowBulkClose = false;
let allowRestoreClose = false;
const bulk = reactive({ primary: "", albumArtists: "", album: "", genres: "", comment: "", reason: "" });
const tags = reactive<TagForm>({ title: "", primary: "", albumArtists: "", featured: "", composers: "", lyricists: "", producers: "", album: "", releaseDate: "", trackNumber: "", trackTotal: "", discNumber: "", discTotal: "", genres: "", bpm: "", isrc: "", copyright: "", comment: "", lyrics: "", lyricsFormat: "PLAIN", lyricsLanguage: "und", reason: "" });

const tracksQuery = useQuery({
  queryKey: computed(() => ["admin", "tracks", { page: page.value, pageSize: pageSize.value, search: debouncedSearch.value, status: status.value, metadataStatus: metadataStatus.value }]),
  queryFn: ({ signal }) => musicAdmin.listTracks({ page: page.value, pageSize: pageSize.value, search: debouncedSearch.value, status: status.value, metadataStatus: metadataStatus.value, sort: "updatedAt", order: "desc" }, signal),
  placeholderData: keepPreviousData,
  refetchInterval: (state) => trackListAudioRefetchInterval(state.state.data),
});
const permanentDeleteJobId = computed(() => permanentDeleteJob.value?.id ?? "");
const permanentDeleteJobQuery = useQuery({
  queryKey: computed(() => ["admin", "tracks", "permanent-delete", permanentDeleteJobId.value]),
  queryFn: ({ signal }) => musicAdmin.getPermanentDeleteTracksJob(permanentDeleteJobId.value, signal),
  enabled: computed(() => permanentDeleteOpen.value && Boolean(permanentDeleteJobId.value)),
  staleTime: 0,
  refetchInterval: (state) => !state.state.data || deleteJobStatusActive(state.state.data.status) ? 1_000 : false,
});
const metadataQuery = useQuery({ queryKey: computed(() => ["admin", "track", selectedTrack.value?.id, "metadata"]), queryFn: ({ signal }) => musicAdmin.getTrackMetadata(selectedTrack.value!.id, signal), enabled: computed(() => editorOpen.value && Boolean(selectedTrack.value)) });
const historyQuery = useQuery({
  queryKey: computed(() => ["admin", "track", selectedTrack.value?.id, "metadata", "revisions", historyPage.value, historyPageSize.value]),
  queryFn: ({ signal }) => musicAdmin.listTagHistory(selectedTrack.value!.id, historyPage.value, historyPageSize.value, signal),
  enabled: computed(() => editorOpen.value && editorTab.value === "history" && Boolean(selectedTrack.value)),
  placeholderData: (previousData, previousQuery) => previousQuery?.queryKey[2] === selectedTrack.value?.id
    ? keepPreviousData(previousData)
    : undefined,
});
const editorWritebackCapability = computed(() => sourceWritebackCapability(metadataQuery.data.value?.source));

function nameList(value: string): string[] { return value.split(/[;\n]/).map((item) => item.trim()).filter(Boolean); }
function genreList(value: string): string[] { return value.split(/[,，;\n]/).map((item) => item.trim()).filter(Boolean); }
function credits(value: TrackTagValues, role: CreditRole): string { return value.credits.filter((credit) => credit.role === role).map((credit) => credit.name).join("; "); }
function populate(record: TrackMetadataRecord): void {
  populating = true;
  const value = record.effective;
  Object.assign(tags, { title: value.title, primary: credits(value, "PRIMARY"), albumArtists: value.albumArtists.join("; "), featured: credits(value, "FEATURED"), composers: credits(value, "COMPOSER"), lyricists: credits(value, "LYRICIST"), producers: credits(value, "PRODUCER"), album: value.album ?? "", releaseDate: value.releaseDate ?? "", trackNumber: value.trackNumber?.toString() ?? "", trackTotal: value.trackTotal?.toString() ?? "", discNumber: value.discNumber?.toString() ?? "", discTotal: value.discTotal?.toString() ?? "", genres: value.genres.join(", "), bpm: value.bpm?.toString() ?? "", isrc: value.isrc ?? "", copyright: value.copyright ?? "", comment: value.comment ?? "", lyrics: value.lyrics?.content ?? "", lyricsFormat: value.lyrics?.format ?? "PLAIN", lyricsLanguage: value.lyrics?.language ?? "und", reason: "" });
  resetAllOverrides.value = false;
  editorDirty.value = false;
  remoteChanged.value = false;
  queueMicrotask(() => { populating = false; });
}
watch(() => metadataQuery.data.value, (value) => {
  if (!value) return;
  if (editorDirty.value) {
    if (!remoteChanged.value) ui.notify("warning", "曲目远端数据已变化", "未保存编辑已保留；保存时会进行版本冲突校验。");
    remoteChanged.value = true;
  } else populate(value);
});
watch(tags, () => { if (editorOpen.value && !populating) editorDirty.value = true; }, { deep: true });
watch(resetAllOverrides, () => { if (editorOpen.value && !populating) editorDirty.value = true; });
watch(() => editorWritebackCapability.value.canWriteBack, (canWriteBack) => {
  if (!canWriteBack) writeBackAfterSave.value = false;
}, { immediate: true });
watch(editorOpen, (value, previous) => {
  if (!value) {
    editorDirty.value = false;
    remoteChanged.value = false;
  }
});
watch(() => route.query.search, (value) => {
  const next = typeof value === "string" ? value : "";
  if (search.value !== next) { search.value = next; page.value = 1; }
});
function desired(): TrackTagValues {
  const makeCredits = (value: string, role: CreditRole) => nameList(value).map((name) => ({ name, role }));
  const scalars = normalizeTrackTagScalars(tags);
  return { title: tags.title.trim(), credits: [...makeCredits(tags.primary, "PRIMARY"), ...makeCredits(tags.featured, "FEATURED"), ...makeCredits(tags.composers, "COMPOSER"), ...makeCredits(tags.lyricists, "LYRICIST"), ...makeCredits(tags.producers, "PRODUCER")], albumArtists: nameList(tags.albumArtists), album: tags.album.trim() || null, releaseDate: scalars.releaseDate, trackNumber: scalars.trackNumber, trackTotal: scalars.trackTotal, discNumber: scalars.discNumber, discTotal: scalars.discTotal, genres: genreList(tags.genres), bpm: scalars.bpm, isrc: scalars.isrc, copyright: tags.copyright.trim() || null, comment: tags.comment.trim() || null, lyrics: tags.lyrics.trim() ? { content: tags.lyrics, format: tags.lyricsFormat, language: scalars.lyricsLanguage } : null, hasArtwork: metadataQuery.data.value?.effective.hasArtwork ?? false };
}
function changedPatch(record: TrackMetadataRecord): Partial<Omit<TrackTagValues, "hasArtwork">> {
  const next = desired();
  const patch: Partial<Omit<TrackTagValues, "hasArtwork">> = {};
  const fields: EditableField[] = ["title", "credits", "albumArtists", "album", "releaseDate", "trackNumber", "trackTotal", "discNumber", "discTotal", "genres", "bpm", "isrc", "comment", "copyright", "lyrics"];
  for (const field of fields) if (JSON.stringify(next[field]) !== JSON.stringify(record.effective[field])) Object.assign(patch, { [field]: next[field] });
  return patch;
}

function edit(track: TrackSummary): void {
  if (track.status === "ARCHIVED") {
    ui.notify("warning", "已归档曲目需先恢复", `恢复“${track.title}”后才能编辑 Tag。`);
    return;
  }
  selectedTrack.value = track;
  editorTab.value = "metadata";
  historyPage.value = 1;
  writeBackAfterSave.value = false;
  editorDirty.value = false;
  remoteChanged.value = false;
  actionError.value = "";
  const cached = queryClient.getQueryData<TrackMetadataRecord>(["admin", "track", track.id, "metadata"]);
  if (cached?.trackId === track.id) populate(cached);
  editorOpen.value = true;
}
function askArchive(track: TrackSummary): void { deletionTrack.value = track; actionError.value = ""; archiveOpen.value = true; }
function askPermanentDelete(track: TrackSummary): void { openPermanentDelete([track]); }
function clearFilters(): void { search.value = ""; status.value = "READY"; metadataStatus.value = ""; page.value = 1; }
function changePageSize(value: number): void { pageSize.value = value; page.value = 1; }
function changeHistoryPageSize(value: number): void { historyPageSize.value = value; historyPage.value = 1; }
function clearSelection(): void { selectedTracks.value = new Map(); }
function removeSelected(trackIds: Iterable<string>): void {
  const next = new Map(selectedTracks.value);
  for (const trackId of trackIds) next.delete(trackId);
  selectedTracks.value = next;
}
function trackSelectionKind(track: TrackSummary): Exclude<TrackSelectionKind, "MIXED" | null> {
  return track.status === "ARCHIVED" ? "ARCHIVED" : "ACTIVE";
}
const selectionKind = computed<TrackSelectionKind>(() => {
  let active = false;
  let archived = false;
  for (const track of selectedTracks.value.values()) {
    if (track.status === "ARCHIVED") archived = true;
    else active = true;
  }
  if (active && archived) return "MIXED";
  if (archived) return "ARCHIVED";
  if (active) return "ACTIVE";
  return null;
});
const selectionScope = computed<"ACTIVE" | "ARCHIVED">(() => status.value === "ARCHIVED" ? "ARCHIVED" : "ACTIVE");
function isSelectableTrack(track: TrackSummary): boolean {
  const kind = trackSelectionKind(track);
  return kind === selectionScope.value && (selectionKind.value === null || selectionKind.value === kind);
}
function toggle(track: TrackSummary): void {
  const next = new Map(selectedTracks.value);
  if (next.has(track.id)) {
    next.delete(track.id);
  } else {
    const kind = trackSelectionKind(track);
    if (!isSelectableTrack(track)) {
      ui.notify("warning", "不能混合选择曲目状态", kind === "ARCHIVED" ? "请先清除当前选择，再选择已归档曲目。" : "请先清除已归档曲目，再选择可操作曲目。");
      return;
    }
    next.set(track.id, track);
  }
  selectedTracks.value = next;
}
const selectablePageTracks = computed(() => tracksQuery.data.value?.items.filter(isSelectableTrack) ?? []);
function togglePage(): void {
  const tracks = selectablePageTracks.value;
  if (!tracks.length) return;
  const next = new Map(selectedTracks.value);
  const all = tracks.length > 0 && tracks.every((track) => next.has(track.id));
  for (const track of tracks) all ? next.delete(track.id) : next.set(track.id, track);
  selectedTracks.value = next;
}
const pageSelected = computed(() => selectablePageTracks.value.length > 0 && selectablePageTracks.value.every((track) => selectedIds.value.has(track.id)));
const pagePartiallySelected = computed(() => !pageSelected.value && selectablePageTracks.value.some((track) => selectedIds.value.has(track.id)));
watch([status, metadataStatus, search], () => clearSelection());
watch(() => tracksQuery.data.value?.items, (items) => {
  if (!items?.length || !selectedTracks.value.size) return;
  const next = new Map(selectedTracks.value);
  let changed = false;
  let removed = 0;
  for (const track of items) {
    if (!next.has(track.id)) continue;
    if (trackSelectionKind(track) !== selectionScope.value) {
      next.delete(track.id);
      changed = true;
      removed += 1;
    } else if (next.get(track.id) !== track) {
      next.set(track.id, track);
      changed = true;
    }
  }
  if (!changed) return;
  selectedTracks.value = next;
  if (removed) ui.notify("warning", "选择已更新", `${removed} 首曲目状态已变化，已移出当前选择。`);
});
async function refresh(): Promise<void> { await Promise.all([queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }), queryClient.invalidateQueries({ queryKey: ["admin", "track"] }), queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] }), queryClient.invalidateQueries({ queryKey: ["admin", "audit"] })]); }
async function refreshLists(): Promise<void> { await Promise.all([queryClient.invalidateQueries({ queryKey: ["admin", "tracks"] }), queryClient.invalidateQueries({ queryKey: ["admin", "dashboard"] })]); }

function displayValue(field: string, value: unknown): string {
  if (field === "lyrics" && value && typeof value === "object" && "content" in value) {
    const content = String((value as { content?: unknown }).content ?? "");
    return content ? `歌词 ${content.length} 字符 · ${content.slice(0, 80).replace(/\s+/g, " ")}${content.length > 80 ? "…" : ""}` : "无歌词";
  }
  const serialized = JSON.stringify(value);
  return serialized && serialized.length > 240 ? `${serialized.slice(0, 240)}…` : serialized ?? "—";
}

function beforeUnload(event: BeforeUnloadEvent): void {
  if (!editorOpen.value || !editorDirty.value) return;
  event.preventDefault();
  event.returnValue = "";
}
onMounted(() => window.addEventListener("beforeunload", beforeUnload));
onBeforeUnmount(() => window.removeEventListener("beforeunload", beforeUnload));
onBeforeRouteLeave(() => !editorOpen.value || !editorDirty.value || window.confirm("曲目 Tag 尚未保存，确定离开吗？"));
function closeEditor(): void {
  if (savingMetadata.value) return;
  if (editorDirty.value && !window.confirm("曲目 Tag 尚未保存，确定关闭吗？")) return;
  editorOpen.value = false;
}

const saveMutation = useMutation({
  mutationFn: (input: {
    trackId: string;
    command: {
      expectedVersion: number;
      patch: Partial<Omit<TrackTagValues, "hasArtwork">>;
      resetFields?: string[];
      reason: string;
    };
  }) => musicAdmin.updateTrackMetadata(input.trackId, input.command),
});
async function saveMetadata(): Promise<void> {
  if (savingMetadata.value) return;
  if (selectedTrack.value?.status === "ARCHIVED") {
    actionError.value = "已归档曲目需先恢复后才能修改 Tag";
    ui.notify("warning", "已归档曲目需先恢复", `恢复“${selectedTrack.value.title}”后才能修改 Tag。`);
    return;
  }
  savingMetadata.value = true;
  actionError.value = "";
  try {
    const record = metadataQuery.data.value;
    if (!record) throw new Error("Tag 数据尚未加载完成，请刷新后重试");
    const reason = tags.reason.trim();
    if (!reason) throw new Error("请填写修改原因");
    if (!tags.title.trim() || !nameList(tags.primary).length || !nameList(tags.albumArtists).length) {
      throw new Error("标题、主要艺术家和专辑艺术家不能为空");
    }
    const requestedWriteBack = writeBackAfterSave.value;
    const currentWritebackCapability = sourceWritebackCapability(record.source);
    if (requestedWriteBack && !currentWritebackCapability.canWriteBack) writeBackAfterSave.value = false;
    assertWritebackAllowed(requestedWriteBack, currentWritebackCapability);
    const patch = resetAllOverrides.value ? {} : changedPatch(record);
    const resetFields = resetAllOverrides.value ? record.overriddenFields : undefined;
    const changed = Object.keys(patch).length > 0 || Boolean(resetFields?.length);
    if (!changed && !requestedWriteBack) throw new Error("没有需要保存的 Tag 变化");
    const saved = changed ? await saveMutation.mutateAsync({
      trackId: record.trackId,
      command: { expectedVersion: record.version, patch, resetFields, reason },
    }) : record;
    if (requestedWriteBack) {
      try {
        assertWritebackAllowed(true, sourceWritebackCapability(saved.source));
        await musicAdmin.writeTrackMetadata(saved.trackId, saved.version, reason);
      } catch (error) {
        queryClient.setQueryData(["admin", "track", saved.trackId, "metadata"], saved);
        populate(saved);
        writeBackAfterSave.value = false;
        await refreshLists();
        throw new Error(`Tag 已保存，但写回任务创建失败：${error instanceof Error ? error.message : "未知错误"}`);
      }
    }
    queryClient.setQueryData(["admin", "track", saved.trackId, "metadata"], saved);
    populate(saved);
    writeBackAfterSave.value = false;
    ui.notify("success", requestedWriteBack ? "Tag 已保存并创建写回任务" : "Tag 覆盖值已保存");
    await refreshLists();
  } catch (error) {
    actionError.value = error instanceof Error ? error.message : "Tag 保存失败";
    ui.notify("error", actionError.value.startsWith("Tag 已保存") ? "Tag 写回失败" : "Tag 保存失败", actionError.value);
  } finally {
    savingMetadata.value = false;
  }
}
const stateMutation = useMutation({ mutationFn: ({ track, action }: { track: TrackSummary; action: TrackStateAction }) => musicAdmin.setTrackState(track.id, track.version, action), onSuccess: async (_, variables) => { if (variables.action === "archive") { allowArchiveClose = true; archiveOpen.value = false; } removeSelected([variables.track.id]); const title = variables.action === "archive" ? "曲目已移入回收站" : variables.action === "restore" ? "曲目已恢复" : "曲目已恢复为可用"; ui.notify("success", title); await refresh(); }, onError: (error) => { actionError.value = error instanceof ApiError ? error.message : "更新曲目状态失败"; ui.notify("error", "更新曲目状态失败", actionError.value); } });

const batchRestoreMutation = useMutation({
  mutationFn: () => musicAdmin.batchRestoreTracks(batchRestoreTargets.value),
  onSuccess: async (result) => {
    removeSelected(result.items.map((item) => item.trackId));
    batchRestoreOpen.value = false;
    ui.notify("success", `已恢复 ${result.restored} 首曲目`);
    await refresh();
  },
  onError: (error) => { batchRestoreError.value = error instanceof Error ? error.message : "批量恢复失败"; },
});

function deleteJobStatusActive(status: PermanentDeleteTracksJob["status"] | undefined): boolean {
  return status === "PENDING" || status === "RUNNING";
}
const deleteJobCreateMutation = useMutation({
  mutationFn: () => musicAdmin.createPermanentDeleteTracksJob(permanentDeleteTargets.value),
  onSuccess: (job) => {
    permanentDeleteJob.value = job;
    queryClient.setQueryData(["admin", "tracks", "permanent-delete", job.id], job);
  },
  onError: (error) => { permanentDeleteError.value = error instanceof Error ? error.message : "无法创建永久删除任务"; },
});
const permanentDeletePending = computed(() => deleteJobCreateMutation.isPending.value || deleteJobStatusActive(permanentDeleteJob.value?.status));
const selectionLocked = computed(() => batchRestoreMutation.isPending.value || permanentDeletePending.value);
const permanentDeleteCounts = computed(() => (permanentDeleteJob.value?.items ?? []).reduce((summary, item) => ({
  deletedFiles: summary.deletedFiles + item.deletedFiles,
  quarantinedFiles: summary.quarantinedFiles + item.quarantinedFiles,
  scheduledObjects: summary.scheduledObjects + item.scheduledObjects,
}), { deletedFiles: 0, quarantinedFiles: 0, scheduledObjects: 0 }));
const permanentDeleteFailedItems = computed(() => permanentDeleteJob.value?.items.filter((item) => item.status === "FAILED") ?? []);
const permanentDeleteTargetById = computed(() => new Map(permanentDeleteTargets.value.map((track) => [track.id, track])));
const finalizedPermanentDeleteJobs = new Set<string>();

function permanentDeleteTargetTitle(trackId: string): string {
  return permanentDeleteTargetById.value.get(trackId)?.title ?? trackId;
}
function permanentDeleteItemMessage(item: PermanentDeleteTrackJobItem): string {
  if (item.errorCode === "VERSION_CONFLICT") return "曲目版本已变化，请刷新后重新确认";
  if (item.errorCode === "INVALID_STATE_TRANSITION") return "曲目已不在回收站，请刷新后重试";
  if (item.errorCode === "RESOURCE_CONFLICT") return "曲目仍有音源扫描或媒体处理任务，请等待任务结束后重试";
  if (item.message?.trim()) return item.message.trim();
  return item.errorCode ? `删除失败（${item.errorCode}）` : "删除失败";
}
async function finalizePermanentDeleteJob(job: PermanentDeleteTracksJob): Promise<void> {
  if (deleteJobStatusActive(job.status) || finalizedPermanentDeleteJobs.has(job.id)) return;
  finalizedPermanentDeleteJobs.add(job.id);
  const succeeded = job.items.filter((item) => item.status === "SUCCEEDED");
  const failed = job.items.filter((item) => item.status === "FAILED");
  const succeededIds = new Set(succeeded.map((item) => item.trackId));
  const next = new Map(selectedTracks.value);
  for (const trackId of succeededIds) next.delete(trackId);
  for (const item of failed) {
    const target = permanentDeleteTargetById.value.get(item.trackId);
    if (target) next.set(item.trackId, target);
  }
  selectedTracks.value = next;
  if (selectedTrack.value && succeededIds.has(selectedTrack.value.id)) {
    editorDirty.value = false;
    editorOpen.value = false;
  }
  const counts = permanentDeleteCounts.value;
  const detail = `已删除本地文件 ${counts.deletedFiles} 个，待清理文件 ${counts.quarantinedFiles} 个，媒体对象 ${counts.scheduledObjects} 个进入清理队列。`;
  if (job.failed > 0) ui.notify(job.succeeded > 0 ? "warning" : "error", `永久删除完成：成功 ${job.succeeded}，失败 ${job.failed}`, detail);
  else ui.notify(counts.quarantinedFiles > 0 ? "warning" : "success", `已永久删除 ${job.succeeded} 首曲目`, detail);
  await refresh();
}

watch(() => permanentDeleteJobQuery.data.value, (job) => {
  if (!job) return;
  permanentDeleteJob.value = job;
  void finalizePermanentDeleteJob(job);
});

const bulkMutation = useMutation({ mutationFn: () => { const tracks = [...selectedTracks.value.values()]; const archived = tracks.find((track) => track.status === "ARCHIVED"); if (archived) throw new Error(`已归档曲目“${archived.title}”需先恢复后才能批量修改`); if (!bulk.reason.trim()) throw new Error("请填写批量修改原因"); const patch: Partial<Omit<TrackTagValues, "hasArtwork">> = {}; if (bulk.primary.trim()) patch.credits = nameList(bulk.primary).map((name) => ({ name, role: "PRIMARY" })); if (bulk.albumArtists.trim()) { const albumArtists = nameList(bulk.albumArtists); if (!albumArtists.length) throw new Error("请填写至少一个专辑艺术家"); patch.albumArtists = albumArtists; } if (bulk.album.trim()) patch.album = bulk.album.trim(); if (bulk.genres.trim()) patch.genres = genreList(bulk.genres); if (bulk.comment.trim()) patch.comment = bulk.comment.trim(); if (!Object.keys(patch).length) throw new Error("至少填写一个批量修改字段"); return musicAdmin.batchUpdateTrackMetadata(tracks, patch, bulk.reason.trim()); }, onSuccess: async (result) => { allowBulkClose = true; bulkOpen.value = false; clearSelection(); ui.notify("success", `已更新 ${result.items.length} 首曲目`); await refresh(); }, onError: (error) => { actionError.value = error instanceof Error ? error.message : "批量修改失败"; } });
function selectedArchivedTrack(): TrackSummary | undefined { return [...selectedTracks.value.values()].find((track) => track.status === "ARCHIVED"); }
function archivedTargetsOrNotify(tracks: readonly TrackSummary[]): TrackSummary[] | undefined {
  if (!tracks.length) { ui.notify("warning", "请先选择已归档曲目"); return undefined; }
  const invalid = tracks.find((track) => track.status !== "ARCHIVED");
  if (invalid) { ui.notify("warning", "选择中包含非归档曲目", `曲目“${invalid.title}”状态已变化，请刷新后重新选择。`); return undefined; }
  return [...tracks];
}
function openBatchRestore(): void {
  const targets = archivedTargetsOrNotify([...selectedTracks.value.values()]);
  if (!targets) return;
  batchRestoreTargets.value = targets;
  batchRestoreError.value = "";
  batchRestoreOpen.value = true;
}
function openPermanentDelete(tracks: readonly TrackSummary[]): void {
  const targets = archivedTargetsOrNotify(tracks);
  if (!targets) return;
  permanentDeleteTargets.value = targets;
  permanentDeleteConfirmation.value = "";
  permanentDeleteJob.value = undefined;
  permanentDeleteError.value = "";
  permanentDeleteOpen.value = true;
}
function openBatchPermanentDelete(): void { openPermanentDelete([...selectedTracks.value.values()]); }
function closePermanentDelete(): void { if (!permanentDeletePending.value) permanentDeleteOpen.value = false; }
function openBulk(): void {
  const archived = selectedArchivedTrack();
  if (archived) { ui.notify("warning", "已归档曲目不能批量修改", `请先恢复“${archived.title}”。`); return; }
  Object.assign(bulk, { primary: "", albumArtists: "", album: "", genres: "", comment: "", reason: "" }); actionError.value = ""; bulkOpen.value = true;
}
function openScrape(): void {
  if (selectedTrack.value?.status === "ARCHIVED") { ui.notify("warning", "已归档曲目不能在线刮削", `请先恢复“${selectedTrack.value.title}”。`); return; }
  actionError.value = ""; scrapeOpen.value = true;
}
function openBatchScrape(): void {
  const archived = selectedArchivedTrack();
  if (archived) { ui.notify("warning", "已归档曲目不能批量刮削", `请先恢复“${archived.title}”。`); return; }
  actionError.value = ""; batchScrapeOpen.value = true;
}
async function scrapingCompleted(): Promise<void> { clearSelection(); await refresh(); }
function askRestore(revision: TrackTagRevision): void { revisionToRestore.value = revision; restoreReason.value = ""; actionError.value = ""; restoreOpen.value = true; }
const restoreMutation = useMutation({ mutationFn: () => { if (!restoreReason.value.trim()) throw new Error("请填写恢复原因"); return musicAdmin.restoreTagRevision(metadataQuery.data.value!.trackId, revisionToRestore.value!.id, metadataQuery.data.value!.version, restoreReason.value.trim()); }, onSuccess: async (record) => { allowRestoreClose = true; restoreOpen.value = false; queryClient.setQueryData(["admin", "track", record.trackId, "metadata"], record); populate(record); editorTab.value = "metadata"; ui.notify("success", "历史 Tag 版本已恢复"); await refresh(); }, onError: (error) => { actionError.value = error instanceof Error ? error.message : "恢复失败"; } });
watch(archiveOpen, (value) => { if (!value && stateMutation.isPending.value && stateMutation.variables.value?.action === "archive" && !allowArchiveClose) archiveOpen.value = true; allowArchiveClose = false; });
watch(batchRestoreOpen, (value) => { if (!value && !batchRestoreMutation.isPending.value) { batchRestoreTargets.value = []; batchRestoreError.value = ""; } });
watch(permanentDeleteOpen, (value) => { if (!value && !permanentDeletePending.value) { permanentDeleteTargets.value = []; permanentDeleteConfirmation.value = ""; permanentDeleteJob.value = undefined; permanentDeleteError.value = ""; } });
watch(bulkOpen, (value) => { if (!value && bulkMutation.isPending.value && !allowBulkClose) bulkOpen.value = true; allowBulkClose = false; });
watch(restoreOpen, (value) => { if (!value && restoreMutation.isPending.value && !allowRestoreClose) restoreOpen.value = true; allowRestoreClose = false; });
</script>

<template>
  <div class="space-y-6 page-enter">
    <PageHeader title="曲目" description="检索全部状态的曲目，修改版本化 Tag 并写回可写音源。"><template #eyebrow>音乐资料库</template><template #actions><AppButton :loading="tracksQuery.isFetching.value" @click="tracksQuery.refetch()"><template #icon><RefreshCw :size="16" /></template>刷新</AppButton></template></PageHeader>
    <nav class="flex gap-1 overflow-x-auto rounded-xl bg-[var(--surface-muted)] p-1 sm:w-max"><RouterLink class="pressable rounded-lg bg-[var(--surface-solid)] px-4 py-2 text-sm font-bold text-[var(--primary)] shadow-sm" to="/music/tracks">曲目</RouterLink><RouterLink class="pressable rounded-lg px-4 py-2 text-sm font-semibold text-[var(--muted)]" to="/music/albums">专辑</RouterLink><RouterLink class="pressable rounded-lg px-4 py-2 text-sm font-semibold text-[var(--muted)]" to="/music/artists">艺术家</RouterLink></nav>
    <Transition name="content-swap">
      <div v-if="selectedIds.size && selectionKind === 'ARCHIVED'" class="sticky top-[84px] z-10 flex flex-wrap items-center gap-3 rounded-2xl border border-slate-500/25 bg-[var(--surface-solid)] p-3 shadow-xl"><span class="grid h-9 w-9 place-items-center rounded-xl bg-slate-500/10 text-slate-500"><Archive :size="17" /></span><p class="mr-auto font-semibold">已选择 {{ selectedIds.size }} 首已归档曲目</p><AppButton variant="ghost" :disabled="selectionLocked" @click="clearSelection"><template #icon><X :size="15" /></template>取消</AppButton><AppButton :disabled="selectionLocked" @click="openBatchRestore"><template #icon><RotateCcw :size="15" /></template>批量恢复</AppButton><AppButton variant="danger" :disabled="selectionLocked" @click="openBatchPermanentDelete"><template #icon><Trash2 :size="15" /></template>批量永久删除</AppButton></div>
      <div v-else-if="selectedIds.size && selectionKind === 'ACTIVE'" class="sticky top-[84px] z-10 flex flex-wrap items-center gap-3 rounded-2xl border border-violet-500/25 bg-[var(--surface-solid)] p-3 shadow-xl"><span class="grid h-9 w-9 place-items-center rounded-xl bg-[var(--primary-soft)] text-[var(--primary)]"><Check :size="17" /></span><p class="mr-auto font-semibold">已选择 {{ selectedIds.size }} 首曲目</p><AppButton variant="ghost" :disabled="selectionLocked" @click="clearSelection"><template #icon><X :size="15" /></template>取消</AppButton><AppButton :disabled="selectionLocked" @click="openBatchScrape"><template #icon><Sparkles :size="15" /></template>在线刮削</AppButton><AppButton variant="primary" :disabled="selectionLocked" @click="openBulk"><template #icon><Tags :size="15" /></template>批量修改</AppButton></div>
      <div v-else-if="selectedIds.size" class="sticky top-[84px] z-10 flex flex-wrap items-center gap-3 rounded-2xl border border-amber-500/30 bg-[var(--surface-solid)] p-3 shadow-xl"><AlertTriangle :size="18" class="text-amber-500" /><p class="mr-auto font-semibold">选择中包含不同曲目状态，请清除后重新选择</p><AppButton variant="ghost" @click="clearSelection"><template #icon><X :size="15" /></template>清除选择</AppButton></div>
    </Transition>
    <section class="ui-card overflow-hidden" :class="{ 'data-refreshing': tracksQuery.isFetching.value && !tracksQuery.isPending.value }" :aria-busy="tracksQuery.isFetching.value">
      <div class="flex flex-col gap-3 border-b border-[var(--border)] p-4 lg:flex-row">
        <div class="relative flex-1">
          <Search :size="16" class="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--muted)]" aria-hidden="true" />
          <input v-model="search" class="ui-input !pl-10" type="search" aria-label="搜索曲目" placeholder="搜索标题、艺术家、专辑或路径" @input="page = 1" />
        </div>
        <select v-model="status" class="ui-select lg:!w-40" aria-label="音频状态" @change="page = 1"><option value="READY">可用</option><option value="PROCESSING">处理中</option><option value="ERROR">异常</option><option value="ARCHIVED">已归档</option></select>
        <select v-model="metadataStatus" class="ui-select lg:!w-48" aria-label="Tag 状态" @change="page = 1"><option value="">全部 Tag 状态</option><option value="ORIGINAL">原始</option><option value="OVERRIDDEN">已修改</option><option value="PENDING_WRITE">等待写回</option><option value="WRITE_FAILED">写回失败</option></select>
        <AppButton class="shrink-0 whitespace-nowrap" variant="ghost" @click="clearFilters"><template #icon><ListFilter :size="15" /></template>清除</AppButton>
      </div>
      <StatePanel v-if="tracksQuery.isPending.value" state="loading" />
      <StatePanel v-else-if="tracksQuery.isError.value" state="error" @retry="tracksQuery.refetch()" />
      <StatePanel v-else-if="!tracksQuery.data.value?.items.length" state="empty" :title="status === 'ARCHIVED' ? '回收站为空' : '没有符合条件的曲目'" />
      <template v-else>
        <div class="overflow-x-auto">
          <table class="data-table min-w-[1080px]">
            <thead><tr><th><input type="checkbox" :checked="pageSelected" :indeterminate="pagePartiallySelected" :disabled="!selectablePageTracks.length || selectionLocked" :aria-label="selectionScope === 'ARCHIVED' ? '选择当前页全部已归档曲目' : '选择当前页全部曲目'" @change="togglePage" /></th><th>曲目</th><th>专辑</th><th>时长 / 格式</th><th>音源</th><th>音频状态</th><th>Tag 状态</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="track in tracksQuery.data.value.items" :key="track.id" :class="track.status !== 'ARCHIVED' ? 'cursor-pointer' : undefined" :tabindex="track.status === 'ARCHIVED' ? -1 : 0" :aria-label="track.status === 'ARCHIVED' ? `已归档曲目：${track.title}` : `编辑曲目：${track.title}`" @click="track.status !== 'ARCHIVED' && edit(track)" @keydown.enter="track.status !== 'ARCHIVED' && edit(track)" @keydown.space.prevent="track.status !== 'ARCHIVED' && edit(track)">
                <td @click.stop @keydown.stop><input type="checkbox" :checked="selectedIds.has(track.id)" :disabled="!isSelectableTrack(track) || selectionLocked" :aria-label="track.status === 'ARCHIVED' ? `选择已归档曲目：${track.title}` : `选择曲目：${track.title}`" @change="toggle(track)" /></td>
                <td><div class="flex items-center gap-3"><span class="grid h-11 w-11 place-items-center overflow-hidden rounded-xl bg-[var(--surface-muted)]"><img v-if="track.artwork" :src="track.artwork.url" class="h-full w-full object-cover" alt="封面" width="44" height="44" loading="lazy" decoding="async" /><FileAudio v-else :size="18" /></span><div><p class="max-w-72 truncate font-semibold">{{ track.title }}</p><p class="mt-0.5 max-w-72 truncate text-xs text-[var(--muted)]">{{ track.artists.join('、') || '未知艺术家' }}</p></div></div></td>
                <td>{{ track.album?.title ?? '—' }}</td>
                <td><p class="font-mono text-xs">{{ formatDuration(track.durationMs) }}</p><p class="text-[10px] text-[var(--muted)]">{{ track.source?.format ?? '—' }}</p></td>
                <td><p class="max-w-44 truncate">{{ track.source?.rootName ?? '未关联' }}</p><p class="max-w-44 truncate font-mono text-[10px] text-[var(--muted)]">{{ track.source?.relativePath ?? '—' }}</p></td>
                <td @click.stop @keydown.stop><TrackStatusDisc :track="track" /></td>
                <td><StatusBadge :status="track.metadataStatus" /></td>
                <td @click.stop @keydown.stop><div class="flex gap-1"><button v-if="track.status !== 'ARCHIVED'" class="btn btn-ghost btn-icon" type="button" :aria-label="`编辑曲目：${track.title}`" :disabled="selectionLocked" @click="edit(track)"><Pencil :size="15" /></button><button v-if="track.status === 'ERROR'" class="btn btn-ghost btn-icon" type="button" :aria-label="`恢复曲目为可用：${track.title}`" :disabled="stateMutation.isPending.value || selectionLocked" @click="stateMutation.mutate({ track, action: 'publish' })"><Upload :size="15" /></button><button v-if="track.status === 'READY' || track.status === 'ERROR'" class="btn btn-ghost btn-icon text-[var(--danger)]" type="button" :aria-label="`移入回收站：${track.title}`" :disabled="selectionLocked" @click="askArchive(track)"><Archive :size="15" /></button><template v-if="track.status === 'ARCHIVED'"><button class="btn btn-ghost btn-icon" type="button" :aria-label="`恢复已归档曲目：${track.title}`" :disabled="stateMutation.isPending.value || selectionLocked" @click="stateMutation.mutate({ track, action: 'restore' })"><RotateCcw :size="15" /></button><button class="btn btn-ghost btn-icon text-[var(--danger)]" type="button" :aria-label="`永久删除曲目：${track.title}`" :disabled="selectionLocked" @click="askPermanentDelete(track)"><Trash2 :size="15" /></button></template></div></td>
              </tr>
            </tbody>
          </table>
        </div>
        <AppPagination :page="page" :page-size="pageSize" :total="tracksQuery.data.value.total" @change="page = $event" @page-size-change="changePageSize" />
      </template>
    </section>

    <BaseDialog v-model="editorOpen" title="编辑音乐 Tag" :description="selectedTrack?.source?.relativePath ?? selectedTrack?.title" side="right" :prevent-close="savingMetadata" :confirm-close="editorDirty ? '曲目 Tag 尚未保存，确定关闭吗？' : undefined"><StatePanel v-if="metadataQuery.isPending.value" state="loading" compact /><StatePanel v-else-if="metadataQuery.isError.value" state="error" compact @retry="metadataQuery.refetch()" /><template v-else-if="metadataQuery.data.value"><div class="flex gap-4 rounded-2xl bg-[var(--surface-muted)] p-4"><span class="grid h-16 w-16 place-items-center overflow-hidden rounded-xl bg-[var(--surface-solid)]"><img v-if="selectedTrack?.artwork" :src="selectedTrack.artwork.url" class="h-full w-full object-cover" alt="封面" width="64" height="64" decoding="async" /><Disc3 v-else :size="24" /></span><div class="min-w-0 flex-1"><h3 class="truncate text-lg font-bold">{{ metadataQuery.data.value.effective.title }}</h3><p class="mt-1 text-sm text-[var(--muted)]">{{ credits(metadataQuery.data.value.effective, 'PRIMARY') }}</p><div class="mt-2 flex flex-wrap gap-2"><AudioStatusBadge v-if="selectedTrack" :status="selectedTrack.audioStatus" :source-status="selectedTrack.source?.status" /><StatusBadge :status="selectedTrack?.metadataStatus ?? 'ORIGINAL'" /><StatusBadge :status="editorWritebackCapability.canWriteBack ? 'READ_WRITE' : 'READ_ONLY'" /></div></div></div><div class="mt-5 flex gap-1 overflow-x-auto rounded-xl bg-[var(--surface-muted)] p-1"><button v-for="item in [{key:'metadata',label:'基本 Tag'},{key:'lyrics',label:'歌词'},{key:'original',label:'原始对比'},{key:'history',label:'修改历史'}]" :key="item.key" class="pressable min-w-max flex-1 rounded-lg px-3 py-2 text-xs font-bold" :class="editorTab === item.key ? 'bg-[var(--surface-solid)] text-[var(--primary)] shadow-sm' : 'text-[var(--muted)]'" type="button" @click="editorTab = item.key as EditorTab">{{ item.label }}</button></div>
      <Transition name="content-swap" mode="out-in">
      <div v-if="editorTab === 'metadata'" key="metadata" class="mt-6 grid gap-5 sm:grid-cols-2"><div class="sm:col-span-2"><label class="ui-label">标题</label><input v-model="tags.title" class="ui-input" /></div><div class="sm:col-span-2"><label class="ui-label">主要艺术家</label><input v-model="tags.primary" class="ui-input" /></div><div class="sm:col-span-2"><label class="ui-label">专辑艺术家</label><input v-model="tags.albumArtists" class="ui-input" placeholder="多个艺术家使用分号或换行分隔" /></div><div><label class="ui-label">合作艺术家</label><input v-model="tags.featured" class="ui-input" /></div><div><label class="ui-label">专辑</label><input v-model="tags.album" class="ui-input" /></div><div><label class="ui-label">作曲</label><input v-model="tags.composers" class="ui-input" /></div><div><label class="ui-label">作词</label><input v-model="tags.lyricists" class="ui-input" /></div><div><label class="ui-label">制作人</label><input v-model="tags.producers" class="ui-input" /></div><div><label class="ui-label">发行日期</label><input v-model="tags.releaseDate" class="ui-input" inputmode="numeric" maxlength="10" placeholder="YYYY、YYYY-MM 或 YYYY-MM-DD" /></div><div class="grid grid-cols-2 gap-2"><div><label class="ui-label">音轨号</label><input v-model="tags.trackNumber" class="ui-input" type="number" min="1" max="9999" step="1" /></div><div><label class="ui-label">总音轨</label><input v-model="tags.trackTotal" class="ui-input" type="number" min="1" max="9999" step="1" /></div></div><div class="grid grid-cols-2 gap-2"><div><label class="ui-label">碟号</label><input v-model="tags.discNumber" class="ui-input" type="number" min="1" max="999" step="1" /></div><div><label class="ui-label">总碟数</label><input v-model="tags.discTotal" class="ui-input" type="number" min="1" max="999" step="1" /></div></div><div><label class="ui-label">流派</label><input v-model="tags.genres" class="ui-input" /></div><div><label class="ui-label">BPM</label><input v-model="tags.bpm" class="ui-input" type="number" min="1" max="999.99" step="any" /></div><div><label class="ui-label">ISRC</label><input v-model="tags.isrc" class="ui-input uppercase" maxlength="20" placeholder="USABC1234567" /></div><div><label class="ui-label">版权</label><input v-model="tags.copyright" class="ui-input" /></div><div class="sm:col-span-2"><label class="ui-label">备注</label><textarea v-model="tags.comment" class="ui-textarea" /></div></div>
      <div v-else-if="editorTab === 'lyrics'" key="lyrics" class="mt-6"><div class="grid gap-3 sm:grid-cols-2"><select v-model="tags.lyricsFormat" class="ui-select" aria-label="歌词格式"><option value="PLAIN">普通文本</option><option value="LRC">LRC 时间轴</option></select><input v-model="tags.lyricsLanguage" class="ui-input" maxlength="35" aria-label="歌词语言" placeholder="语言标签，例如 zh-CN 或 und" /></div><textarea v-model="tags.lyrics" class="ui-textarea mt-4 min-h-[380px] font-mono leading-7" aria-label="歌词内容" /></div>
      <div v-else-if="editorTab === 'original'" key="original" class="mt-6"><div class="overflow-hidden rounded-xl border border-[var(--border)]"><table class="data-table"><thead><tr><th>字段</th><th>原始值</th><th>当前生效值</th></tr></thead><tbody><tr v-for="field in ['title','credits','albumArtists','album','releaseDate','trackNumber','trackTotal','discNumber','discTotal','genres','bpm','isrc','comment','copyright','lyrics']" :key="field"><td class="font-mono text-xs">{{ field }}</td><td class="max-w-52 break-all text-xs text-[var(--muted)]">{{ displayValue(field, metadataQuery.data.value.raw[field as keyof TrackTagValues]) }}</td><td class="max-w-52 break-all text-xs" :class="metadataQuery.data.value.overriddenFields.includes(field) && 'text-[var(--primary)] font-semibold'">{{ displayValue(field, metadataQuery.data.value.effective[field as keyof TrackTagValues]) }}</td></tr></tbody></table></div></div>
      <div v-else key="history" class="mt-6" :class="{ 'data-refreshing': historyQuery.isFetching.value && !historyQuery.isPending.value }" :aria-busy="historyQuery.isFetching.value"><StatePanel v-if="historyQuery.isPending.value" state="loading" compact /><StatePanel v-else-if="historyQuery.isError.value" state="error" compact @retry="historyQuery.refetch()" /><StatePanel v-else-if="!historyQuery.data.value?.items.length" state="empty" compact title="暂无修改历史" /><template v-else><div class="space-y-3"><article v-for="revision in historyQuery.data.value.items" :key="revision.id" class="rounded-xl border border-[var(--border)] p-4"><div class="flex items-start justify-between"><div><p class="font-semibold">版本 {{ revision.metadataVersion }} · {{ revision.action }}</p><p class="mt-1 text-xs text-[var(--muted)]">{{ revision.reason ?? '系统生成' }} · {{ formatDate(revision.createdAt) }}</p></div><button class="btn btn-ghost" type="button" @click="askRestore(revision)"><RotateCcw :size="14" />恢复</button></div><div class="mt-3 flex flex-wrap gap-1"><span v-for="field in revision.overriddenFields" :key="field" class="rounded bg-[var(--surface-muted)] px-2 py-1 font-mono text-[10px]">{{ field }}</span></div></article></div><AppPagination :page="historyPage" :page-size="historyPageSize" :total="historyQuery.data.value.total" @change="historyPage = $event" @page-size-change="changeHistoryPageSize" /></template></div>
      </Transition>
      <div class="mt-6"><label class="ui-label">修改原因</label><input v-model="tags.reason" class="ui-input" placeholder="会写入版本历史和审计日志" /></div><label v-if="editorWritebackCapability.canWriteBack" class="mt-4 flex items-center justify-between rounded-xl border border-[var(--border)] p-4"><span><span class="block font-semibold">写回源文件 Tag</span><span class="text-xs text-[var(--muted)]">保存后创建安全写回任务；默认关闭。</span></span><button type="button" class="switch" role="switch" :aria-checked="writeBackAfterSave" @click="writeBackAfterSave = !writeBackAfterSave" /></label><p v-else class="mt-4 rounded-xl bg-[var(--surface-muted)] p-4 text-xs leading-5 text-[var(--muted)]">{{ writebackBlockedMessage(editorWritebackCapability) }}，只保存数据库中的 Tag 修改。</p><label v-if="metadataQuery.data.value.overriddenFields.length" class="mt-4 flex items-center justify-between rounded-xl border border-[var(--border)] p-4"><span><span class="block font-semibold">恢复全部原始字段</span><span class="text-xs text-[var(--muted)]">移除当前 {{ metadataQuery.data.value.overriddenFields.length }} 个覆盖字段</span></span><button type="button" class="switch" role="switch" :aria-checked="resetAllOverrides" @click="resetAllOverrides = !resetAllOverrides" /></label><p v-if="actionError" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p></template>
      <template #footer><AppButton v-if="selectedTrack?.status !== 'ARCHIVED'" @click="openScrape"><template #icon><Sparkles :size="15" /></template>在线刮削</AppButton><span class="flex-1" /><AppButton :disabled="savingMetadata" @click="closeEditor">关闭</AppButton><AppButton v-if="selectedTrack?.status !== 'ARCHIVED' && (editorTab === 'metadata' || editorTab === 'lyrics')" variant="primary" :loading="savingMetadata" @click="saveMetadata"><template #icon><Save :size="15" /></template>{{ writeBackAfterSave ? '保存并写回' : '保存覆盖值' }}</AppButton></template>
    </BaseDialog>

    <BaseDialog v-model="bulkOpen" :title="`批量修改 ${selectedIds.size} 首曲目`" description="服务端在一个事务中校验全部元数据版本；任一冲突则整体回滚。" width="lg"><div class="grid gap-5 sm:grid-cols-2"><div><label class="ui-label">主要艺术家</label><input v-model="bulk.primary" class="ui-input" placeholder="填写后会替换全部署名" /></div><div><label class="ui-label">专辑艺术家</label><input v-model="bulk.albumArtists" class="ui-input" placeholder="可选；多个名称使用分号或换行分隔" /></div><div><label class="ui-label">专辑</label><input v-model="bulk.album" class="ui-input" /></div><div><label class="ui-label">流派</label><input v-model="bulk.genres" class="ui-input" /></div><div><label class="ui-label">备注</label><input v-model="bulk.comment" class="ui-input" /></div><div class="sm:col-span-2"><label class="ui-label">批量修改原因</label><input v-model="bulk.reason" class="ui-input" /></div></div><p v-if="actionError" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton @click="bulkOpen = false">取消</AppButton><AppButton variant="primary" :loading="bulkMutation.isPending.value" @click="bulkMutation.mutate()">应用批量修改</AppButton></template></BaseDialog>
    <BaseDialog v-model="restoreOpen" title="恢复历史 Tag 版本" :description="revisionToRestore ? `元数据版本 ${revisionToRestore.metadataVersion}` : ''"><div><label class="ui-label">恢复原因</label><input v-model="restoreReason" class="ui-input" /></div><p v-if="actionError" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton @click="restoreOpen = false">取消</AppButton><AppButton variant="primary" :loading="restoreMutation.isPending.value" @click="restoreMutation.mutate()">恢复此版本</AppButton></template></BaseDialog>
    <BaseDialog v-model="batchRestoreOpen" :title="`批量恢复 ${batchRestoreTargets.length} 首曲目`" description="恢复操作会原子校验全部曲目版本；任一曲目状态或版本变化时不会恢复任何曲目。" :prevent-close="batchRestoreMutation.isPending.value">
      <div class="rounded-xl bg-[var(--surface-muted)] p-4"><p class="font-semibold">恢复后曲目会重新进入曲库</p><p class="mt-1 text-xs leading-5 text-[var(--muted)]">不会删除本地文件或媒体对象；服务端会再次确认每首曲目仍然可播放。</p></div>
      <div class="mt-4 max-h-56 divide-y divide-[var(--border)] overflow-y-auto rounded-xl border border-[var(--border)]"><div v-for="track in batchRestoreTargets.slice(0, 8)" :key="track.id" class="px-4 py-3"><p class="truncate font-semibold">{{ track.title }}</p><p class="mt-0.5 truncate text-xs text-[var(--muted)]">{{ track.artists.join('、') || '未知艺术家' }}</p></div><p v-if="batchRestoreTargets.length > 8" class="px-4 py-3 text-xs text-[var(--muted)]">另有 {{ batchRestoreTargets.length - 8 }} 首曲目</p></div>
      <p v-if="batchRestoreError" class="mt-4 whitespace-pre-line rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ batchRestoreError }}</p>
      <template #footer><AppButton :disabled="batchRestoreMutation.isPending.value" @click="batchRestoreOpen = false">取消</AppButton><AppButton variant="primary" :loading="batchRestoreMutation.isPending.value" @click="batchRestoreMutation.mutate()"><template #icon><RotateCcw :size="15" /></template>确认恢复</AppButton></template>
    </BaseDialog>
    <BaseDialog v-model="archiveOpen" title="删除曲目" description="曲目会移入回收站，并立即从默认列表及客户端中消失。"><div class="rounded-xl bg-[var(--surface-muted)] p-4"><p class="font-semibold">{{ deletionTrack?.title }}</p><p class="mt-1 text-xs text-[var(--muted)]">可以在回收站中永久删除原始文件和全部数据。</p></div><p v-if="actionError" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ actionError }}</p><template #footer><AppButton @click="archiveOpen = false">取消</AppButton><AppButton variant="danger" :loading="stateMutation.isPending.value" @click="stateMutation.mutate({ track: deletionTrack!, action: 'archive' })">移入回收站</AppButton></template></BaseDialog>
    <BaseDialog v-model="permanentDeleteOpen" :title="permanentDeleteJob ? '永久删除任务' : `永久删除 ${permanentDeleteTargets.length} 首曲目`" :description="permanentDeleteJob ? '任务由服务端持久化执行，页面持续查询逐项结果。' : '此操作不可恢复，并会清理曲目关联的本地文件与媒体对象。'" width="lg" :prevent-close="permanentDeletePending">
      <template v-if="!permanentDeleteJob">
        <div class="rounded-xl border border-rose-500/30 bg-rose-500/10 p-4"><p class="font-semibold text-[var(--danger)]">将永久删除 {{ permanentDeleteTargets.length }} 首曲目</p><p class="mt-2 text-xs leading-5 text-[var(--muted)]">歌单引用、收藏、播放历史、Tag、歌词、转码文件及 MinIO 对象都会进入删除或安全清理流程。</p></div>
        <div class="mt-4 max-h-48 divide-y divide-[var(--border)] overflow-y-auto rounded-xl border border-[var(--border)]"><div v-for="track in permanentDeleteTargets.slice(0, 8)" :key="track.id" class="px-4 py-3"><p class="truncate font-semibold">{{ track.title }}</p><p class="mt-0.5 truncate text-xs text-[var(--muted)]">{{ track.source?.relativePath ?? track.id }}</p></div><p v-if="permanentDeleteTargets.length > 8" class="px-4 py-3 text-xs text-[var(--muted)]">另有 {{ permanentDeleteTargets.length - 8 }} 首曲目</p></div>
        <div class="mt-5"><label class="ui-label">输入“永久删除”以确认</label><input v-model="permanentDeleteConfirmation" class="ui-input" autocomplete="off" aria-label="永久删除确认文字" placeholder="永久删除" /></div>
        <p v-if="permanentDeleteError" class="mt-4 whitespace-pre-line rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]">{{ permanentDeleteError }}</p>
      </template>
      <template v-else>
        <div class="rounded-xl bg-[var(--surface-muted)] p-4" aria-live="polite"><div class="flex items-center justify-between gap-3"><StatusBadge :status="permanentDeleteJob.status" dot /><span class="font-semibold">{{ permanentDeleteJob.processed }} / {{ permanentDeleteJob.total }}</span></div><div class="mt-3 h-2 overflow-hidden rounded-full bg-[var(--surface-solid)]"><div class="progress-fill h-full rounded-full" :class="permanentDeleteJob.failed ? 'bg-amber-500' : 'bg-[var(--primary)]'" :style="{ width: `${permanentDeleteJob.total ? permanentDeleteJob.processed / permanentDeleteJob.total * 100 : 0}%` }" /></div><p class="mt-2 text-xs text-[var(--muted)]">成功 {{ permanentDeleteJob.succeeded }} · 失败 {{ permanentDeleteJob.failed }}</p></div>
        <div class="mt-4 grid grid-cols-3 gap-px overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--border)] text-center"><div class="bg-[var(--surface-solid)] p-3"><p class="text-lg font-bold">{{ permanentDeleteCounts.deletedFiles }}</p><p class="text-[10px] text-[var(--muted)]">已删除本地文件</p></div><div class="bg-[var(--surface-solid)] p-3"><p class="text-lg font-bold" :class="permanentDeleteCounts.quarantinedFiles ? 'text-amber-500' : undefined">{{ permanentDeleteCounts.quarantinedFiles }}</p><p class="text-[10px] text-[var(--muted)]">待清理文件</p></div><div class="bg-[var(--surface-solid)] p-3"><p class="text-lg font-bold">{{ permanentDeleteCounts.scheduledObjects }}</p><p class="text-[10px] text-[var(--muted)]">待清理媒体对象</p></div></div>
        <div class="mt-4 max-h-72 divide-y divide-[var(--border)] overflow-y-auto rounded-xl border border-[var(--border)]"><article v-for="item in permanentDeleteJob.items" :key="item.id" class="px-4 py-3"><div class="flex items-start justify-between gap-3"><div class="min-w-0"><p class="truncate font-semibold">{{ permanentDeleteTargetTitle(item.trackId) }}</p><p v-if="item.status === 'FAILED'" class="mt-1 whitespace-pre-line text-xs leading-5 text-[var(--danger)]">{{ permanentDeleteItemMessage(item) }}</p><p v-else-if="item.status === 'SUCCEEDED'" class="mt-1 text-xs text-[var(--muted)]">本地文件 {{ item.deletedFiles }} · 待清理文件 {{ item.quarantinedFiles }} · 媒体对象 {{ item.scheduledObjects }}</p><p v-else class="mt-1 text-xs text-[var(--muted)]">尝试 {{ item.attempts }} 次</p></div><StatusBadge :status="item.status" /></div></article></div>
        <div v-if="permanentDeleteJobQuery.isError.value" class="mt-4 rounded-xl bg-rose-500/10 p-3 text-sm text-[var(--danger)]"><p>{{ permanentDeleteJobQuery.error.value instanceof Error ? permanentDeleteJobQuery.error.value.message : '读取删除任务失败' }}</p><AppButton class="mt-3" variant="ghost" :loading="permanentDeleteJobQuery.isFetching.value" @click="permanentDeleteJobQuery.refetch()">重新查询</AppButton></div>
        <p v-if="permanentDeleteFailedItems.length" class="mt-4 rounded-xl bg-amber-500/10 p-3 text-xs leading-5 text-amber-700 dark:text-amber-300">失败曲目会保留在当前选择中。版本或状态冲突必须刷新列表并重新确认，系统不会自动使用新版本永久删除。</p>
      </template>
      <template #footer><template v-if="!permanentDeleteJob"><AppButton :disabled="deleteJobCreateMutation.isPending.value" @click="closePermanentDelete">取消</AppButton><AppButton variant="danger" :loading="deleteJobCreateMutation.isPending.value" :disabled="permanentDeleteConfirmation !== '永久删除' || !permanentDeleteTargets.length" @click="deleteJobCreateMutation.mutate()"><template #icon><Trash2 :size="15" /></template>永久删除 {{ permanentDeleteTargets.length }} 首</AppButton></template><AppButton v-else-if="permanentDeletePending" variant="danger" loading disabled>永久删除处理中</AppButton><AppButton v-else @click="closePermanentDelete">关闭</AppButton></template>
    </BaseDialog>
    <TagScrapeDialog v-model="scrapeOpen" :track="selectedTrack" :expected-version="metadataQuery.data.value?.version" :writeback-source="metadataQuery.data.value?.source" @applied="scrapingCompleted" />
    <BatchTagScrapeDialog v-model="batchScrapeOpen" :tracks="[...selectedTracks.values()]" @completed="scrapingCompleted" />
  </div>
</template>
