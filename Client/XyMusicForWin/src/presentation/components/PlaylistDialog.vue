<script setup lang="ts">
import { reactive, watch } from "vue";
import type { Playlist, PlaylistVisibility } from "../../domain/music";
import AppDialog from "./ui/AppDialog.vue";

const props = defineProps<{ open: boolean; playlist?: Playlist; busy: boolean; error: string }>();
const emit = defineEmits<{ close: []; save: [value: { name: string; description: string; visibility: PlaylistVisibility }] }>();
const form = reactive<{ name: string; description: string; visibility: PlaylistVisibility }>({ name: "", description: "", visibility: "PRIVATE" });

watch(() => [props.open, props.playlist] as const, () => {
  form.name = props.playlist?.title ?? "";
  form.description = props.playlist?.description ?? "";
  form.visibility = props.playlist?.visibility ?? "PRIVATE";
}, { immediate: true });

function save() {
  const name = form.name.trim();
  if (name) emit("save", { name, description: form.description.trim(), visibility: form.visibility });
}
</script>

<template>
  <AppDialog
    :open="open"
    :title="playlist ? '编辑歌单' : '新建歌单'"
    :description="playlist ? '修改歌单信息与可见范围。' : '创建一个新的音乐集合。'"
    :dismissible="!busy"
    @close="emit('close')"
  >
    <form id="playlist-editor-form" class="dialog-form" @submit.prevent="save">
      <label class="field-group"><span>名称</span><input v-model="form.name" maxlength="100" required autofocus /></label>
      <label class="field-group"><span>描述</span><textarea v-model="form.description" maxlength="1000" rows="4" placeholder="这个歌单里有什么？"></textarea></label>
      <label class="field-group"><span>可见性</span><select v-model="form.visibility"><option value="PRIVATE">私密</option><option value="UNLISTED">不公开列出</option><option value="PUBLIC">公开</option></select></label>
      <p v-if="error" class="dialog-error" role="alert">{{ error }}</p>
    </form>
    <template #actions>
      <button type="button" class="secondary-button" :disabled="busy" @click="emit('close')">取消</button>
      <button type="submit" form="playlist-editor-form" class="primary-button" :disabled="busy || !form.name.trim()">{{ busy ? "正在保存…" : "保存" }}</button>
    </template>
  </AppDialog>
</template>
