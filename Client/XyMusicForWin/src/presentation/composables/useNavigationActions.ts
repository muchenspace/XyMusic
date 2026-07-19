import { computed, nextTick, onMounted, onUnmounted, watch, type Ref } from "vue";
import type { Album, Artist, Playlist } from "../../domain/music";
import type { LibraryView } from "../../domain/navigation";
import { useHomeStore } from "../stores/homeStore";
import { useLibraryStore } from "../stores/libraryStore";
import { useNavigationStore, type NavigationEntry } from "../stores/navigationStore";

export interface ScrollContainer {
  scrollElement(): HTMLElement | null;
}

export function useNavigationActions(shell: Ref<ScrollContainer | null>) {
  const home = useHomeStore();
  const library = useLibraryStore();
  const navigation = useNavigationStore();
  const currentEntry = computed(() => navigation.current);
  const detailOpen = computed(() => ["album", "artist", "playlist"].includes(currentEntry.value.kind));
  const currentLibraryView = computed(() => currentEntry.value.kind === "library" ? currentEntry.value.view : navigation.activeView);
  const activeTrackViewCopy = computed(() => currentLibraryView.value === "favorites"
    ? { title: "喜欢的音乐", description: "集中查看已经收藏的歌曲。" }
    : { title: "最近播放", description: "继续收听最近播放过的歌曲。" });

  watch(() => home.searchResultsQuery, (resultsQuery) => {
    const entry = navigation.current;
    if (!resultsQuery || entry.kind !== "search" || resultsQuery !== entry.query.trim()) return;
    entry.scrollTop = 0;
    const element = shell.value?.scrollElement();
    if (element) element.scrollTop = 0;
  });

  function updateSearch(value: string): void {
    const wasSearch = currentEntry.value.kind === "search";
    home.updateSearch(value);
    if (value.trim()) {
      if (!wasSearch) {
        rememberScroll();
        navigation.showSearch(value);
        void restoreScroll(navigation.current);
      } else if (navigation.current.kind === "search") {
        navigation.current.query = value;
      }
    } else if (wasSearch) {
      navigation.replace({ kind: "library", view: library.activeView });
    }
  }

  async function navigate(view: LibraryView): Promise<void> {
    rememberScroll();
    home.updateSearch("");
    const entry = currentEntry.value.kind === "library" && currentEntry.value.view === view
      ? currentEntry.value
      : navigation.push({ kind: "library", view });
    await library.navigate(view);
    await restoreScroll(entry);
  }

  async function openAlbum(album: Album): Promise<void> {
    rememberScroll();
    const entry = navigation.push({ kind: "album", album, sourceView: navigation.activeView });
    await library.openAlbum(album);
    await restoreScroll(entry);
  }

  async function openArtist(artist: Artist): Promise<void> {
    rememberScroll();
    const entry = navigation.push({ kind: "artist", artist, sourceView: navigation.activeView });
    await library.openArtist(artist);
    await restoreScroll(entry);
  }

  async function openPlaylist(playlist: Playlist): Promise<void> {
    rememberScroll();
    const entry = navigation.push({ kind: "playlist", playlist, sourceView: "playlists" });
    await library.openPlaylist(playlist);
    await restoreScroll(entry);
  }

  async function goBack(): Promise<void> {
    rememberScroll();
    const entry = navigation.back();
    if (entry) await applyNavigation(entry);
  }

  async function goForward(): Promise<void> {
    rememberScroll();
    const entry = navigation.forward();
    if (entry) await applyNavigation(entry);
  }

  function rememberScroll(): void {
    navigation.rememberScroll(shell.value?.scrollElement()?.scrollTop ?? 0);
  }

  async function restoreScroll(entry: NavigationEntry): Promise<void> {
    await nextTick();
    const element = shell.value?.scrollElement();
    if (element) element.scrollTop = entry.scrollTop;
  }

  async function applyNavigation(entry: NavigationEntry): Promise<void> {
    if (entry.kind === "search") {
      if (home.search !== entry.query) home.updateSearch(entry.query);
    } else if (entry.kind === "library") {
      if (home.search) home.updateSearch("");
      await library.navigate(entry.view);
    } else if (entry.kind === "album") await library.openAlbum(entry.album);
    else if (entry.kind === "artist") await library.openArtist(entry.artist);
    else await library.openPlaylist(entry.playlist);
    await restoreScroll(entry);
  }

  function handleNavigationShortcut(event: KeyboardEvent): void {
    if (!event.altKey || event.defaultPrevented || event.isComposing) return;
    const target = event.target;
    if (target instanceof Element && target.closest("input, textarea, select, [contenteditable='true']")) return;
    if (document.querySelector('[aria-modal="true"]')) return;
    if (event.key === "ArrowLeft") { event.preventDefault(); void goBack(); }
    if (event.key === "ArrowRight") { event.preventDefault(); void goForward(); }
  }

  onMounted(() => window.addEventListener("keydown", handleNavigationShortcut));
  onUnmounted(() => window.removeEventListener("keydown", handleNavigationShortcut));

  return { currentEntry, detailOpen, currentLibraryView, activeTrackViewCopy, updateSearch, navigate, openAlbum, openArtist, openPlaylist, goBack, goForward };
}
