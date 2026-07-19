import { computed, ref } from "vue";
import type { Playlist, PlaylistVisibility, Track } from "../../domain/music";
import { useApplicationServices } from "../services";
import { useHomeStore } from "../stores/homeStore";
import { useLibraryStore } from "../stores/libraryStore";
import { useNavigationStore } from "../stores/navigationStore";
import { useToastStore } from "../stores/toastStore";
import { errorMessage } from "../utils/errorMessage";

export function usePlaylistActions(reportActionError: (cause: unknown) => void) {
  const services = useApplicationServices();
  const home = useHomeStore();
  const library = useLibraryStore();
  const navigation = useNavigationStore();
  const toast = useToastStore();
  const playlistDialogOpen = ref(false);
  const editingPlaylist = ref<Playlist>();
  const dialogBusy = ref(false);
  const dialogError = ref("");
  const addTrack = ref<Track | null>(null);
  const addDialogError = ref("");
  const addPlaylists = ref<Playlist[]>([]);
  const addPlaylistsLoading = ref(false);
  const addPlaylistsRetryAvailable = ref(false);
  const addPlaylistCursor = ref<string | null>(null);
  const addPlaylistsHasMore = computed(() => Boolean(addPlaylistCursor.value));
  let addPlaylistRequest = 0;
  let addPlaylistController: AbortController | null = null;
  let addPlaylistRetryReset = true;

  function newPlaylist(): void {
    editingPlaylist.value = undefined;
    dialogError.value = "";
    playlistDialogOpen.value = true;
  }

  function editPlaylist(playlist: Playlist): void {
    editingPlaylist.value = playlist;
    dialogError.value = "";
    playlistDialogOpen.value = true;
  }

  async function savePlaylist(value: { name: string; description: string; visibility: PlaylistVisibility }): Promise<void> {
    dialogBusy.value = true;
    dialogError.value = "";
    try {
      if (editingPlaylist.value) {
        await library.updatePlaylist(editingPlaylist.value, value.name, value.description, value.visibility);
        toast.show("歌单已更新", "success");
      } else {
        await library.createPlaylist(value.name, value.description, value.visibility);
        toast.show("歌单已创建", "success");
      }
      home.setPlaylists(library.playlists);
      playlistDialogOpen.value = false;
    } catch (cause) { dialogError.value = errorMessage(cause); }
    finally { dialogBusy.value = false; }
  }

  async function deletePlaylist(playlist: Playlist): Promise<void> {
    if (!window.confirm(`确定删除歌单“${playlist.title}”？`)) return;
    try {
      await library.deletePlaylist(playlist);
      home.setPlaylists(library.playlists);
      navigation.replace({ kind: "library", view: "playlists" });
      toast.show("歌单已删除", "success");
    } catch (cause) { reportActionError(cause); }
  }

  async function addToPlaylist(playlist: Playlist): Promise<void> {
    const track = addTrack.value;
    if (!track) return;
    dialogBusy.value = true;
    addDialogError.value = "";
    addPlaylistsRetryAvailable.value = false;
    try {
      await library.addTrack(playlist, track.id);
      home.setPlaylists(library.playlists);
      closeAddToPlaylist();
      toast.show("已添加到歌单", "success");
    } catch (cause) {
      addDialogError.value = errorMessage(cause);
      toast.show(addDialogError.value, "error", 4800);
    }
    finally { dialogBusy.value = false; }
  }

  function openAddToPlaylist(track: Track): void {
    cancelAddPlaylistLoad();
    addTrack.value = track;
    addDialogError.value = "";
    addPlaylists.value = [...library.playlists];
    addPlaylistCursor.value = null;
    void loadAddPlaylists(true);
  }

  function closeAddToPlaylist(): void {
    cancelAddPlaylistLoad();
    addTrack.value = null;
    addDialogError.value = "";
    addPlaylists.value = [];
    addPlaylistCursor.value = null;
  }

  async function loadMoreAddPlaylists(): Promise<void> {
    await loadAddPlaylists(false);
  }

  async function retryAddPlaylists(): Promise<void> {
    if (!addPlaylistsRetryAvailable.value) return;
    await loadAddPlaylists(addPlaylistRetryReset);
  }

  async function loadAddPlaylists(reset: boolean): Promise<void> {
    if (!addTrack.value || addPlaylistsLoading.value) return;
    const cursor = reset ? undefined : addPlaylistCursor.value ?? undefined;
    if (!reset && !cursor) return;
    const controller = new AbortController();
    addPlaylistController = controller;
    const request = ++addPlaylistRequest;
    addPlaylistsLoading.value = true;
    addPlaylistsRetryAvailable.value = false;
    addDialogError.value = "";
    if (reset) addPlaylistCursor.value = null;
    try {
      const page = await services.playlists.list("UPDATED_DESC", cursor, ADD_PLAYLIST_PAGE_SIZE, controller.signal);
      if (request !== addPlaylistRequest || controller.signal.aborted || !addTrack.value) return;
      addPlaylists.value = reset
        ? mergePlaylists(page.items, addPlaylists.value)
        : mergePlaylists(addPlaylists.value, page.items);
      addPlaylistCursor.value = page.nextCursor;
    } catch (cause) {
      if (request === addPlaylistRequest && !controller.signal.aborted) {
        addDialogError.value = errorMessage(cause, "加载歌单失败");
        addPlaylistRetryReset = reset;
        addPlaylistsRetryAvailable.value = true;
      }
    } finally {
      if (request === addPlaylistRequest) {
        addPlaylistsLoading.value = false;
        addPlaylistController = null;
      }
    }
  }

  function cancelAddPlaylistLoad(): void {
    addPlaylistRequest += 1;
    addPlaylistController?.abort();
    addPlaylistController = null;
    addPlaylistsLoading.value = false;
    addPlaylistsRetryAvailable.value = false;
  }

  async function removeEntry(entryId: string): Promise<void> {
    try {
      await library.removeEntry(entryId);
      home.setPlaylists(library.playlists);
      toast.show("已移出歌单", "success");
    }
    catch (cause) { reportActionError(cause); }
  }

  async function removeEntries(entryIds: string[]): Promise<void> {
    if (!entryIds.length || !window.confirm(`确定从歌单移除选中的 ${entryIds.length} 首歌曲？`)) return;
    try {
      const removed = await library.removeEntries(entryIds);
      home.setPlaylists(library.playlists);
      if (removed) toast.show(`已从歌单移除 ${removed} 首歌曲`, "success");
    } catch (cause) { reportActionError(cause); }
  }

  async function moveEntry(entryId: string, direction: -1 | 1): Promise<void> {
    try {
      await library.moveEntry(entryId, direction);
      home.setPlaylists(library.playlists);
    }
    catch (cause) { reportActionError(cause); }
  }

  async function reorderEntries(orderedEntryIds: string[]): Promise<void> {
    try {
      await library.reorderEntries(orderedEntryIds);
      home.setPlaylists(library.playlists);
    }
    catch (cause) { reportActionError(cause); }
  }

  function resetDialogs(): void {
    playlistDialogOpen.value = false;
    editingPlaylist.value = undefined;
    dialogBusy.value = false;
    dialogError.value = "";
    closeAddToPlaylist();
  }

  return { playlistDialogOpen, editingPlaylist, dialogBusy, dialogError, addTrack, addDialogError, addPlaylists, addPlaylistsLoading, addPlaylistsRetryAvailable, addPlaylistsHasMore, newPlaylist, editPlaylist, savePlaylist, deletePlaylist, openAddToPlaylist, closeAddToPlaylist, loadMoreAddPlaylists, retryAddPlaylists, addToPlaylist, removeEntry, removeEntries, moveEntry, reorderEntries, resetDialogs };
}

function mergePlaylists(primary: Playlist[], secondary: Playlist[]): Playlist[] {
  const merged = new Map(primary.map((playlist) => [playlist.id, playlist]));
  for (const playlist of secondary) if (!merged.has(playlist.id)) merged.set(playlist.id, playlist);
  return [...merged.values()];
}

const ADD_PLAYLIST_PAGE_SIZE = 100;
