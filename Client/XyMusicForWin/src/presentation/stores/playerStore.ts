import { computed, onScopeDispose, ref, watch } from "vue";
import { defineStore } from "pinia";
import type { PlaybackQuality, Track } from "../../domain/music";
import { normalizeResumePosition, type RepeatMode, type PlayMode, derivePlayMode, splitPlayMode, cyclePlayMode as nextPlayMode } from "../../domain/playbackState";
import { keepCurrentTrack, nextTrackIndex, previousTrackIndex, removeTrackAtIndex, removeTrackFromQueue, selectTrack } from "../../domain/playbackQueue";
import type { DesktopIntegration, DesktopPlaybackState } from "../../application/ports/DesktopIntegration";
import type { Diagnostics } from "../../application/ports/Diagnostics";
import type { Notifier } from "../../application/ports/Notifier";
import type { PlayerPreferences } from "../../application/ports/PlayerPreferences";
import { DesktopMediaSessionCoordinator } from "../../application/services/DesktopMediaSessionCoordinator";
import { useApplicationServices } from "../services";
import { errorMessage } from "../utils/errorMessage";

type PlaybackTerminalEvent = "PAUSED" | "COMPLETED";

interface CapturedPlaybackSession {
  track: Track;
  sessionId: string;
  position: number;
  duration: number;
  started: boolean;
}

interface QueueStart {
  revision: number;
  playback: Promise<boolean>;
}

