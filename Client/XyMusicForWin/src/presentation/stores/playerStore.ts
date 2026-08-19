import { computed, onScopeDispose, ref, shallowReactive, toRef } from "vue";
import { defineStore } from "pinia";
import type { PlaybackQuality, Track } from "../../domain/music";
import { derivePlayMode, type PlayMode } from "../../domain/playbackState";
import type { PlaybackQueue, PlaybackTerminalEvent } from "../../application/ports/PlaybackSession";
import { useApplicationServices } from "../services";

/**
 * Vue projection of the application playback session. The session publishes
 * immutable snapshots, while this store owns a mutable queue projection for
 * view-only state such as optimistic favorites. Scalar fields stay shallow so
 * progress ticks do not rerender queue and track consumers.
 */
export const usePlayerStore = defineStore("player", () => {
  const session = useApplicationServices().playbackSession;
  const playbackState = shallowReactive({ ...session.state() });
  const favoriteOverrides = new Map<string, boolean>();
  const queue = ref<Track[]>(projectQueue(playbackState.queue));
  let queueSource = playbackState.queue;
  const queueOpen = ref(false);
  const lyricsOpen = ref(false);
  const queueVersion = toRef(playbackState, "queueVersion");
  const playbackIntentVersion = toRef(playbackState, "playbackIntentVersion");
  const positionDiscontinuityVersion = toRef(playbackState, "positionDiscontinuityVersion");
  const currentIndex = toRef(playbackState, "currentIndex");
  const isPlaying = toRef(playbackState, "isPlaying");
  const loading = toRef(playbackState, "loading");
  const progress = toRef(playbackState, "progress");
  const currentTime = toRef(playbackState, "currentTime");
  const duration = toRef(playbackState, "duration");
  const shuffled = toRef(playbackState, "shuffled");
  const repeatMode = toRef(playbackState, "repeatMode");
  const currentTrack = computed(() => currentIndex.value >= 0 ? queue.value[currentIndex.value] : undefined);
  const playMode = computed<PlayMode>(() => derivePlayMode(repeatMode.value, shuffled.value));
  const volume = computed({
    get: () => playbackState.volume,
    set: (value: number) => session.setVolume(value),
  });
  const quality = computed({
    get: () => playbackState.quality,
    set: (value: PlaybackQuality) => session.setQuality(value),
  });
  const crossfadeSeconds = computed({
    get: () => playbackState.crossfadeSeconds,
    set: (value: number) => session.setCrossfadeSeconds(value),
  });
  const notificationsEnabled = computed({
    get: () => playbackState.notificationsEnabled,
    set: (value: boolean) => session.setNotificationsEnabled(value),
  });
  const miniMode = toRef(playbackState, "miniMode");
  const error = toRef(playbackState, "error");

  const unsubscribe = session.subscribe((state) => {
    if (state.queue !== queueSource) {
      queueSource = state.queue;
      queue.value = projectQueue(state.queue);
    }
    Object.assign(playbackState, state);
  });

  function setFavorite(trackId: string, favorite: boolean): void {
    favoriteOverrides.set(trackId, favorite);
    for (const track of queue.value) {
      if (track.id === trackId) track.liked = favorite;
    }
  }

  function projectQueue(source: PlaybackQueue): Track[] {
    return source.map((track) => ({
      ...track,
      artistIds: [...track.artistIds],
      liked: favoriteOverrides.get(track.id) ?? track.liked,
    }));
  }

  function toggleQueue(): void {
    queueOpen.value = !queueOpen.value;
    if (queueOpen.value) lyricsOpen.value = false;
  }

  function toggleLyrics(): void {
    lyricsOpen.value = !lyricsOpen.value;
    if (lyricsOpen.value) queueOpen.value = false;
  }

  function reset(): void {
    session.reset();
    favoriteOverrides.clear();
    queueOpen.value = false;
    lyricsOpen.value = false;
  }

  function restoreState(ownerKey: string): boolean {
    favoriteOverrides.clear();
    return session.restoreState(ownerKey);
  }

  async function setMiniMode(enabled: boolean): Promise<void> {
    await session.setMiniMode(enabled);
    if (enabled && session.state().miniMode) {
      queueOpen.value = false;
      lyricsOpen.value = false;
    }
  }

  function setPlayMode(mode: PlayMode): void {
    session.setPlayMode(mode);
  }

  function cyclePlayMode(): void {
    session.cyclePlayMode();
  }

  onScopeDispose(() => {
    unsubscribe();
  });

  return {
    queue,
    queueVersion,
    playbackIntentVersion,
    positionDiscontinuityVersion,
    currentIndex,
    currentTrack,
    isPlaying,
    loading,
    progress,
    currentTime,
    duration,
    volume,
    queueOpen,
    lyricsOpen,
    shuffled,
    repeatMode,
    playMode,
    quality,
    crossfadeSeconds,
    notificationsEnabled,
    miniMode,
    error,
    setFavorite,
    seed: (tracks: Track[]) => session.seed(tracks),
    restoreState,
    clearPersistedState: () => session.clearPersistedState(),
    flushPlayerPreferences: () => session.flushPlayerPreferences(),
    play: (track: Track, tracks?: Track[]) => session.play(track, tracks),
    playAt: (index: number) => session.playAt(index),
    playFromIndex: (tracks: Track[], index: number, terminalEvent?: PlaybackTerminalEvent | null) => session.playFromIndex(tracks, index, terminalEvent),
    startQueue: (tracks: Track[], index: number, terminalEvent?: PlaybackTerminalEvent | null) => session.startQueue(tracks, index, terminalEvent),
    appendToQueue: (revision: number, tracks: Track[]) => session.appendToQueue(revision, tracks),
    setQueueExtending: (revision: number, extending: boolean) => session.setQueueExtending(revision, extending),
    toggle: () => session.toggle(),
    next: () => session.next(),
    previous: () => session.previous(),
    seek: (percent: number) => session.seek(percent),
    seekTo: (seconds: number) => session.seekTo(seconds),
    removeFromQueue: (trackId: string) => session.removeFromQueue(trackId),
    removeFromQueueAt: (index: number) => session.removeFromQueueAt(index),
    clearQueue: () => session.clearQueue(),
    stopPlayback: (terminalEvent?: PlaybackTerminalEvent | null) => session.stopPlayback(terminalEvent),
    reset,
    toggleQueue,
    toggleLyrics,
    setMiniMode,
    setPlayMode,
    cyclePlayMode,
  };
});
