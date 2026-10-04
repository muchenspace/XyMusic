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

export function useMusicActions() {
  const services = useApplicationServices();
  const home = useHomeStore();
  const library = useLibraryStore();
  const navigation = useNavigationStore();
  const player = usePlayerStore();
  const toast = useToastStore();
  const queueLoading = services.queueLoading;
  const favorites = services.favorites;
  const actionError = ref("");
  const albumPlayLoadingId = ref("");
  const playlistPlayLoadingId = ref("");

  watch(() => player.queueVersion, (revision) => {
    if (queueLoading.handleQueueVersionChange(revision)) clearCollectionLoadingIds();
  });
  watch(() => player.playbackIntentVersion, (revision) => {
    if (queueLoading.handlePlaybackIntentChange(revision)) clearCollectionLoadingIds();
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
    cancelCollectionLoad();
    actionError.value = "";
    albumPlayLoadingId.value = album.id;
    const outcome = await queueLoading.playAlbum(album.id, {
      seed,
      onAudioSettled: () => { if (albumPlayLoadingId.value === album.id) albumPlayLoadingId.value = ""; },
    });
    if (outcome.kind === "empty" && outcome.current) toast.show("该专辑暂无可播放歌曲", "info");
    else if (outcome.kind === "partial" && outcome.current) toast.show("已开始播放，但后续歌曲未能完整加入队列", "warning", 5200);
    else if (outcome.kind === "failed" && outcome.current) reportActionError(outcome.cause);
    if (outcome.current && albumPlayLoadingId.value === album.id) albumPlayLoadingId.value = "";
  }

  async function playPlaylist(playlist: Playlist, seed?: CollectionSeed): Promise<void> {
    cancelCollectionLoad();
    actionError.value = "";
    playlistPlayLoadingId.value = playlist.id;
    const outcome = await queueLoading.playPlaylist(playlist.id, {
      seed,
      onAudioSettled: () => { if (playlistPlayLoadingId.value === playlist.id) playlistPlayLoadingId.value = ""; },
    });
    if (outcome.kind === "empty" && outcome.current) toast.show("该歌单暂无歌曲", "info");
    else if (outcome.kind === "partial" && outcome.current) toast.show("已开始播放，但后续歌曲未能完整加入队列", "warning", 5200);
    else if (outcome.kind === "failed" && outcome.current) reportActionError(outcome.cause);
    if (outcome.current && playlistPlayLoadingId.value === playlist.id) playlistPlayLoadingId.value = "";
  }

  async function toggleFavorite(track: Track): Promise<void> {
    actionError.value = "";
    const favorite = !track.liked;
    player.setFavorite(track.id, favorite);
    home.setFavorite(track.id, favorite);
    library.setFavorite(track.id, favorite);
    const outcome = await favorites.setFavorite(track.id, favorite);
    if (outcome.kind === "superseded") return;
    if (outcome.kind === "error") {
      player.setFavorite(track.id, !favorite);
      home.setFavorite(track.id, !favorite);
      library.setFavorite(track.id, !favorite);
      reportActionError(outcome.cause);
      return;
    }
    if (!favorite && library.activeView === "favorites") library.removeFavorite(track.id);
    toast.show(favorite ? "已添加到喜欢的音乐" : "已取消收藏", "success");
  }

  function reportActionError(cause: unknown): void {
    actionError.value = errorMessage(cause);
    toast.show(actionError.value, "error", 4800);
  }

  function clearActionError(): void { actionError.value = ""; }

  function cancelCollectionLoad(): void {
    queueLoading.cancel();
    clearCollectionLoadingIds();
  }

  function clearCollectionLoadingIds(): void {
    albumPlayLoadingId.value = "";
    playlistPlayLoadingId.value = "";
  }

  return { actionError, albumPlayLoadingId, playlistPlayLoadingId, play, playAt, playVisible, playDiscoveryTrack, playAlbum, playPlaylist, toggleFavorite, reportActionError, clearActionError };
}