export const usePlayerStore = defineStore("player", () => {
  const services = useApplicationServices();
  const { audio: audioPlayer, playback } = services;
  const desktop = services.desktop ?? NOOP_DESKTOP;
  const diagnostics = services.diagnostics ?? NOOP_DIAGNOSTICS;
  const notifier = services.notifier ?? NOOP_NOTIFIER;
  const playerPreferences = services.playerPreferences ?? NOOP_PLAYER_PREFERENCES;
  const storedPreferences = playerPreferences.read();
  const desktopWindow = services.desktopWindow;
  const mediaSession = new DesktopMediaSessionCoordinator(desktop, (operation, cause) => {
    const fallback = operation === "metadata"
      ? "无法更新 Windows 媒体信息"
      : operation === "clear"
        ? "无法清除 Windows 媒体会话"
        : "无法更新 Windows 媒体播放状态";
    diagnostics.warn("media-session", errorMessage(cause, fallback));
  });
  const queue = ref<Track[]>([]);
  const queueRevision = ref(0);
  const queueVersion = computed(() => queueRevision.value);
  const playbackIntentRevision = ref(0);
  const playbackIntentVersion = computed(() => playbackIntentRevision.value);
  const currentIndex = ref(-1);
  const isPlaying = ref(false);
  const loading = ref(false);
  const progress = ref(0);
  const currentTime = ref(0);
  const duration = ref(0);
  const volume = ref(storedPreferences.volume);
  const queueOpen = ref(false);
  const lyricsOpen = ref(false);
  const shuffled = ref(false);
  const repeatMode = ref<RepeatMode>("off");
  const playMode = computed<PlayMode>(() => derivePlayMode(repeatMode.value, shuffled.value));
  const quality = ref<PlaybackQuality>(storedPreferences.quality);
  const hasStoredCrossfade = storedPreferences.hasCrossfadePreference;
  const crossfadeSeconds = ref(storedPreferences.crossfadeSeconds);
  const notificationsEnabled = ref(storedPreferences.notificationsEnabled);
  const miniMode = ref(false);
  const error = ref("");
  const currentTrack = computed(() => currentIndex.value >= 0 ? queue.value[currentIndex.value] : undefined);
  let loadRequest = 0;
  let playbackSessionId = crypto.randomUUID();
  let lastCheckpoint = 0;
  let loadController: AbortController | null = null;
  let lastNativePosition = -1;
  let playbackOwnerKey = "";
  let pendingResumeTrackId = "";
  let pendingResumePosition = 0;
  let lastPersistedPosition = -1;
  let lastSnapshotSavedAt = "";
  let prefetchController: AbortController | null = null;
  let prefetchedIndex = -1;
  let transitioning = false;
  let transitionActivated = false;
  let endedDuringTransition = false;
  let playbackSessionStarted = false;
  let persistTimer: number | null = null;
  let persistIdleCallback: number | null = null;
  let checkpointTimer: number | null = null;
  let checkpointIdleCallback: number | null = null;
  let persistSnapshotDirty = false;
  let removeMediaActions: (() => void) | undefined;
  let disposed = false;
  let lastNativeStatus: DesktopPlaybackState["status"] | "" = "";
  let extendingQueueRevision: number | null = null;
  let resumeWhenQueueExtends = false;

  const removeAudioUpdate = audioPlayer.onUpdate((snapshot) => {
    currentTime.value = snapshot.currentTime;
    if (Number.isFinite(snapshot.duration) && snapshot.duration > 0) duration.value = snapshot.duration;
    progress.value = snapshot.duration > 0 ? snapshot.currentTime / snapshot.duration * 100 : 0;
    isPlaying.value = !snapshot.paused;
    if (!snapshot.paused && snapshot.currentTime - lastCheckpoint >= 15 && currentTrack.value) {
      lastCheckpoint = snapshot.currentTime;
      void record("PROGRESS");
    }
    const nativeStatus = snapshot.paused ? "paused" : "playing";
    if (currentTrack.value && (nativeStatus !== lastNativeStatus || Math.abs(snapshot.currentTime - lastNativePosition) >= NATIVE_POSITION_INTERVAL_SECONDS)) {
      lastNativePosition = snapshot.currentTime;
      lastNativeStatus = nativeStatus;
      mediaSession.updatePlayback({
        status: nativeStatus,
        position: snapshot.currentTime,
        duration: snapshot.duration || duration.value,
      });
    }
    if (!loading.value && Math.abs(snapshot.currentTime - lastPersistedPosition) >= PERSIST_POSITION_INTERVAL_SECONDS) {
      scheduleProgressCheckpoint();
    }
    const remaining = snapshot.duration - snapshot.currentTime;
    if (crossfadeSeconds.value > 0 && remaining > 0 && remaining <= crossfadeSeconds.value && prefetchedIndex >= 0 && !transitioning && !snapshot.paused) {
      void activatePrefetched(crossfadeSeconds.value, "COMPLETED");
    }
  });
  const removeAudioEnded = audioPlayer.onEnded(() => { void handleEnded(); });
  const removeAudioError = audioPlayer.onError((message) => {
    error.value = message;
    isPlaying.value = false;
    lastNativeStatus = "stopped";
    mediaSession.updatePlayback({ status: "stopped", position: currentTime.value, duration: duration.value });
    diagnostics.error("playback", message);
  });
  audioPlayer.setVolume(volume.value / 100);
  watch(volume, (value) => {
    const normalized = Math.max(0, Math.min(100, Number(value)));
    audioPlayer.setVolume(normalized / 100);
    playerPreferences.writeVolume(normalized);
  });
  watch(quality, (value) => playerPreferences.writeQuality(value));
  watch([shuffled, repeatMode, quality], () => {
    schedulePersistState();
    refreshPrefetch();
  });
  watch(crossfadeSeconds, (value) => {
    playerPreferences.writeCrossfadeSeconds(normalizeCrossfade(value));
    schedulePersistState();
  });
  watch(notificationsEnabled, (value) => playerPreferences.writeNotificationsEnabled(value));
  void desktop.onMediaAction((action, position) => {
    if (action === "seek") {
      if (position !== undefined) seekTo(position);
      return;
    }
    if (action === "play" && !isPlaying.value) {
      if (currentTrack.value && audioPlayer.snapshot().duration <= 0) {
        playbackIntentRevision.value += 1;
        void playQueueIndex(currentIndex.value, null);
      }
      else void toggle();
    }
    if (action === "pause" && isPlaying.value) void toggle();
    if (action === "toggle") void toggle();
    if (action === "previous") void previous();
    if (action === "next") void next();
    if (action === "stop") { playbackIntentRevision.value += 1; stopPlayback(); }
  }).then((remove) => {
    if (disposed) remove();
    else removeMediaActions = remove;
  }).catch(() => undefined);
  window.addEventListener("pagehide", flushPersistState);

  function seed(tracks: Track[]) {
    if (queue.value.length || !tracks.length) return;
    advanceQueueRevision();
    queue.value = [...tracks];
  }

  function restoreState(ownerKey: string): boolean {
    cancelScheduledPersist();
    playbackOwnerKey = ownerKey;
    persistSnapshotDirty = false;
    lastSnapshotSavedAt = "";
    lastPersistedPosition = -1;
    playbackSessionStarted = false;
    const state = services.playbackState.restore(ownerKey);
    if (!state?.queue.length) return false;
    if (state.currentIndex < 0 || state.currentIndex >= state.queue.length) {
      services.playbackState.clear(ownerKey);
      return false;
    }
    advanceQueueRevision();
    queue.value = state.queue;
    currentIndex.value = state.currentIndex;
    shuffled.value = state.shuffled;
    repeatMode.value = state.repeatMode;
    quality.value = state.quality;
    if (!hasStoredCrossfade) crossfadeSeconds.value = state.crossfadeSeconds;
    const restoredPosition = normalizeResumePosition(state.position, currentTrack.value?.duration ?? 0);
    currentTime.value = restoredPosition;
    duration.value = currentTrack.value?.duration ?? 0;
    progress.value = duration.value > 0 ? currentTime.value / duration.value * 100 : 0;
    pendingResumeTrackId = currentTrack.value?.id ?? "";
    pendingResumePosition = restoredPosition;
    lastPersistedPosition = restoredPosition;
    lastSnapshotSavedAt = state.savedAt;
    return true;
  }

  async function play(track: Track, tracks: Track[] = queue.value) {
    playbackIntentRevision.value += 1;
    const selection = selectTrack(track, tracks);
    await startQueue(selection.tracks, selection.currentIndex)?.playback;
  }

  async function playAt(index: number): Promise<void> {
    playbackIntentRevision.value += 1;
    await playQueueIndex(index);
  }

  async function playFromIndex(
    tracks: Track[],
    index: number,
    terminalEvent: PlaybackTerminalEvent | null = "PAUSED",
  ): Promise<void> {
    playbackIntentRevision.value += 1;
    await startQueue(tracks, index, terminalEvent)?.playback;
  }

  function startQueue(
    tracks: Track[],
    index: number,
    terminalEvent: PlaybackTerminalEvent | null = "PAUSED",
  ): QueueStart | null {
    if (!Number.isInteger(index) || index < 0 || index >= tracks.length) return null;
    const playback = playSelection([...tracks], index, terminalEvent, true);
    return { revision: queueRevision.value, playback };
  }

  async function playQueueIndex(
    index: number,
    terminalEvent: PlaybackTerminalEvent | null = "PAUSED",
  ): Promise<void> {
    if (!Number.isInteger(index) || index < 0 || index >= queue.value.length) return;
    await playSelection(queue.value, index, terminalEvent, false);
  }

  async function playSelection(
    tracks: Track[],
    selectedIndex: number,
    terminalEvent: PlaybackTerminalEvent | null,
    replaceQueue: boolean,
  ): Promise<boolean> {
    resumeWhenQueueExtends = false;
    const previousSession = capturePlaybackSession();
    const request = ++loadRequest;
    loadController?.abort();
    clearPrefetch();
    const controller = new AbortController();
    loadController = controller;
    loading.value = true;
    error.value = "";
    finishPlaybackSession(previousSession, terminalEvent);
    audioPlayer.stop?.();
    if (replaceQueue) {
      advanceQueueRevision();
      queue.value = tracks;
    }
    currentIndex.value = selectedIndex;
    const selectedTrack = tracks[selectedIndex]!;
    if (selectedTrack.id !== pendingResumeTrackId) pendingResumePosition = 0;
    duration.value = selectedTrack.duration;
    currentTime.value = 0;
    progress.value = 0;
    isPlaying.value = false;
    const selectedSessionId = crypto.randomUUID();
    playbackSessionId = selectedSessionId;
    playbackSessionStarted = false;
    lastCheckpoint = 0;
    lastNativePosition = -1;
    mediaSession.updateMetadata({
      title: selectedTrack.title,
      artist: selectedTrack.artist,
      album: selectedTrack.album,
      ...(selectedTrack.coverUrl ? { artworkUrl: selectedTrack.coverUrl } : {}),
    });
    lastNativeStatus = "paused";
    lastNativePosition = 0;
    mediaSession.updatePlayback({ status: "paused", position: 0, duration: selectedTrack.duration });
    try {
      await loadTrackWithRetry(selectedTrack, controller, request);
      if (request !== loadRequest || controller.signal.aborted) return false;
      if (pendingResumePosition > 0 && selectedTrack.id === pendingResumeTrackId) {
        const playableDuration = audioPlayer.snapshot().duration || selectedTrack.duration;
        const resumePosition = normalizeResumePosition(pendingResumePosition, playableDuration);
        if (resumePosition > 0) audioPlayer.seek(resumePosition);
        currentTime.value = resumePosition;
      }
      pendingResumeTrackId = "";
      pendingResumePosition = 0;
      if (request !== loadRequest || controller.signal.aborted) return false;
      await audioPlayer.play();
      if (request !== loadRequest || controller.signal.aborted) return false;
      error.value = "";
      const startedAt = audioPlayer.snapshot().currentTime;
      playbackSessionStarted = true;
      void recordPlayback(selectedTrack, selectedSessionId, startedAt, "STARTED");
      announceTrack(selectedTrack);
      schedulePersistState();
      void prepareNext();
      return true;
    } catch (cause) {
      if (request === loadRequest && !controller.signal.aborted && !isAbortError(cause)) {
        error.value = errorMessage(cause, "无法播放该曲目");
        diagnostics.error("playback", `${selectedTrack.title}: ${error.value}`);
        mediaSession.updatePlayback({ status: "stopped", position: 0, duration: selectedTrack.duration });
      }
      return false;
    } finally {
      if (request === loadRequest) {
        loading.value = false;
        loadController = null;
      }
    }
  }

  function setPlayMode(mode: PlayMode): void {
    const { repeatMode: nextRepeat, shuffled: nextShuffled } = splitPlayMode(mode);
    // 互斥设置：确保 RepeatMode 与 shuffled 不会组合出 PlayMode 之外的歧义状态。
    repeatMode.value = nextRepeat;
    shuffled.value = nextShuffled;
  }

  function cyclePlayMode(): void {
    setPlayMode(nextPlayMode(playMode.value));
  }

  async function toggle() {
    playbackIntentRevision.value += 1;
    const track = currentTrack.value;
    if (!track || loading.value) return;
    if (isPlaying.value) {
      const cancelledTransition = transitioning;
      if (cancelledTransition) {
        loadRequest += 1;
        clearPrefetch();
      }
      audioPlayer.pause();
      if (cancelledTransition) void prepareNext();
      const pauseRecord = record("PAUSED");
      playbackSessionStarted = false;
      await pauseRecord;
      flushPersistState();
    }
    else if (audioPlayer.snapshot().duration <= 0) await playQueueIndex(currentIndex.value, null);
    else {
      const request = loadRequest;
      const sessionId = playbackSessionId;
      try {
        await audioPlayer.play();
        if (request !== loadRequest || currentTrack.value !== track || playbackSessionId !== sessionId) return;
        error.value = "";
        playbackSessionStarted = true;
        void recordPlayback(track, sessionId, audioPlayer.snapshot().currentTime, "STARTED");
      } catch (cause) {
        if (request === loadRequest && currentTrack.value === track && playbackSessionId === sessionId) {
          error.value = errorMessage(cause, "播放失败");
        }
      }
    }
  }

  async function next() {
    playbackIntentRevision.value += 1;
    if (!queue.value.length) return;
    if (repeatMode.value !== "one" && prefetchedIndex >= 0) { await activatePrefetched(0, "PAUSED"); return; }
    if (repeatMode.value === "one") clearPrefetch();
    const index = nextTrackIndex(queue.value.length, currentIndex.value, shuffled.value);
    await playQueueIndex(index);
  }

  async function previous() {
    playbackIntentRevision.value += 1;
    if (!queue.value.length) return;
    if (currentTime.value > 5) { seek(0); return; }
    const index = previousTrackIndex(queue.value.length, currentIndex.value);
    await playQueueIndex(index);
  }

  function seek(percent: number) {
    if (!currentTrack.value) return;
    const normalized = Math.max(0, Math.min(100, Number(percent)));
    seekTo(duration.value * normalized / 100);
  }

  function seekTo(seconds: number) {
    if (!currentTrack.value || loading.value || !Number.isFinite(seconds)) return;
    const cancelledTransition = transitioning;
    if (cancelledTransition) {
      loadRequest += 1;
      clearPrefetch();
    }
    const normalized = Math.max(0, Math.min(duration.value, seconds));
    audioPlayer.seek(normalized);
    currentTime.value = normalized;
    progress.value = duration.value > 0 ? normalized / duration.value * 100 : 0;
    lastCheckpoint = normalized;
    lastNativePosition = normalized;
    lastNativeStatus = isPlaying.value ? "playing" : "paused";
    mediaSession.updatePlayback({ status: lastNativeStatus, position: normalized, duration: duration.value });
    scheduleProgressCheckpoint();
    if (cancelledTransition) void prepareNext();
  }

  function removeFromQueue(trackId: string) {
    const selection = removeTrackFromQueue(queue.value, currentIndex.value, trackId);
    if (selection.tracks.length === queue.value.length) return;
    advanceQueueRevision();
    queue.value = selection.tracks;
    currentIndex.value = selection.currentIndex;
    schedulePersistState();
    refreshPrefetch();
  }

  function removeFromQueueAt(index: number) {
    const selection = removeTrackAtIndex(queue.value, currentIndex.value, index);
    if (selection.tracks.length === queue.value.length) return;
    advanceQueueRevision();
    queue.value = selection.tracks;
    currentIndex.value = selection.currentIndex;
    schedulePersistState();
    refreshPrefetch();
  }

  function clearQueue() {
    const selection = keepCurrentTrack(queue.value, currentIndex.value);
    if (selection.tracks.length === queue.value.length && selection.currentIndex === currentIndex.value) return;
    advanceQueueRevision();
    queue.value = selection.tracks;
    currentIndex.value = selection.currentIndex;
    schedulePersistState();
    refreshPrefetch();
  }

  function reset() {
    finishPlaybackSession(capturePlaybackSession(), "PAUSED");
    loadRequest += 1;
    loadController?.abort();
    loadController = null;
    clearPrefetch();
    audioPlayer.stop?.();
    advanceQueueRevision();
    queue.value = [];
    currentIndex.value = -1;
    isPlaying.value = false;
    loading.value = false;
    progress.value = 0;
    currentTime.value = 0;
    duration.value = 0;
    queueOpen.value = false;
    lyricsOpen.value = false;
    error.value = "";
    if (miniMode.value) void setMiniMode(false);
    lastNativePosition = -1;
    playbackOwnerKey = "";
    pendingResumeTrackId = "";
    pendingResumePosition = 0;
    lastNativeStatus = "";
    lastNativePosition = -1;
    cancelScheduledPersist();
    persistSnapshotDirty = false;
    lastSnapshotSavedAt = "";
    lastPersistedPosition = -1;
    playbackSessionStarted = false;
    mediaSession.clear();
  }

  function clearPersistedState() {
    cancelScheduledPersist();
    if (playbackOwnerKey) services.playbackState.clear(playbackOwnerKey);
    persistSnapshotDirty = false;
    lastSnapshotSavedAt = "";
    lastPersistedPosition = -1;
  }

  function toggleQueue() { queueOpen.value = !queueOpen.value; if (queueOpen.value) lyricsOpen.value = false; }
  function toggleLyrics() { lyricsOpen.value = !lyricsOpen.value; if (lyricsOpen.value) queueOpen.value = false; }

  async function setMiniMode(enabled: boolean): Promise<void> {
    if (enabled && !currentTrack.value) return;
    try {
      await desktopWindow?.setMiniMode(enabled);
      miniMode.value = enabled;
      if (enabled) { queueOpen.value = false; lyricsOpen.value = false; }
    } catch (cause) {
      diagnostics.error("window", errorMessage(cause, "切换迷你播放器模式失败"));
    }
  }

  async function handleEnded() {
    if (transitioning) {
      if (transitionActivated) endedDuringTransition = true;
      return;
    }
    if (prefetchedIndex >= 0) { await activatePrefetched(0, "COMPLETED"); return; }
    if (repeatMode.value === "one" && currentTrack.value) {
      await playQueueIndex(currentIndex.value, "COMPLETED");
    } else if (!shuffled.value && repeatMode.value === "off" && currentIndex.value >= queue.value.length - 1) {
      if (extendingQueueRevision === queueRevision.value) {
        finishPlaybackSession(capturePlaybackSession(), "COMPLETED");
        resumeWhenQueueExtends = true;
        isPlaying.value = false;
        currentTime.value = duration.value;
        progress.value = 100;
        lastNativeStatus = "paused";
        mediaSession.updatePlayback({ status: "paused", position: duration.value, duration: duration.value });
        schedulePersistState();
        return;
      }
      finishPlaybackSession(capturePlaybackSession(), "COMPLETED");
      stopPlayback(null);
    } else {
      const index = nextTrackIndex(queue.value.length, currentIndex.value, shuffled.value);
      await playQueueIndex(index, "COMPLETED");
    }
  }

  function stopPlayback(terminalEvent: PlaybackTerminalEvent | null = "PAUSED") {
    resumeWhenQueueExtends = false;
    finishPlaybackSession(capturePlaybackSession(), terminalEvent);
    loadRequest += 1;
    loadController?.abort();
    loadController = null;
    clearPrefetch();
    audioPlayer.stop?.();
    isPlaying.value = false;
    progress.value = 0;
    currentTime.value = 0;
    lastNativePosition = -1;
    lastNativeStatus = "stopped";
    mediaSession.updatePlayback({ status: "stopped", position: 0, duration: duration.value });
    flushPersistState();
  }

  async function record(event: "STARTED" | "PROGRESS" | "PAUSED" | "COMPLETED") {
    const track = currentTrack.value;
    if (!track) return;
    await recordPlayback(track, playbackSessionId, currentTime.value, event);
  }

  function capturePlaybackSession(): CapturedPlaybackSession | null {
    const track = currentTrack.value;
    if (!track) return null;
    return {
      track,
      sessionId: playbackSessionId,
      position: currentTime.value,
      duration: duration.value || track.duration,
      started: playbackSessionStarted,
    };
  }

  function finishPlaybackSession(session: CapturedPlaybackSession | null, event: PlaybackTerminalEvent | null): void {
    if (!session) return;
    playbackSessionStarted = false;
    if (!session.started || !event) return;
    const position = event === "COMPLETED" ? session.duration : session.position;
    void recordPlayback(session.track, session.sessionId, position, event);
  }

  async function recordPlayback(
    track: Track,
    sessionId: string,
    position: number,
    event: "STARTED" | "PROGRESS" | "PAUSED" | "COMPLETED",
  ): Promise<void> {
    await playback.record(track.id, sessionId, position * 1000, event).catch(() => undefined);
  }

  async function loadTrackWithRetry(track: Track, controller: AbortController, request: number): Promise<void> {
    let lastError: unknown;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      if (request !== loadRequest || controller.signal.aborted) throw controller.signal.reason ?? new DOMException("请求已取消", "AbortError");
      try {
        const grant = services.playbackGrants
          ? await services.playbackGrants.get(track.id, quality.value, controller.signal, attempt > 0)
          : await playback.grant(track.id, quality.value, controller.signal);
        await audioPlayer.load(grant.url, controller.signal);
        return;
      } catch (cause) {
        if (controller.signal.aborted || isAbortError(cause)) throw cause;
        lastError = cause;
        diagnostics.warn("playback", `${track.title}：第 ${attempt + 1} 次播放尝试失败`);
        services.playbackGrants?.invalidate(track.id, quality.value);
        if (attempt < 2) await retryDelay(RETRY_DELAYS[attempt]!, controller.signal);
      }
    }
    throw lastError;
  }

  async function prepareNext(): Promise<void> {
    if (!audioPlayer.preload || !queue.value.length) return;
    clearPrefetch();
    if (!shuffled.value && repeatMode.value === "off" && currentIndex.value >= queue.value.length - 1) return;
    const index = repeatMode.value === "one" && currentTrack.value
      ? currentIndex.value
      : nextTrackIndex(queue.value.length, currentIndex.value, shuffled.value);
    const track = queue.value[index];
    if (!track) return;
    const controller = new AbortController();
    prefetchController = controller;
    try {
      const grant = services.playbackGrants
        ? await services.playbackGrants.get(track.id, quality.value, controller.signal)
        : await playback.grant(track.id, quality.value, controller.signal);
      await audioPlayer.preload(grant.url, controller.signal);
      if (!controller.signal.aborted && prefetchController === controller) prefetchedIndex = index;
    } catch {
      if (prefetchController === controller) prefetchedIndex = -1;
    } finally {
      if (prefetchController === controller) prefetchController = null;
    }
  }

  async function activatePrefetched(fadeSeconds: number, terminalEvent: PlaybackTerminalEvent): Promise<void> {
    if (transitioning || prefetchedIndex < 0) return;
    const previousSession = capturePlaybackSession();
    const request = ++loadRequest;
    const index = prefetchedIndex;
    const track = queue.value[index];
    if (!track) { clearPrefetch(); return; }
    const nextSessionId = crypto.randomUUID();
    let switched = false;
    transitioning = true;
    transitionActivated = false;
    endedDuringTransition = false;
    prefetchedIndex = -1;
    const commitActivation = () => {
      if (switched || request !== loadRequest) return;
      switched = true;
      transitionActivated = true;
      const snapshot = audioPlayer.snapshot();
      finishPlaybackSession(previousSession, terminalEvent);
      currentIndex.value = index;
      duration.value = snapshot.duration || track.duration;
      currentTime.value = snapshot.currentTime;
      progress.value = duration.value > 0 ? currentTime.value / duration.value * 100 : 0;
      playbackSessionId = nextSessionId;
      playbackSessionStarted = true;
      lastCheckpoint = snapshot.currentTime;
      lastNativePosition = snapshot.currentTime;
      error.value = "";
      mediaSession.updateMetadata({
        title: track.title,
        artist: track.artist,
        album: track.album,
        ...(track.coverUrl ? { artworkUrl: track.coverUrl } : {}),
      });
      void recordPlayback(track, nextSessionId, snapshot.currentTime, "STARTED");
      announceTrack(track);
      lastNativeStatus = "playing";
      mediaSession.updatePlayback({ status: "playing", position: snapshot.currentTime, duration: duration.value });
      schedulePersistState();
    };
    try {
      const activated = await audioPlayer.activatePreloaded?.(fadeSeconds, commitActivation);
      if (request !== loadRequest) return;
      if (!activated) {
        const fallbackIndex = queue.value.indexOf(track);
        if (fallbackIndex >= 0) await playQueueIndex(fallbackIndex, terminalEvent);
        return;
      }
      commitActivation();
      if (!endedDuringTransition) void prepareNext();
    } catch (cause) {
      if (request !== loadRequest) return;
      error.value = errorMessage(cause, "切换下一首失败");
      diagnostics.error("playback", `${track.title}: ${error.value}`);
      if (!switched) {
        const fallbackIndex = queue.value.indexOf(track);
        if (fallbackIndex >= 0) await playQueueIndex(fallbackIndex, terminalEvent);
      }
    } finally {
      transitioning = false;
      transitionActivated = false;
      const shouldHandleEnded = endedDuringTransition && request === loadRequest;
      endedDuringTransition = false;
      if (shouldHandleEnded) void handleEnded();
    }
  }

  function clearPrefetch(): void {
    prefetchController?.abort();
    prefetchController = null;
    prefetchedIndex = -1;
    audioPlayer.clearPreloaded?.();
  }

  function refreshPrefetch(): void {
    clearPrefetch();
    if (currentTrack.value && audioPlayer.snapshot().duration > 0) void prepareNext();
  }

  function appendToQueue(revision: number, tracks: Track[]): boolean {
    if (revision !== queueRevision.value) return false;
    if (!tracks.length) return true;
    const previousLength = queue.value.length;
    const shouldPrepareNext = !resumeWhenQueueExtends
      && currentIndex.value === previousLength - 1
      && !transitioning
      && prefetchedIndex < 0
      && prefetchController === null
      && Boolean(currentTrack.value)
      && audioPlayer.snapshot().duration > 0;
    queue.value = [...queue.value, ...tracks];
    schedulePersistState();
    if (resumeWhenQueueExtends && currentIndex.value === previousLength - 1) {
      resumeWhenQueueExtends = false;
      void playQueueIndex(previousLength, null);
    } else if (shouldPrepareNext) {
      void prepareNext();
    }
    return true;
  }

  function setQueueExtending(revision: number, extending: boolean): void {
    if (revision !== queueRevision.value) return;
    extendingQueueRevision = extending ? revision : null;
    if (!extending && resumeWhenQueueExtends) {
      resumeWhenQueueExtends = false;
      stopPlayback(null);
    }
  }

  function advanceQueueRevision(): void {
    queueRevision.value += 1;
    extendingQueueRevision = null;
    resumeWhenQueueExtends = false;
  }

  function schedulePersistState(): void {
    if (!playbackOwnerKey) return;
    persistSnapshotDirty = true;
    cancelScheduledCheckpoint();
    if (persistTimer !== null || persistIdleCallback !== null) return;
    persistTimer = window.setTimeout(() => {
      persistTimer = null;
      persistIdleCallback = runWhenIdle(() => {
        persistIdleCallback = null;
        persistStateNow();
      });
    }, PERSIST_DEBOUNCE_MS);
  }

  function flushPersistState(): void {
    cancelScheduledPersist();
    if (persistSnapshotDirty) persistStateNow();
    else persistProgressNow();
  }

  function cancelScheduledPersist(): void {
    if (persistTimer !== null) window.clearTimeout(persistTimer);
    persistTimer = null;
    if (persistIdleCallback !== null) cancelIdleTask(persistIdleCallback);
    persistIdleCallback = null;
    cancelScheduledCheckpoint();
  }

  function cancelScheduledCheckpoint(): void {
    if (checkpointTimer !== null) window.clearTimeout(checkpointTimer);
    checkpointTimer = null;
    if (checkpointIdleCallback !== null) cancelIdleTask(checkpointIdleCallback);
    checkpointIdleCallback = null;
  }

  function scheduleProgressCheckpoint(): void {
    if (!playbackOwnerKey || !currentTrack.value || persistSnapshotDirty || !lastSnapshotSavedAt) return;
    lastPersistedPosition = currentTime.value;
    if (checkpointTimer !== null || checkpointIdleCallback !== null) return;
    checkpointTimer = window.setTimeout(() => {
      checkpointTimer = null;
      checkpointIdleCallback = runWhenIdle(() => {
        checkpointIdleCallback = null;
        persistProgressNow();
      });
    }, PERSIST_DEBOUNCE_MS);
  }

  function persistStateNow(): void {
    if (!playbackOwnerKey) return;
    const savedAt = new Date().toISOString();
    const snapshot = {
      ownerKey: playbackOwnerKey,
      queue: queue.value,
      currentIndex: currentIndex.value,
      position: currentTime.value,
      shuffled: shuffled.value,
      repeat: repeatMode.value === "one",
      repeatMode: repeatMode.value,
      quality: quality.value,
      crossfadeSeconds: crossfadeSeconds.value,
      savedAt,
    };
    try {
      services.playbackState.save(snapshot);
      persistSnapshotDirty = false;
      lastSnapshotSavedAt = savedAt;
      lastPersistedPosition = currentTime.value;
    } catch (cause) {
      const track = currentTrack.value;
      if (track && isQuotaExceeded(cause)) {
        try {
          services.playbackState.save({ ...snapshot, queue: [track], currentIndex: 0 });
          persistSnapshotDirty = false;
          lastSnapshotSavedAt = savedAt;
          lastPersistedPosition = currentTime.value;
          diagnostics.warn("playback-state", "Playback queue exceeded local storage quota; saved the current track only");
          return;
        } catch {
          // Report the original quota failure below.
        }
      }
      diagnostics.warn("playback-state", errorMessage(cause, "无法保存播放队列状态"));
    }
  }

  function persistProgressNow(): void {
    const track = currentTrack.value;
    if (!playbackOwnerKey || !track || persistSnapshotDirty || !lastSnapshotSavedAt) return;
    try {
      services.playbackState.checkpoint({
        ownerKey: playbackOwnerKey,
        currentIndex: currentIndex.value,
        trackId: track.id,
        position: currentTime.value,
        savedAt: new Date().toISOString(),
        snapshotSavedAt: lastSnapshotSavedAt,
      });
      lastPersistedPosition = currentTime.value;
    } catch (cause) {
      diagnostics.warn("playback-state", errorMessage(cause, "无法保存播放进度"));
    }
  }

  function announceTrack(track: Track): void {
    diagnostics.info("playback", `Now playing: ${track.title} - ${track.artist}`);
    if (!notificationsEnabled.value) return;
    void notifier.notify("正在播放", `${track.title} · ${track.artist}`).catch((cause) => {
      diagnostics.warn("notification", errorMessage(cause, "无法显示播放通知"));
    });
  }

  // 封面签名 URL 过期后，通过 catalog 重新获取 track 元数据刷新 coverUrl。
  // 触发场景：重启后恢复队列使用旧签名 URL，或会话停留时间超过 URL TTL。
  async function refreshCurrentTrackArtwork(): Promise<void> {
    const track = currentTrack.value;
    if (!track?.albumId) return;
    try {
      const tracks = await services.catalog.albumTracks(track.albumId);
      const fresh = tracks.find((item) => item.id === track.id);
      if (!fresh?.coverUrl || fresh.coverUrl === track.coverUrl) return;
      const index = queue.value.findIndex((item) => item.id === track.id);
      if (index < 0) return;
      // 用 splice 替换元素以触发 Vue 响应式更新，currentTrack computed 会自动派生新 coverUrl。
      queue.value.splice(index, 1, { ...queue.value[index]!, coverUrl: fresh.coverUrl });
    } catch {
      // 刷新失败时静默处理，ArtworkImage 已显示 fallback 图标。
    }
  }

  onScopeDispose(() => {
    flushPersistState();
    disposed = true;
    removeAudioUpdate();
    removeAudioEnded();
    removeAudioError();
    removeMediaActions?.();
    loadController?.abort();
    clearPrefetch();
    audioPlayer.stop?.();
    mediaSession.clear();
    cancelScheduledPersist();
    window.removeEventListener("pagehide", flushPersistState);
  });

  return { queue, queueVersion, playbackIntentVersion, currentIndex, currentTrack, isPlaying, loading, progress, currentTime, duration, volume, queueOpen, lyricsOpen, shuffled, repeatMode, playMode, quality, crossfadeSeconds, notificationsEnabled, miniMode, error, seed, restoreState, clearPersistedState, play, playAt, playFromIndex, startQueue, appendToQueue, setQueueExtending, toggle, next, previous, seek, seekTo, removeFromQueue, removeFromQueueAt, clearQueue, stopPlayback, reset, toggleQueue, toggleLyrics, setMiniMode, setPlayMode, cyclePlayMode, refreshCurrentTrackArtwork };
});

