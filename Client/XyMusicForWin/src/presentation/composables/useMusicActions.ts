import { ref, watch } from "vue";
import type { Album, Playlist, Track } from "../../domain/music";
import { useApplicationServices } from "../services";
import { useHomeStore } from "../stores/homeStore";
import { useLibraryStore } from "../stores/libraryStore";
import { useNavigationStore } from "../stores/navigationStore";
import { usePlayerStore } from "../stores/playerStore";
import { useToastStore } from "../stores/toastStore";
import { errorMessage } from "../utils/errorMessage";

interface CollectionSeed {
  tracks: Track[];
  nextCursor: string | null;
}

interface CollectionPage {
  tracks: Track[];
  nextCursor: string | null;
}

export function useMusicActions() {
  const services = useApplicationServices();
  const home = useHomeStore();
  const library = useLibraryStore();
  const navigation = useNavigationStore();
  const player = usePlayerStore();
  const toast = useToastStore();
  const actionError = ref("");
  const albumPlayLoadingId = ref("");
  const playlistPlayLoadingId = ref("");
  let collectionController: AbortController | null = null;
  let collectionRequest = 0;
  let collectionQueueRevision: number | null = null;
  let collectionPlaybackIntentRevision: number | null = null;
  let collectionStarted = false;
  const favoriteQueues = new Map<string, Promise<void>>();
  const favoriteIntents = new Map<string, boolean>();

  watch(() => player.queueVersion, (revision) => {
    if (collectionController && collectionQueueRevision !== null && revision !== collectionQueueRevision) cancelCollectionLoad();
  });
  watch(() => player.playbackIntentVersion, (revision) => {
    if (collectionController && !collectionStarted && collectionPlaybackIntentRevision !== null && revision !== collectionPlaybackIntentRevision) cancelCollectionLoad();
  });

  function play(track: Track, tracks: Track[]): void {
    cancelCollectionLoad();
    actionError.value = "";
    startPlayback(track, tracks);
  }

  function startPlayback(track: Track, tracks: Track[]): void { void player.play(track, tracks); }

  function playAt(tracks: Track[], index: number): void {
    cancelCollectionLoad();
    actionError.value = "";
    void player.playFromIndex(tracks, index);
  }

  function playVisible(track: Track): void {
    const detailOpen = ["album", "artist", "playlist"].includes(navigation.current.kind);
    const tracks = navigation.current.kind === "search"
      ? home.searchResults?.tracks ?? []
      : detailOpen ? library.tracks
        : library.tracks.length ? library.tracks : home.feed?.tracks ?? [];
    play(track, tracks);
  }

  function playDiscoveryTrack(track: Track): void {
    play(track, home.randomTracks.length ? home.randomTracks : home.feed?.tracks ?? []);
  }

  async function playAlbum(album: Album, seed?: CollectionSeed): Promise<void> {
    await playPagedCollection({
      kind: "album",
      id: album.id,
      emptyMessage: "该专辑暂无可播放歌曲",
      seed,
      loadPage: async (cursor, signal) => {
        const page = await services.catalog.albumTracksPage(album.id, cursor, COLLECTION_PAGE_SIZE, signal);
        return { tracks: page.items, nextCursor: page.nextCursor };
      },
    });
  }

  async function playPlaylist(playlist: Playlist, seed?: CollectionSeed): Promise<void> {
    await playPagedCollection({
      kind: "playlist",
      id: playlist.id,
      emptyMessage: "该歌单暂无歌曲",
      seed,
      loadPage: async (cursor, signal) => {
        const page = await services.playlists.getPage(playlist.id, cursor, COLLECTION_PAGE_SIZE, signal);
        return { tracks: page.entries.map((entry) => entry.track), nextCursor: page.nextCursor ?? null };
      },
    });
  }

  async function playPagedCollection(options: {
    kind: "album" | "playlist";
    id: string;
    emptyMessage: string;
    seed?: CollectionSeed;
    loadPage: (cursor: string | undefined, signal: AbortSignal) => Promise<CollectionPage>;
  }): Promise<void> {
    const { request, controller, queueVersion, playbackIntentVersion } = beginCollectionLoad(options.kind, options.id);
    let playbackStarted = false;
    let startedRevision: number | null = null;
    try {
      const first = options.seed ?? await options.loadPage(undefined, controller.signal);
      if (!isCurrentCollectionRequest(request, controller) || player.queueVersion !== queueVersion || player.playbackIntentVersion !== playbackIntentVersion) return;
      if (!first.tracks.length) {
        toast.show(options.emptyMessage, "info");
        return;
      }

      const started = player.startQueue(first.tracks, 0);
      if (!started || !isCurrentCollectionRequest(request, controller)) return;
      startedRevision = started.revision;
      collectionQueueRevision = started.revision;
      collectionStarted = true;
      player.setQueueExtending(started.revision, Boolean(first.nextCursor));
      const audioStarted = await started.playback;
      clearCollectionLoading(options.kind, options.id, request);
      if (!isCurrentCollectionRequest(request, controller) || !audioStarted) return;
      playbackStarted = true;

      let cursor = first.nextCursor;
      let pageCount = 1;
      let itemCount = first.tracks.length;
      const seenCursors = new Set<string>();
      while (cursor) {
        if (!isCurrentCollectionRequest(request, controller)) return;
        if (seenCursors.has(cursor)) throw new Error("服务器返回了重复的分页游标");
        if (pageCount >= MAX_COLLECTION_PAGES || itemCount >= MAX_COLLECTION_TRACKS) throw new Error("集合歌曲数量超过客户端安全上限");
        seenCursors.add(cursor);
        const page = await options.loadPage(cursor, controller.signal);
        if (!isCurrentCollectionRequest(request, controller)) return;
        itemCount += page.tracks.length;
        if (itemCount > MAX_COLLECTION_TRACKS) throw new Error("集合歌曲数量超过客户端安全上限");
        if (!player.appendToQueue(started.revision, page.tracks)) {
          controller.abort();
          return;
        }
        pageCount += 1;
        cursor = page.nextCursor;
      }
    } catch (cause) {
      if (controller.signal.aborted || request !== collectionRequest) return;
      if (playbackStarted) toast.show("已开始播放，但后续歌曲未能完整加入队列", "warning", 5200);
      else reportActionError(cause);
    } finally {
      if (startedRevision !== null) player.setQueueExtending(startedRevision, false);
      clearCollectionLoading(options.kind, options.id, request);
      if (collectionController === controller) collectionController = null;
    }
  }

  async function toggleFavorite(track: Track): Promise<void> {
    actionError.value = "";
    const favorite = !track.liked;
    favoriteIntents.set(track.id, favorite);
    track.liked = favorite;
    home.setFavorite(track.id, favorite);
    library.setFavorite(track.id, favorite);
    const previous = favoriteQueues.get(track.id) ?? Promise.resolve();
    const operation = previous.catch(() => undefined).then(() => services.library.favorite(track.id, favorite));
    favoriteQueues.set(track.id, operation);
    try {
      await operation;
      if (favoriteIntents.get(track.id) !== favorite) return;
      if (!favorite && library.activeView === "favorites") library.removeFavorite(track.id);
      toast.show(favorite ? "已添加到喜欢的音乐" : "已取消收藏", "success");
    } catch (cause) {
      if (favoriteIntents.get(track.id) !== favorite) return;
      track.liked = !favorite;
      home.setFavorite(track.id, !favorite);
      library.setFavorite(track.id, !favorite);
      reportActionError(cause);
    } finally {
      if (favoriteQueues.get(track.id) === operation) favoriteQueues.delete(track.id);
      if (favoriteIntents.get(track.id) === favorite) favoriteIntents.delete(track.id);
    }
  }

  function reportActionError(cause: unknown): void {
    actionError.value = errorMessage(cause);
    toast.show(actionError.value, "error", 4800);
  }

  function clearActionError(): void { actionError.value = ""; }

  function beginCollectionLoad(kind: "album" | "playlist", id: string): { request: number; controller: AbortController; queueVersion: number; playbackIntentVersion: number } {
    cancelCollectionLoad();
    actionError.value = "";
    const controller = new AbortController();
    collectionController = controller;
    collectionQueueRevision = player.queueVersion;
    collectionPlaybackIntentRevision = player.playbackIntentVersion;
    collectionStarted = false;
    if (kind === "album") albumPlayLoadingId.value = id;
    else playlistPlayLoadingId.value = id;
    return { request: collectionRequest, controller, queueVersion: player.queueVersion, playbackIntentVersion: player.playbackIntentVersion };
  }

  function cancelCollectionLoad(): void {
    collectionRequest += 1;
    collectionController?.abort();
    collectionController = null;
    collectionQueueRevision = null;
    collectionPlaybackIntentRevision = null;
    collectionStarted = false;
    albumPlayLoadingId.value = "";
    playlistPlayLoadingId.value = "";
  }

  function isCurrentCollectionRequest(request: number, controller: AbortController): boolean {
    return request === collectionRequest && collectionController === controller && !controller.signal.aborted;
  }

  function clearCollectionLoading(kind: "album" | "playlist", id: string, request: number): void {
    if (request !== collectionRequest) return;
    if (kind === "album" && albumPlayLoadingId.value === id) albumPlayLoadingId.value = "";
    if (kind === "playlist" && playlistPlayLoadingId.value === id) playlistPlayLoadingId.value = "";
  }

  return { actionError, albumPlayLoadingId, playlistPlayLoadingId, play, playAt, playVisible, playDiscoveryTrack, playAlbum, playPlaylist, toggleFavorite, reportActionError, clearActionError };
}

const COLLECTION_PAGE_SIZE = 100;
const MAX_COLLECTION_PAGES = 100;
const MAX_COLLECTION_TRACKS = 10_000;
