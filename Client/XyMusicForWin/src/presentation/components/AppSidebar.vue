<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Activity, Disc3, Heart, Home, ListMusic, LogOut, Plus, Settings } from "@lucide/vue";
import type { UserSession } from "../../application/ports/SessionRepository";
import brandMark from "../../assets/brand-mark.png";
import type { Playlist } from "../../domain/music";
import type { LibraryView } from "../../domain/navigation";

const props = defineProps<{ user: UserSession["user"]; active: LibraryView; playlists: Playlist[] }>();
const emit = defineEmits<{ logout: []; navigate: [view: LibraryView]; playlist: [playlist: Playlist]; createPlaylist: [] }>();
const initials = computed(() => (props.user.displayName || props.user.username).trim().slice(0, 1).toUpperCase());
const avatarFailed = ref(false);
watch(() => props.user.avatarUrl, () => { avatarFailed.value = false; });
const primary = [{ view: "discover" as const, label: "发现音乐", icon: Home }];
const library = [
  { view: "recent" as const, label: "最近播放", icon: Disc3 },
  { view: "favorites" as const, label: "喜欢的音乐", icon: Heart },
];
</script>

<template>
  <aside class="sidebar" aria-label="应用侧栏">
    <div class="brand" aria-label="XY Music">
      <span class="brand-mark"><img :src="brandMark" alt="" /></span>
      <span>XY Music</span>
    </div>

    <nav class="nav-group nav-primary" aria-label="主导航">
      <button
        v-for="item in primary"
        :key="item.view"
        type="button"
        class="nav-item"
        :class="{ active: active === item.view }"
        :aria-current="active === item.view ? 'page' : undefined"
        :title="item.label"
        @click="emit('navigate', item.view)"
      >
        <component :is="item.icon" :size="19" aria-hidden="true" />
        <span>{{ item.label }}</span>
      </button>
    </nav>

    <section class="nav-section" aria-labelledby="library-heading">
      <p id="library-heading" class="nav-label">音乐库</p>
      <nav class="nav-group" aria-label="音乐库">
        <button
          v-for="item in library"
          :key="item.view"
          type="button"
          class="nav-item"
          :class="{ active: active === item.view }"
          :aria-current="active === item.view ? 'page' : undefined"
          :title="item.label"
          @click="emit('navigate', item.view)"
        >
          <component :is="item.icon" :size="19" aria-hidden="true" />
          <span>{{ item.label }}</span>
        </button>
      </nav>
    </section>

    <section class="nav-section playlist-section" aria-labelledby="playlist-heading">
      <div class="nav-label-row">
        <button id="playlist-heading" type="button" class="playlist-heading" @click="emit('navigate', 'playlists')">
          <ListMusic :size="15" aria-hidden="true" />
          <span>我的歌单</span>
        </button>
        <button type="button" class="icon-button small" title="新建歌单" aria-label="新建歌单" @click="emit('createPlaylist')">
          <Plus :size="16" aria-hidden="true" />
        </button>
      </div>
      <div class="playlist-links">
        <button
          v-for="playlist in playlists.slice(0, 8)"
          :key="playlist.id"
          type="button"
          class="playlist-link"
          :title="playlist.title"
          @click="emit('playlist', playlist)"
        >
          <span class="playlist-dot" :style="{ background: playlist.accent }" aria-hidden="true"></span>
          <span>{{ playlist.title }}</span>
        </button>
      </div>
    </section>

    <div class="sidebar-footer">
      <button
        type="button"
        class="nav-item"
        :class="{ active: active === 'diagnostics' }"
        :aria-current="active === 'diagnostics' ? 'page' : undefined"
        title="诊断"
        @click="emit('navigate', 'diagnostics')"
      >
        <Activity :size="19" aria-hidden="true" />
        <span>诊断</span>
      </button>
      <button
        type="button"
        class="nav-item"
        :class="{ active: active === 'settings' }"
        :aria-current="active === 'settings' ? 'page' : undefined"
        title="设置"
        @click="emit('navigate', 'settings')"
      >
        <Settings :size="19" aria-hidden="true" />
        <span>设置</span>
      </button>
      <div class="profile">
        <img v-if="user.avatarUrl && !avatarFailed" :src="user.avatarUrl" class="profile-avatar" :alt="`${user.displayName}的头像`" @error="avatarFailed = true" />
        <div v-else class="profile-avatar" aria-hidden="true">{{ initials }}</div>
        <div class="profile-copy">
          <strong>{{ user.displayName }}</strong>
          <span>@{{ user.username }}</span>
        </div>
        <button type="button" class="bare-button profile-logout" title="退出登录" aria-label="退出登录" @click="emit('logout')">
          <LogOut :size="17" aria-hidden="true" />
        </button>
      </div>
    </div>
  </aside>
</template>