function normalizeCrossfade(value: number): number {
  return Number.isFinite(value) ? Math.max(0, Math.min(5, Math.round(value))) : 0;
}

function isAbortError(cause: unknown): boolean {
  return cause instanceof DOMException && cause.name === "AbortError";
}

function isQuotaExceeded(cause: unknown): boolean {
  if (!cause || typeof cause !== "object") return false;
  const error = cause as { name?: unknown; code?: unknown };
  return error.name === "QuotaExceededError" || error.code === 22 || error.code === 1014;
}

function runWhenIdle(callback: () => void): number | null {
  if (typeof window.requestIdleCallback === "function") {
    return window.requestIdleCallback(callback, { timeout: PERSIST_IDLE_TIMEOUT_MS });
  }
  callback();
  return null;
}

function cancelIdleTask(handle: number): void {
  if (handle >= 0 && typeof window.cancelIdleCallback === "function") window.cancelIdleCallback(handle);
}

const NOOP_DESKTOP: DesktopIntegration = {
  async onMediaAction() { return () => undefined; },
  async updateMediaMetadata() { return undefined; },
  async updateMediaPlayback() { return undefined; },
  async clearMediaSession() { return undefined; },
};

const NOOP_DIAGNOSTICS: Diagnostics = {
  info() {}, warn() {}, error() {}, entries: () => [], clear() {},
};

