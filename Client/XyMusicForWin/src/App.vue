<script setup lang="ts">
import { computed, defineAsyncComponent, ref } from "vue";
import AppSidebar from "./presentation/components/AppSidebar.vue";
import TopBar from "./presentation/components/TopBar.vue";
import PlayerBar from "./presentation/components/PlayerBar.vue";
import MiniPlayer from "./presentation/components/MiniPlayer.vue";
import LoginView from "./presentation/components/LoginView.vue";
import ToastHost from "./presentation/components/ui/ToastHost.vue";
import DesktopShell from "./presentation/layouts/DesktopShell.vue";
import { useMusicActions } from "./presentation/composables/useMusicActions";
import { useNavigationActions, type ScrollContainer } from "./presentation/composables/useNavigationActions";
import { usePlaylistActions } from "./presentation/composables/usePlaylistActions";
import { usePlaybackShortcuts } from "./presentation/composables/usePlaybackShortcuts";
import { useDesktopLyricsBridge } from "./presentation/composables/useDesktopLyricsBridge";
import { useSessionLifecycle } from "./presentation/composables/useSessionLifecycle";
import { useWindowFullscreen } from "./presentation/composables/useWindowFullscreen";
import { useHomeStore } from "./presentation/stores/homeStore";
import { useDesktopLyricsStore } from "./presentation/stores/desktopLyricsStore";
import { useLibraryStore } from "./presentation/stores/libraryStore";
import { useLyricsStore } from "./presentation/stores/lyricsStore";
import { useNavigationStore } from "./presentation/stores/navigationStore";
import { usePlayerStore } from "./presentation/stores/playerStore";
import { useSessionStore } from "./presentation/stores/sessionStore";
import { useThemeStore } from "./presentation/stores/themeStore";
import { useToastStore } from "./presentation/stores/toastStore";
import { libraryViewRequiresHomeFeed } from "./domain/navigation";

const SettingsView = defineAsyncComponent(() => import("./presentation/components/SettingsView.vue"));
const DiscoverView = defineAsyncComponent(() => import("./presentation/views/DiscoverView.vue"));
const SearchView = defineAsyncComponent(() => import("./presentation/views/SearchView.vue"));
const CollectionView = defineAsyncComponent(() => import("./presentation/views/CollectionView.vue"));
const TracksView = defineAsyncComponent(() => import("./presentation/views/TracksView.vue"));
const PlaylistsView = defineAsyncComponent(() => import("./presentation/views/PlaylistsView.vue"));
const PlaylistDialog = defineAsyncComponent(() => import("./presentation/components/PlaylistDialog.vue"));
const AddToPlaylistDialog = defineAsyncComponent(() => import("./presentation/components/AddToPlaylistDialog.vue"));
const QueuePanel = defineAsyncComponent(() => import("./presentation/components/QueuePanel.vue"));
const LyricsView = defineAsyncComponent(() => import("./presentation/components/LyricsView.vue"));
const DiagnosticsView = defineAsyncComponent(() => import("./presentation/views/DiagnosticsView.vue"));

const home = useHomeStore();
const desktopLyrics = useDesktopLyricsStore();
const library = useLibraryStore();
const lyrics = useLyricsStore();
const navigation = useNavigationStore();
const player = usePlayerStore();
const session = useSessionStore();
const theme = useThemeStore();
const toast = useToastStore();
const shell = ref<ScrollContainer | null>(null);

theme.initialize();
void desktopLyrics.initialize();
useDesktopLyricsBridge();
usePlaybackShortcuts();
const windowFullscreen = useWindowFullscreen();

const {
  currentEntry,
  detailOpen,
  currentLibraryView,
  activeTrackViewCopy,
  updateSearch,
  navigate,
  openAlbum,
  openArtist,
  openPlaylist,
  goBack,
  goForward,
} = useNavigationActions(shell);

const {
  actionError,
  albumPlayLoadingId,
  playlistPlayLoadingId,
  play,
  playAt,
  playVisible,
  playDiscoveryTrack,
  playAlbum,
  playPlaylist,
  toggleFavorite,
  reportActionError,
  clearActionError,
} = useMusicActions();

const {
  playlistDialogOpen,
  editingPlaylist,
  dialogBusy,
  dialogError,
  addTrack,
  addDialogError,
  addPlaylists,
  addPlaylistsLoading,
  addPlaylistsRetryAvailable,
  addPlaylistsHasMore,
  newPlaylist,
  editPlaylist,
  savePlaylist,
  deletePlaylist,
  openAddToPlaylist,
  closeAddToPlaylist,
  loadMoreAddPlaylists,
  retryAddPlaylists,
  addToPlaylist,
  removeEntry,
  removeEntries,
  moveEntry,
  reorderEntries,
  resetDialogs,
} = usePlaylistActions(reportActionError);

