<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Compass, Disc3, Heart, Home, LogOut, Plus, Radio, Settings } from "@lucide/vue";
import type { UserSession } from "../../application/ports/SessionRepository";
import type { Playlist } from "../../domain/music";
import type { LibraryView } from "../../application/navigation";
import ArtworkImage from "./ui/ArtworkImage.vue";

const props = withDefaults(defineProps<{ user: UserSession["user"]; active: LibraryView; playlists: Playlist[]; fullscreen?: boolean }>(), {
  fullscreen: false,
});
const emit = defineEmits<{ logout: []; navigate: [view: LibraryView]; playlist: [playlist: Playlist]; createPlaylist: [] }>();
const initials = computed(() => (props.user.displayName || props.user.username).trim().slice(0, 1).toUpperCase());
const avatarFailed = ref(false);
watch(() => props.user.avatarUrl, () => { avatarFailed.value = false; });
</script>

<template>
  <aside class="sidebar" aria-label="应用侧栏">
    <div v-if="props.fullscreen" class="sidebar-user" aria-label="用户个人中心">
      <div class="user-avatar-wrap">
        <img v-if="user.avatarUrl && !avatarFailed" :src="user.avatarUrl" class="user-avatar" :alt="`${user.displayName}的头像`" @error="avatarFailed = true" />
        <div v-else class="user-avatar-fallback" aria-hidden="true">{{ initials }}</div>
      </div>
      <div class="user-info">
        <span class="user-name" :title="user.displayName">{{ user.displayName }}</span>
      </div>
    </div>
    <div v-else class="sidebar-user" aria-label="用户个人中心" data-tauri-drag-region>
      <div class="user-avatar-wrap">
        <img v-if="user.avatarUrl && !avatarFailed" :src="user.avatarUrl" class="user-avatar" :alt="`${user.displayName}的头像`" @error="avatarFailed = true" />
        <div v-else class="user-avatar-fallback" aria-hidden="true">{{ initials }}</div>
      </div>
      <div class="user-info">
        <span class="user-name" :title="user.displayName">{{ user.displayName }}</span>
      </div>
    </div>

    <!-- 2x2 Quick Navigation Dock -->
    <div class="nav-dock-grid" aria-label="核心导航">
      <button
        type="button"
        class="dock-item"
        :class="{ active: active === 'discover' }"
        :aria-current="active === 'discover' ? 'page' : undefined"
        title="发现音乐"
        @click="emit('navigate', 'discover')"
      >
        <Home :size="20" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="dock-item"
        :class="{ active: active === 'recent' }"
        :aria-current="active === 'recent' ? 'page' : undefined"
        title="最近播放"
        @click="emit('navigate', 'recent')"
      >
        <Radio :size="20" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="dock-item"
        :class="{ active: active === 'playlists' }"
        :aria-current="active === 'playlists' ? 'page' : undefined"
        title="歌单库"
        @click="emit('navigate', 'playlists')"
      >
        <Compass :size="20" aria-hidden="true" />
      </button>
      <button
        type="button"
        class="dock-item dock-item--action"
        title="新建歌单"
        aria-label="新建歌单"
        @click="emit('createPlaylist')"
      >
        <Plus :size="20" aria-hidden="true" />
      </button>
    </div>

    <!-- Secondary Nav List -->
    <nav class="nav-group nav-secondary" aria-label="快捷入口">
      <button
        type="button"
        class="nav-item"
        :class="{ active: active === 'favorites' }"
        :aria-current="active === 'favorites' ? 'page' : undefined"
        title="喜欢的音乐"
        @click="emit('navigate', 'favorites')"
      >
        <Heart :size="17" aria-hidden="true" />
        <span>喜欢</span>
      </button>
      <button
        type="button"
        class="nav-item"
        :class="{ active: active === 'recent' }"
        :aria-current="active === 'recent' ? 'page' : undefined"
        title="最近播放"
        @click="emit('navigate', 'recent')"
      >
        <Disc3 :size="17" aria-hidden="true" />
        <span>最近</span>
      </button>
    </nav>

    <!-- Playlist Section -->
    <section class="nav-section playlist-section" aria-labelledby="playlist-heading">
      <div class="nav-label-row">
        <button id="playlist-heading" type="button" class="playlist-heading" @click="emit('navigate', 'playlists')">
          <span class="playlist-tab-active">自建歌单</span>
        </button>
        <button type="button" class="icon-button small" title="新建歌单" aria-label="新建歌单" @click="emit('createPlaylist')">
          <Plus :size="15" aria-hidden="true" />
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
          <ArtworkImage class="playlist-cover" :src="playlist.coverUrl" :alt="`${playlist.title}歌单封面`" kind="playlist" />
          <span>{{ playlist.title }}</span>
        </button>
      </div>
    </section>

    <!-- Sidebar Footer -->
    <div class="sidebar-footer">
      <div class="footer-actions">
        <button
          type="button"
          class="footer-icon-btn"
          :class="{ active: active === 'settings' }"
          title="设置"
          aria-label="设置"
          @click="emit('navigate', 'settings')"
        >
          <Settings :size="17" aria-hidden="true" />
        </button>
        <button
          type="button"
          class="footer-icon-btn"
          title="退出登录"
          aria-label="退出登录"
          @click="emit('logout')"
        >
          <LogOut :size="17" aria-hidden="true" />
        </button>
      </div>
    </div>
  </aside>
</template>