const NOOP_NOTIFIER: Notifier = {
  async notify() { return undefined; },
};

const NOOP_PLAYER_PREFERENCES: PlayerPreferences = {
  read: () => ({ volume: 72, quality: "AUTO", crossfadeSeconds: 0, notificationsEnabled: false, hasCrossfadePreference: false }),
  writeVolume() {},
  writeQuality() {},
  writeCrossfadeSeconds() {},
  writeNotificationsEnabled() {},
};

function retryDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    let timer = 0;
    const cleanup = () => signal.removeEventListener("abort", aborted);
    const completed = () => {
      cleanup();
      resolve();
    };
    const aborted = () => {
      window.clearTimeout(timer);
      cleanup();
      reject(signal.reason ?? new DOMException("请求已取消", "AbortError"));
    };
    if (signal.aborted) {
      aborted();
      return;
    }
    signal.addEventListener("abort", aborted, { once: true });
    timer = window.setTimeout(completed, milliseconds);
  });
}

const RETRY_DELAYS = [300, 900];
const NATIVE_POSITION_INTERVAL_SECONDS = 3;
const PERSIST_POSITION_INTERVAL_SECONDS = 15;
const PERSIST_DEBOUNCE_MS = 500;
const PERSIST_IDLE_TIMEOUT_MS = 1000;