const { logout, logoutAll, updateProfile, uploadAvatar, switchServer } = useSessionLifecycle(resetDialogs, clearActionError);
const lyricsTextColor = computed(() => lyrics.colors[theme.theme].textColor);
const lyricsHighlightColor = computed(() => lyrics.colors[theme.theme].highlightColor);
const homeFeedRequired = computed(() => currentEntry.value.kind === "library" && libraryViewRequiresHomeFeed(currentLibraryView.value));
const pageLoading = computed(() => (
  (homeFeedRequired.value && home.loading && !home.feed)
  || (currentEntry.value.kind !== "search" && library.loading)
));
const homeFeedUnavailable = computed(() => homeFeedRequired.value && Boolean(home.feedError) && !home.feed);
const pageReady = computed(() => currentEntry.value.kind === "search" || !homeFeedRequired.value || Boolean(home.feed));
const initialPageError = computed(() => {
  if (currentEntry.value.kind === "search") return home.searchError && !home.searchResults ? home.searchError : "";
  if (!library.error || !library.retryAvailable) return "";
  if (detailOpen.value) return library.tracks.length || library.selectedPlaylist ? "" : library.error;
  if (currentLibraryView.value === "playlists") return library.playlists.length ? "" : library.error;
  if (currentLibraryView.value === "recent" || currentLibraryView.value === "favorites") return library.tracks.length ? "" : library.error;
  return "";
});
const visiblePageError = computed(() => {
  if (currentEntry.value.kind === "search") return home.searchError;
  const feedError = homeFeedRequired.value ? home.feedError : "";
  return feedError || library.error || actionError.value;
});
const pageRetryAvailable = computed(() => {
  if (currentEntry.value.kind === "search") return Boolean(home.searchError);
  if (homeFeedRequired.value && home.feedError) return true;
  return Boolean(library.error && library.retryAvailable);
});
const detailPlayAllLoading = computed(() => {
  if (currentEntry.value.kind === "album") return albumPlayLoadingId.value === currentEntry.value.album.id;
  if (currentEntry.value.kind === "playlist") return playlistPlayLoadingId.value === currentEntry.value.playlist.id;
  return false;
});
const currentPlaylistEntryId = computed(() => {
  const entries = library.selectedPlaylist?.entries;
  if (!entries || player.queue.length !== library.tracks.length) return undefined;
  if (!player.queue.every((track, index) => track.id === library.tracks[index]?.id)) return undefined;
  return entries[player.currentIndex]?.id;
});

function retryCurrentPage(): void {
  clearActionError();
  if (currentEntry.value.kind === "search") {
    home.retrySearch();
    return;
  }
  if (homeFeedRequired.value && home.feedError) {
    void home.load();
    return;
  }
  if (library.retryAvailable) void library.retry();
}

function playCurrentCollection(): void {
  const firstTrack = library.tracks[0];
  if (!firstTrack) return;
  if (currentEntry.value.kind === "album") {
    void playAlbum(currentEntry.value.album, { tracks: [...library.tracks], nextCursor: library.detailNextCursor });
    return;
  }
  if (currentEntry.value.kind === "playlist" && library.selectedPlaylist) {
    void playPlaylist(library.selectedPlaylist, { tracks: [...library.tracks], nextCursor: library.detailNextCursor });
    return;
  }
  play(firstTrack, library.tracks);
}

</script>

<template>
  <MiniPlayer v-if="player.miniMode && session.session" :fullscreen="windowFullscreen" @favorite="toggleFavorite" />
  <div v-else class="window-shell">
    <TopBar
      v-if="!player.lyricsOpen"
      :model-value="session.session ? home.search : ''"
      :searching="Boolean(session.session) && home.searching"
      :search-enabled="Boolean(session.session)"
      :can-go-back="navigation.canGoBack"
      :can-go-forward="navigation.canGoForward"
      :fullscreen="windowFullscreen"
      @update:model-value="updateSearch"
      @back="goBack"
      @forward="goForward"
    />
    <div class="window-content">
      <div v-if="session.restoring" class="loading-shell fullscreen" aria-label="正在恢复登录状态"><span></span><span></span><span></span></div>
      <LoginView v-else-if="!session.session" />
      <DesktopShell v-else ref="shell" :has-player="Boolean(player.currentTrack)" :content-inert="player.lyricsOpen">
        <template #sidebar>
          <AppSidebar :inert="player.lyricsOpen || undefined" :user="session.session.user" :active="navigation.activeView" :playlists="library.playlists" @navigate="navigate" @playlist="openPlaylist" @create-playlist="newPlaylist" @logout="logout" />
        </template>

        <div v-if="pageLoading" key="loading" class="loading-shell" aria-label="正在加载内容"><span></span><span></span><span></span></div>
        <div v-else-if="homeFeedUnavailable" key="error" class="page-error"><p role="alert">{{ home.feedError }}</p><button class="primary-button" type="button" @click="home.load">重试</button></div>
        <div v-else-if="initialPageError" key="page-error" class="page-error"><p role="alert">{{ initialPageError }}</p><button class="primary-button" type="button" @click="retryCurrentPage">重试</button></div>
        <div v-else-if="pageReady" :key="`view-${currentEntry.id}`" class="page-content">
          <div v-if="visiblePageError" class="inline-error inline-error--action"><p role="alert">{{ visiblePageError }}</p><button v-if="pageRetryAvailable" type="button" class="bare-button" @click="retryCurrentPage">重试</button></div>

          <SearchView
            v-if="currentEntry.kind === 'search'"
            :query="currentEntry.query"
            :results-query="home.searchResultsQuery"
            :results="home.searchResults"
             :searching="home.searching"
            :loading-more="home.searchLoadingScope"
            :album-play-loading-id="albumPlayLoadingId"
             :current-id="player.currentTrack?.id"
             :is-playing="player.isPlaying"
            @play-track="playVisible"
            @toggle="player.toggle"
            @favorite="toggleFavorite"
            @add="openAddToPlaylist"
            @play-album="playAlbum"
            @open-album="openAlbum"
            @open-artist="openArtist"
            @load-more="home.loadMoreSearch"
          />

          <CollectionView
            v-else-if="detailOpen"
            :heading="library.heading"
            :tracks="library.tracks"
            :entries="library.selectedPlaylist?.entries"
            :playlist="library.selectedPlaylist"
            :album="currentEntry.kind === 'album' ? currentEntry.album : null"
             :artist="currentEntry.kind === 'artist' ? currentEntry.artist : null"
             :current-id="player.currentTrack?.id"
             :current-entry-id="currentPlaylistEntryId"
             :is-playing="player.isPlaying"
            :playlist-busy="library.playlistMutating"
            :play-all-loading="detailPlayAllLoading"
            :error="library.error"
            :reorder-disabled="Boolean(library.detailNextCursor)"
            :has-more="Boolean(library.detailNextCursor)"
            :loading-more="library.detailLoadingMore"
            :page-key="library.detailNextCursor"
            @back="goBack"
            @play-all="playCurrentCollection"
            @edit-playlist="editPlaylist"
            @play="(_track, index) => playAt(library.tracks, index)"
            @toggle="player.toggle"
            @favorite="toggleFavorite"
            @add="openAddToPlaylist"
            @remove="removeEntry"
            @remove-selected="removeEntries"
            @move="moveEntry"
            @reorder="reorderEntries"
            @load-more="library.loadMoreCollection"
          />

          <DiscoverView
            v-else-if="currentLibraryView === 'discover' && home.feed"
            :feed="home.feed"
            :random-albums="home.randomAlbums"
            :random-tracks="home.randomTracks"
            :random-albums-loading="home.randomAlbumsLoading"
            :random-tracks-loading="home.randomTracksLoading"
            :random-albums-error="home.randomAlbumsError"
            :random-tracks-error="home.randomTracksError"
            :album-play-loading-id="albumPlayLoadingId"
            :playlist-play-loading-id="playlistPlayLoadingId"
            :current-id="player.currentTrack?.id"
            :is-playing="player.isPlaying"
            @play-album="playAlbum"
            @open-album="openAlbum"
            @play-track="playDiscoveryTrack"
            @toggle="player.toggle"
            @favorite="toggleFavorite"
            @add="openAddToPlaylist"
            @play-playlist="playPlaylist"
            @open-playlist="openPlaylist"
            @retry-random-albums="home.loadRandomAlbums"
            @retry-random-tracks="home.loadRandomTracks"
          />

          <SettingsView
            v-else-if="currentLibraryView === 'settings'"
            :user="session.session.user"
            :server-config="session.serverConfig"
            :quality="player.quality"
            :crossfade-seconds="player.crossfadeSeconds"
            :notifications-enabled="player.notificationsEnabled"
            :theme="theme.theme"
            :theme-preference="theme.preference"
            :lyrics-font-scale="lyrics.fontScale"
            :lyrics-word-lyrics-enabled="lyrics.wordLyricsEnabled"
            :lyrics-text-color="lyricsTextColor"
            :lyrics-highlight-color="lyricsHighlightColor"
            :desktop-lyrics-visible="desktopLyrics.visible"
            :desktop-lyrics-locked="desktopLyrics.locked"
            :desktop-lyrics-fullscreen-behavior="desktopLyrics.fullscreenBehavior"
            :desktop-lyrics-font-scale="desktopLyrics.fontScale"
            :desktop-lyrics-text-color="desktopLyrics.textColor"
            :desktop-lyrics-highlight-color="desktopLyrics.highlightColor"
            :desktop-lyrics-word-lyrics-enabled="desktopLyrics.wordLyricsEnabled"
            :desktop-lyrics-show-translation="lyrics.showTranslation"
            :saving-profile="session.savingProfile"
            :uploading-avatar="session.uploadingAvatar"
            :switching-server="session.switchingServer"
            :error="session.error"
            @update:quality="player.quality = $event"
            @update:crossfade-seconds="player.crossfadeSeconds = $event"
            @update:notifications-enabled="player.notificationsEnabled = $event"
            @update:theme-preference="theme.setPreference"
            @update:lyrics-font-scale="lyrics.setFontScale"
            @update:lyrics-word-lyrics-enabled="lyrics.setWordLyricsEnabled"
            @update:lyrics-text-color="lyrics.setTextColor(theme.theme, $event)"
            @update:lyrics-highlight-color="lyrics.setHighlightColor(theme.theme, $event)"
            @update:desktop-lyrics-visible="desktopLyrics.setVisible"
            @update:desktop-lyrics-locked="desktopLyrics.setLocked"
            @update:desktop-lyrics-fullscreen-behavior="desktopLyrics.setFullscreenBehavior"
            @update:desktop-lyrics-font-scale="desktopLyrics.setFontScale"
            @update:desktop-lyrics-text-color="desktopLyrics.setTextColor"
            @update:desktop-lyrics-highlight-color="desktopLyrics.setHighlightColor"
            @update:desktop-lyrics-word-lyrics-enabled="desktopLyrics.setWordLyricsEnabled"
            @update:desktop-lyrics-show-translation="lyrics.setTranslationVisible"
            @update-profile="updateProfile"
            @upload-avatar="uploadAvatar"
            @switch-server="switchServer"
            @logout="logout"
            @logout-all="logoutAll"
          />

          <DiagnosticsView v-else-if="currentLibraryView === 'diagnostics'" :server-config="session.serverConfig" />

          <PlaylistsView
            v-else-if="currentLibraryView === 'playlists'"
            :playlists="library.playlists"
            :sort="library.playlistSort"
            :play-loading-id="playlistPlayLoadingId"
            :error="library.error"
            :has-more="Boolean(library.nextCursor)"
            :loading-more="library.loadingMore"
            :page-key="library.nextCursor"
            @open="openPlaylist"
            @play="playPlaylist"
            @edit="editPlaylist"
            @delete="deletePlaylist"
            @create="newPlaylist"
            @change-sort="library.changeSort"
            @load-more="library.loadMore"
          />
          <TracksView
            v-else-if="currentLibraryView === 'recent' || currentLibraryView === 'favorites'"
            :title="activeTrackViewCopy.title"
            :description="activeTrackViewCopy.description"
            :tracks="library.visibleTracks"
            :current-id="player.currentTrack?.id"
            :is-playing="player.isPlaying"
            :sort="currentLibraryView === 'favorites' ? library.favoriteSort : undefined"
            :error="library.error"
            :has-more="Boolean(library.nextCursor)"
            :loading-more="library.loadingMore"
            :page-key="library.nextCursor"
            @play="playVisible"
            @toggle="player.toggle"
            @favorite="toggleFavorite"
            @add="openAddToPlaylist"
            @change-sort="library.changeSort"
            @load-more="library.loadMore"
          />
        </div>
        <template #player><PlayerBar v-if="!player.lyricsOpen" @favorite="toggleFavorite" /></template>
        <template #overlays>
          <QueuePanel />
          <LyricsView :fullscreen="windowFullscreen" @favorite="toggleFavorite" />
          <PlaylistDialog :open="playlistDialogOpen" :playlist="editingPlaylist" :busy="dialogBusy" :error="dialogError" @close="playlistDialogOpen = false" @save="savePlaylist" />
          <AddToPlaylistDialog :track="addTrack" :playlists="addPlaylists" :busy="dialogBusy" :loading="addPlaylistsLoading" :retry-available="addPlaylistsRetryAvailable" :has-more="addPlaylistsHasMore" :error="addDialogError" @close="closeAddToPlaylist" @load-more="loadMoreAddPlaylists" @retry="retryAddPlaylists" @select="addToPlaylist" />
          <ToastHost :toasts="toast.messages" @dismiss="id => toast.dismiss(String(id))" />
        </template>
      </DesktopShell>
    </div>
  </div>
</template>
