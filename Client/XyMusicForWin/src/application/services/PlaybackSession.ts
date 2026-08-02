import type { AudioSnapshot, AudioPlayer } from "../ports/AudioPlayer";
import type { DesktopWindow } from "../ports/DesktopWindow";
import type { Diagnostics } from "../ports/Diagnostics";
import type { Notifier } from "../ports/Notifier";
import type { PageLifecycle } from "../ports/PageLifecycle";
import type {
  PlaybackSession as PlaybackSessionPort,
  PlaybackQueue,
  PlaybackSessionState,
  PlaybackTerminalEvent,
  QueueStart,
} from "../ports/PlaybackSession";
import type { SessionIdGenerator } from "../ports/SessionIdGenerator";
import type { TaskScheduler } from "../ports/TaskScheduler";
import type { PlaybackQuality, ReadonlyTrack, Track } from "../../domain/music";
import {
  cyclePlayMode as nextPlayMode,
  derivePlayMode,
  normalizeResumePosition,
  splitPlayMode,
  type PlayMode,
} from "../../domain/playbackState";
import {
  keepCurrentTrack,
  nextTrackIndex,
  previousTrackIndex,
  removeTrackAtIndex,
  removeTrackFromQueue,
  selectTrack,
} from "../../domain/playbackQueue";
import { errorMessage } from "../errors/errorMessage";
import type { PlaybackUseCases } from "../use-cases/PlaybackUseCases";
import type { PlaybackGrantCache } from "./PlaybackGrantCache";
import type { PlaybackDesktopIntegration, PlaybackDesktopStatus } from "./PlaybackDesktopIntegration";
import type { PlaybackPreferences } from "./PlaybackPreferences";
import type { PlaybackStatePersistence } from "./PlaybackStatePersistence";

interface CapturedPlaybackSession {
  track: ReadonlyTrack;
  sessionId: string;
  position: number;
  duration: number;
  started: boolean;
}

/**
 * Owns playback orchestration and exposes a low-frequency state projection for
 * presentation. Audio updates remain bounded by the AudioPlayer adapter.
 */
export class PlaybackSession implements PlaybackSessionPort {
  private readonly listeners = new Set<(state: PlaybackSessionState) => void>();
  private readonly removeAudioUpdate: () => void;
  private readonly removeAudioEnded: () => void;
  private readonly removeAudioError: () => void;
  private readonly removePageHide: () => void;
  private readonly hasStoredCrossfade: boolean;
  private stateValue: PlaybackSessionState;
  private loadRequest = 0;
  private playbackSessionId: string;
  private lastCheckpoint = 0;
  private loadController: AbortController | null = null;
  private lastNativePosition = -1;
  private pendingResumeTrackId = "";
  private pendingResumePosition = 0;
  private prefetchController: AbortController | null = null;
  private prefetchedIndex = -1;
  private transitioning = false;
  private transitionActivated = false;
  private endedDuringTransition = false;
  private playbackSessionStarted = false;
  private lastNativeStatus: PlaybackDesktopStatus | "" = "";
  private extendingQueueRevision: number | null = null;
  private resumeWhenQueueExtends = false;
  private miniModeOperation = 0;
  private miniModeRequestActive = false;
  private disposed = false;

  constructor(
    private readonly audio: AudioPlayer,
    private readonly playback: PlaybackUseCases,
    private readonly playbackGrants: PlaybackGrantCache,
    private readonly playbackPersistence: PlaybackStatePersistence,
    private readonly playbackPreferences: PlaybackPreferences,
    private readonly desktopPlayback: PlaybackDesktopIntegration,
    private readonly desktopWindow: DesktopWindow,
    private readonly diagnostics: Diagnostics,
    private readonly notifier: Notifier,
    private readonly scheduler: TaskScheduler,
    pageLifecycle: PageLifecycle,
    private readonly sessionIds: SessionIdGenerator,
  ) {
    const preferences = playbackPreferences.read();
    this.hasStoredCrossfade = preferences.hasCrossfadePreference;
    this.stateValue = Object.freeze({
      queue: this.ownQueue([]),
      queueVersion: 0,
      playbackIntentVersion: 0,
      currentIndex: -1,
      isPlaying: false,
      loading: false,
      progress: 0,
      currentTime: 0,
      duration: 0,
      volume: playbackPreferences.initializeVolume(preferences.volume),
      shuffled: false,
      repeatMode: "off",
      quality: preferences.quality,
      crossfadeSeconds: preferences.crossfadeSeconds,
      notificationsEnabled: preferences.notificationsEnabled,
      miniMode: false,
      error: "",
    });
    this.playbackSessionId = sessionIds.next();
    this.removeAudioUpdate = audio.onUpdate((snapshot) => this.handleAudioUpdate(snapshot));
    this.removeAudioEnded = audio.onEnded(() => { void this.handleEnded(); });
    this.removeAudioError = audio.onError((message) => this.handleAudioError(message));
    this.desktopPlayback.connect({
      play: () => this.handleDesktopPlay(),
      pause: () => { if (this.stateValue.isPlaying) void this.toggle(); },
      toggle: () => { void this.toggle(); },
      previous: () => { void this.previous(); },
      next: () => { void this.next(); },
      stop: () => { this.advancePlaybackIntent(); this.stopPlayback(); },
      seekTo: (seconds) => this.seekTo(seconds),
    });
    this.removePageHide = pageLifecycle.onPageHide(() => this.flush());
  }

  state(): PlaybackSessionState {
    return this.stateValue;
  }

  subscribe(listener: (state: PlaybackSessionState) => void): () => void {
    this.listeners.add(listener);
    listener(this.stateValue);
    return () => this.listeners.delete(listener);
  }

  seed(tracks: readonly ReadonlyTrack[]): void {
    if (this.disposed || this.stateValue.queue.length || !tracks.length) return;
    this.updateState({
      queue: this.ownQueue(tracks),
      queueVersion: this.advanceQueueRevision(),
    });
  }

  restoreState(ownerKey: string): boolean {
    if (this.disposed) return false;
    this.cancelActivePlaybackForRestore();
    this.playbackSessionStarted = false;
    const restored = this.playbackPersistence.restore(ownerKey);
    if (!restored?.queue.length) {
      this.clearPlaybackAfterRestoreFailure();
      return false;
    }
    if (restored.currentIndex < 0 || restored.currentIndex >= restored.queue.length) {
      this.playbackPersistence.clear();
      this.clearPlaybackAfterRestoreFailure();
      return false;
    }
    const queue = this.ownQueue(restored.queue);
    const track = queue[restored.currentIndex]!;
    const restoredPosition = normalizeResumePosition(restored.position, track.duration);
    const crossfadeSeconds = this.hasStoredCrossfade
      ? this.stateValue.crossfadeSeconds
      : this.playbackPreferences.setCrossfadeSeconds(restored.crossfadeSeconds);
    this.playbackPreferences.setQuality(restored.quality);
    this.pendingResumeTrackId = track.id;
    this.pendingResumePosition = restoredPosition;
    this.updateState({
      queue,
      queueVersion: this.advanceQueueRevision(),
      currentIndex: restored.currentIndex,
      isPlaying: false,
      loading: false,
      progress: track.duration > 0 ? restoredPosition / track.duration * 100 : 0,
      currentTime: restoredPosition,
      duration: track.duration,
      shuffled: restored.shuffled,
      repeatMode: restored.repeatMode,
      quality: restored.quality,
      crossfadeSeconds,
      error: "",
    });
    this.playbackPersistence.setRestoredPosition(restoredPosition, restored.savedAt);
    return true;
  }

  clearPersistedState(): void {
    this.playbackPersistence.clear();
  }

  clearGrants(): void {
    this.playbackGrants.clear();
  }

  flushPlayerPreferences(): void {
    this.playbackPreferences.flush();
  }

  async play(track: ReadonlyTrack, tracks?: readonly ReadonlyTrack[]): Promise<void> {
    if (this.disposed) return;
    this.advancePlaybackIntent();
    const selection = selectTrack(track, tracks ?? this.copyQueue(this.stateValue.queue));
    await this.startQueue(selection.tracks, selection.currentIndex)?.playback;
  }

  async playAt(index: number): Promise<void> {
    if (this.disposed) return;
    this.advancePlaybackIntent();
    await this.playQueueIndex(index);
  }

  async playFromIndex(
    tracks: readonly ReadonlyTrack[],
    index: number,
    terminalEvent: PlaybackTerminalEvent | null = "PAUSED",
  ): Promise<void> {
    if (this.disposed) return;
    this.advancePlaybackIntent();
    await this.startQueue(tracks, index, terminalEvent)?.playback;
  }

  startQueue(
    tracks: readonly ReadonlyTrack[],
    index: number,
    terminalEvent: PlaybackTerminalEvent | null = "PAUSED",
  ): QueueStart | null {
    if (this.disposed || !Number.isInteger(index) || index < 0 || index >= tracks.length) return null;
    const playback = this.playSelection(this.ownQueue(tracks), index, terminalEvent, true);
    return { revision: this.stateValue.queueVersion, playback };
  }

  async toggle(): Promise<void> {
    if (this.disposed) return;
    this.advancePlaybackIntent();
    const track = this.currentTrack;
    if (!track || this.stateValue.loading) return;
    if (this.stateValue.isPlaying) {
      const cancelledTransition = this.transitioning;
      if (cancelledTransition) {
        this.loadRequest += 1;
        this.clearPrefetch();
      }
      this.audio.pause();
      if (cancelledTransition) void this.prepareNext();
      const pauseRecord = this.record("PAUSED");
      this.playbackSessionStarted = false;
      await pauseRecord;
      this.flushPersistState();
      return;
    }
    if (this.audio.snapshot().duration <= 0) {
      await this.playQueueIndex(this.stateValue.currentIndex, null);
      return;
    }
    const request = this.loadRequest;
    const sessionId = this.playbackSessionId;
    try {
      await this.audio.play();
      if (request !== this.loadRequest || this.currentTrack !== track || this.playbackSessionId !== sessionId) return;
      this.updateState({ error: "" });
      this.playbackSessionStarted = true;
      void this.recordPlayback(track, sessionId, this.audio.snapshot().currentTime, "STARTED");
    } catch (cause) {
      if (request === this.loadRequest && this.currentTrack === track && this.playbackSessionId === sessionId) {
        this.updateState({ error: errorMessage(cause, "播放失败") });
      }
    }
  }

  async next(): Promise<void> {
    if (this.disposed) return;
    this.advancePlaybackIntent();
    if (!this.stateValue.queue.length) return;
    if (this.stateValue.repeatMode !== "one" && this.prefetchedIndex >= 0) {
      await this.activatePrefetched(0, "PAUSED");
      return;
    }
    if (this.stateValue.repeatMode === "one") this.clearPrefetch();
    const index = nextTrackIndex(
      this.stateValue.queue.length,
      this.stateValue.currentIndex,
      this.stateValue.shuffled,
    );
    await this.playQueueIndex(index);
  }

  async previous(): Promise<void> {
    if (this.disposed) return;
    this.advancePlaybackIntent();
    if (!this.stateValue.queue.length) return;
    if (this.stateValue.currentTime > 5) {
      this.seek(0);
      return;
    }
    const index = previousTrackIndex(this.stateValue.queue.length, this.stateValue.currentIndex);
    await this.playQueueIndex(index);
  }

  seek(percent: number): void {
    if (!this.currentTrack) return;
    const normalized = Math.max(0, Math.min(100, Number(percent)));
    this.seekTo(this.stateValue.duration * normalized / 100);
  }

  seekTo(seconds: number): void {
    if (this.disposed || !this.currentTrack || this.stateValue.loading || !Number.isFinite(seconds)) return;
    const cancelledTransition = this.transitioning;
    if (cancelledTransition) {
      this.loadRequest += 1;
      this.clearPrefetch();
    }
    const normalized = Math.max(0, Math.min(this.stateValue.duration, seconds));
    this.audio.seek(normalized);
    this.lastCheckpoint = normalized;
    this.lastNativePosition = normalized;
    this.lastNativeStatus = this.stateValue.isPlaying ? "playing" : "paused";
    this.updateState({
      currentTime: normalized,
      progress: this.stateValue.duration > 0 ? normalized / this.stateValue.duration * 100 : 0,
    });
    this.desktopPlayback.setPlayback(this.lastNativeStatus, normalized, this.stateValue.duration);
    this.scheduleProgressCheckpoint();
    if (cancelledTransition) void this.prepareNext();
  }

  removeFromQueue(trackId: string): void {
    if (this.disposed) return;
    const selection = removeTrackFromQueue(this.stateValue.queue, this.stateValue.currentIndex, trackId);
    if (selection.tracks.length === this.stateValue.queue.length) return;
    this.updateState({
      queue: this.ownQueue(selection.tracks),
      queueVersion: this.advanceQueueRevision(),
      currentIndex: selection.currentIndex,
    });
    this.schedulePersistState();
    this.refreshPrefetch();
  }

  removeFromQueueAt(index: number): void {
    if (this.disposed) return;
    const selection = removeTrackAtIndex(this.stateValue.queue, this.stateValue.currentIndex, index);
    if (selection.tracks.length === this.stateValue.queue.length) return;
    this.updateState({
      queue: this.ownQueue(selection.tracks),
      queueVersion: this.advanceQueueRevision(),
      currentIndex: selection.currentIndex,
    });
    this.schedulePersistState();
    this.refreshPrefetch();
  }

  clearQueue(): void {
    if (this.disposed) return;
    const selection = keepCurrentTrack(this.stateValue.queue, this.stateValue.currentIndex);
    if (selection.tracks.length === this.stateValue.queue.length && selection.currentIndex === this.stateValue.currentIndex) return;
    this.updateState({
      queue: this.ownQueue(selection.tracks),
      queueVersion: this.advanceQueueRevision(),
      currentIndex: selection.currentIndex,
    });
    this.schedulePersistState();
    this.refreshPrefetch();
  }

  stopPlayback(terminalEvent: PlaybackTerminalEvent | null = "PAUSED"): void {
    if (this.disposed) return;
    this.resumeWhenQueueExtends = false;
    this.finishPlaybackSession(this.capturePlaybackSession(), terminalEvent);
    this.loadRequest += 1;
    this.loadController?.abort();
    this.loadController = null;
    this.clearPrefetch();
    this.audio.stop();
    this.lastNativePosition = -1;
    this.lastNativeStatus = "stopped";
    this.updateState({ isPlaying: false, loading: false, progress: 0, currentTime: 0 });
    this.desktopPlayback.setPlayback("stopped", 0, this.stateValue.duration);
    this.flushPersistState();
  }

  reset(): void {
    if (this.disposed) return;
    this.finishPlaybackSession(this.capturePlaybackSession(), "PAUSED");
    this.loadRequest += 1;
    this.loadController?.abort();
    this.loadController = null;
    this.clearPrefetch();
    this.audio.stop();
    const shouldExitMiniMode = this.stateValue.miniMode || this.miniModeRequestActive;
    this.miniModeOperation += 1;
    this.miniModeRequestActive = false;
    if (shouldExitMiniMode) void this.disableMiniModeAfterReset();
    this.pendingResumeTrackId = "";
    this.pendingResumePosition = 0;
    this.lastNativeStatus = "";
    this.lastNativePosition = -1;
    this.playbackSessionStarted = false;
    this.playbackPersistence.detach();
    this.updateState({
      queue: this.ownQueue([]),
      queueVersion: this.advanceQueueRevision(),
      currentIndex: -1,
      isPlaying: false,
      loading: false,
      progress: 0,
      currentTime: 0,
      duration: 0,
      miniMode: false,
      error: "",
    });
    this.desktopPlayback.clear();
  }

  async setMiniMode(enabled: boolean): Promise<void> {
    if (this.disposed || (enabled && !this.currentTrack)) return;
    const operation = ++this.miniModeOperation;
    this.miniModeRequestActive = true;
    try {
      await this.desktopWindow.setMiniMode(enabled);
      if (this.disposed || operation !== this.miniModeOperation) return;
      this.miniModeRequestActive = false;
      if (enabled && !this.currentTrack) return;
      this.updateState({ miniMode: enabled });
    } catch (cause) {
      if (this.disposed || operation !== this.miniModeOperation) return;
      this.miniModeRequestActive = false;
      this.diagnostics.error("window", errorMessage(cause, "切换迷你播放器模式失败"));
    }
  }

  setPlayMode(mode: PlayMode): void {
    if (this.disposed) return;
    const { repeatMode, shuffled } = splitPlayMode(mode);
    this.updateState({ repeatMode, shuffled });
    this.schedulePersistState();
    this.refreshPrefetch();
  }

  cyclePlayMode(): void {
    this.setPlayMode(nextPlayMode(derivePlayMode(this.stateValue.repeatMode, this.stateValue.shuffled)));
  }

  setVolume(value: number): void {
    if (this.disposed) return;
    this.updateState({ volume: this.playbackPreferences.setVolume(value) });
  }

  setQuality(value: PlaybackQuality): void {
    if (this.disposed) return;
    this.playbackPreferences.setQuality(value);
    this.updateState({ quality: value });
    this.schedulePersistState();
    this.refreshPrefetch();
  }

  setCrossfadeSeconds(value: number): void {
    if (this.disposed) return;
    this.updateState({ crossfadeSeconds: this.playbackPreferences.setCrossfadeSeconds(value) });
    this.schedulePersistState();
  }

  setNotificationsEnabled(value: boolean): void {
    if (this.disposed) return;
    this.playbackPreferences.setNotificationsEnabled(value);
    this.updateState({ notificationsEnabled: value });
  }

  appendToQueue(revision: number, tracks: readonly ReadonlyTrack[]): boolean {
    if (this.disposed || revision !== this.stateValue.queueVersion) return false;
    if (!tracks.length) return true;
    const previousLength = this.stateValue.queue.length;
    const shouldPrepareNext = !this.resumeWhenQueueExtends
      && this.stateValue.currentIndex === previousLength - 1
      && !this.transitioning
      && this.prefetchedIndex < 0
      && this.prefetchController === null
      && Boolean(this.currentTrack)
      && this.audio.snapshot().duration > 0;
    this.updateState({ queue: this.ownQueue([...this.stateValue.queue, ...tracks]) });
    this.schedulePersistState();
    if (this.resumeWhenQueueExtends && this.stateValue.currentIndex === previousLength - 1) {
      this.resumeWhenQueueExtends = false;
      void this.playQueueIndex(previousLength, null);
    } else if (shouldPrepareNext) {
      void this.prepareNext();
    }
    return true;
  }

  setQueueExtending(revision: number, extending: boolean): void {
    if (this.disposed || revision !== this.stateValue.queueVersion) return;
    this.extendingQueueRevision = extending ? revision : null;
    if (!extending && this.resumeWhenQueueExtends) {
      this.resumeWhenQueueExtends = false;
      this.stopPlayback(null);
    }
  }

  dispose(): void {
    if (this.disposed) return;
    this.flush();
    this.disposed = true;
    this.miniModeOperation += 1;
    this.miniModeRequestActive = false;
    this.removePageHide();
    this.removeAudioUpdate();
    this.removeAudioEnded();
    this.removeAudioError();
    this.loadController?.abort();
    this.clearPrefetch();
    this.audio.stop();
    this.desktopPlayback.dispose();
    this.playbackPersistence.dispose();
    this.playbackPreferences.dispose();
    this.listeners.clear();
  }

  private get currentTrack(): ReadonlyTrack | undefined {
    const { queue, currentIndex } = this.stateValue;
    return currentIndex >= 0 ? queue[currentIndex] : undefined;
  }

  private handleDesktopPlay(): void {
    if (this.stateValue.isPlaying) return;
    if (this.currentTrack && this.audio.snapshot().duration <= 0) {
      this.advancePlaybackIntent();
      void this.playQueueIndex(this.stateValue.currentIndex, null);
      return;
    }
    void this.toggle();
  }

  private cancelActivePlaybackForRestore(): void {
    this.loadRequest += 1;
    this.loadController?.abort();
    this.loadController = null;
    this.clearPrefetch();
    this.updateState({ loading: true });
    this.finishPlaybackSession(this.capturePlaybackSession(), "PAUSED");
    this.audio.stop();
    this.desktopPlayback.clear();
    this.pendingResumeTrackId = "";
    this.pendingResumePosition = 0;
  }

  private clearPlaybackAfterRestoreFailure(): void {
    this.lastCheckpoint = 0;
    this.lastNativeStatus = "";
    this.lastNativePosition = -1;
    this.playbackSessionStarted = false;
    this.updateState({
      queue: this.ownQueue([]),
      queueVersion: this.advanceQueueRevision(),
      currentIndex: -1,
      isPlaying: false,
      loading: false,
      progress: 0,
      currentTime: 0,
      duration: 0,
      error: "",
    });
  }

  private async disableMiniModeAfterReset(): Promise<void> {
    try {
      await this.desktopWindow.setMiniMode(false);
    } catch (cause) {
      this.diagnostics.error("window", errorMessage(cause, "切换迷你播放器模式失败"));
    }
  }

  private handleAudioUpdate(snapshot: AudioSnapshot): void {
    if (this.disposed) return;
    const currentTrack = this.currentTrack;
    const duration = Number.isFinite(snapshot.duration) && snapshot.duration > 0
      ? snapshot.duration
      : this.stateValue.duration;
    this.updateState({
      currentTime: snapshot.currentTime,
      duration,
      progress: snapshot.duration > 0 ? snapshot.currentTime / snapshot.duration * 100 : 0,
      isPlaying: !snapshot.paused,
    });
    if (!snapshot.paused && snapshot.currentTime - this.lastCheckpoint >= 15 && currentTrack) {
      this.lastCheckpoint = snapshot.currentTime;
      void this.record("PROGRESS");
    }
    const nativeStatus: PlaybackDesktopStatus = snapshot.paused ? "paused" : "playing";
    if (currentTrack && (nativeStatus !== this.lastNativeStatus || Math.abs(snapshot.currentTime - this.lastNativePosition) >= NATIVE_POSITION_INTERVAL_SECONDS)) {
      this.lastNativePosition = snapshot.currentTime;
      this.lastNativeStatus = nativeStatus;
      this.desktopPlayback.setPlayback(nativeStatus, snapshot.currentTime, snapshot.duration || duration);
    }
    if (!this.stateValue.loading && Math.abs(snapshot.currentTime - this.playbackPersistence.persistedPosition) >= PERSIST_POSITION_INTERVAL_SECONDS) {
      this.scheduleProgressCheckpoint();
    }
    const remaining = snapshot.duration - snapshot.currentTime;
    if (this.stateValue.crossfadeSeconds > 0
      && remaining > 0
      && remaining <= this.stateValue.crossfadeSeconds
      && this.prefetchedIndex >= 0
      && !this.transitioning
      && !snapshot.paused) {
      void this.activatePrefetched(this.stateValue.crossfadeSeconds, "COMPLETED");
    }
  }

  private handleAudioError(message: string): void {
    if (this.disposed) return;
    this.lastNativeStatus = "stopped";
    this.updateState({ error: message, isPlaying: false });
    this.desktopPlayback.setPlayback("stopped", this.stateValue.currentTime, this.stateValue.duration);
    this.diagnostics.error("playback", message);
  }

  private async playQueueIndex(index: number, terminalEvent: PlaybackTerminalEvent | null = "PAUSED"): Promise<void> {
    if (!Number.isInteger(index) || index < 0 || index >= this.stateValue.queue.length) return;
    await this.playSelection(this.stateValue.queue, index, terminalEvent, false);
  }

  private async playSelection(
    tracks: PlaybackQueue,
    selectedIndex: number,
    terminalEvent: PlaybackTerminalEvent | null,
    replaceQueue: boolean,
  ): Promise<boolean> {
    this.resumeWhenQueueExtends = false;
    const previousSession = this.capturePlaybackSession();
    const request = ++this.loadRequest;
    this.loadController?.abort();
    this.clearPrefetch();
    const controller = new AbortController();
    this.loadController = controller;
    // HtmlAudioPlayer.stop() emits synchronously; mark the replacement active
    // first so that stopped old-track updates cannot checkpoint position zero.
    this.updateState({ loading: true, error: "" });
    this.finishPlaybackSession(previousSession, terminalEvent);
    this.audio.stop();
    const selectedTrack = tracks[selectedIndex]!;
    if (selectedTrack.id !== this.pendingResumeTrackId) this.pendingResumePosition = 0;
    const queueVersion = replaceQueue ? this.advanceQueueRevision() : this.stateValue.queueVersion;
    this.updateState({
      ...(replaceQueue ? { queue: tracks, queueVersion } : {}),
      currentIndex: selectedIndex,
      isPlaying: false,
      progress: 0,
      currentTime: 0,
      duration: selectedTrack.duration,
    });
    const selectedSessionId = this.sessionIds.next();
    this.playbackSessionId = selectedSessionId;
    this.playbackSessionStarted = false;
    this.lastCheckpoint = 0;
    this.lastNativePosition = -1;
    this.desktopPlayback.setTrack(selectedTrack);
    this.lastNativeStatus = "paused";
    this.lastNativePosition = 0;
    this.desktopPlayback.setPlayback("paused", 0, selectedTrack.duration);
    try {
      await this.loadTrackWithRetry(selectedTrack, controller, request);
      if (request !== this.loadRequest || controller.signal.aborted) return false;
      if (this.pendingResumePosition > 0 && selectedTrack.id === this.pendingResumeTrackId) {
        const playableDuration = this.audio.snapshot().duration || selectedTrack.duration;
        const resumePosition = normalizeResumePosition(this.pendingResumePosition, playableDuration);
        if (resumePosition > 0) this.audio.seek(resumePosition);
        this.updateState({
          currentTime: resumePosition,
          progress: playableDuration > 0 ? resumePosition / playableDuration * 100 : 0,
        });
      }
      this.pendingResumeTrackId = "";
      this.pendingResumePosition = 0;
      if (request !== this.loadRequest || controller.signal.aborted) return false;
      await this.audio.play();
      if (request !== this.loadRequest || controller.signal.aborted) return false;
      const startedAt = this.audio.snapshot().currentTime;
      this.playbackSessionStarted = true;
      this.updateState({ error: "" });
      void this.recordPlayback(selectedTrack, selectedSessionId, startedAt, "STARTED");
      this.announceTrack(selectedTrack);
      this.schedulePersistState();
      void this.prepareNext();
      return true;
    } catch (cause) {
      if (request === this.loadRequest && !controller.signal.aborted && !isAbortError(cause)) {
        const message = errorMessage(cause, "无法播放该曲目");
        this.updateState({ error: message });
        this.diagnostics.error("playback", `${selectedTrack.title}: ${message}`);
        this.desktopPlayback.setPlayback("stopped", 0, selectedTrack.duration);
      }
      return false;
    } finally {
      if (request === this.loadRequest) {
        this.loadController = null;
        this.updateState({ loading: false });
      }
    }
  }

  private async handleEnded(): Promise<void> {
    if (this.disposed) return;
    if (this.transitioning) {
      if (this.transitionActivated) this.endedDuringTransition = true;
      return;
    }
    if (this.prefetchedIndex >= 0) {
      await this.activatePrefetched(0, "COMPLETED");
      return;
    }
    if (this.stateValue.repeatMode === "one" && this.currentTrack) {
      await this.playQueueIndex(this.stateValue.currentIndex, "COMPLETED");
      return;
    }
    if (!this.stateValue.shuffled
      && this.stateValue.repeatMode === "off"
      && this.stateValue.currentIndex >= this.stateValue.queue.length - 1) {
      if (this.extendingQueueRevision === this.stateValue.queueVersion) {
        this.finishPlaybackSession(this.capturePlaybackSession(), "COMPLETED");
        this.resumeWhenQueueExtends = true;
        this.lastNativeStatus = "paused";
        this.updateState({
          isPlaying: false,
          currentTime: this.stateValue.duration,
          progress: 100,
        });
        this.desktopPlayback.setPlayback("paused", this.stateValue.duration, this.stateValue.duration);
        this.schedulePersistState();
        return;
      }
      this.finishPlaybackSession(this.capturePlaybackSession(), "COMPLETED");
      this.stopPlayback(null);
      return;
    }
    const index = nextTrackIndex(
      this.stateValue.queue.length,
      this.stateValue.currentIndex,
      this.stateValue.shuffled,
    );
    await this.playQueueIndex(index, "COMPLETED");
  }

  private capturePlaybackSession(): CapturedPlaybackSession | null {
    const track = this.currentTrack;
    if (!track) return null;
    return {
      track,
      sessionId: this.playbackSessionId,
      position: this.stateValue.currentTime,
      duration: this.stateValue.duration || track.duration,
      started: this.playbackSessionStarted,
    };
  }

  private finishPlaybackSession(session: CapturedPlaybackSession | null, event: PlaybackTerminalEvent | null): void {
    if (!session) return;
    this.playbackSessionStarted = false;
    if (!session.started || !event) return;
    const position = event === "COMPLETED" ? session.duration : session.position;
    void this.recordPlayback(session.track, session.sessionId, position, event);
  }

  private async record(event: "STARTED" | "PROGRESS" | "PAUSED" | "COMPLETED"): Promise<void> {
    const track = this.currentTrack;
    if (!track) return;
    await this.recordPlayback(track, this.playbackSessionId, this.stateValue.currentTime, event);
  }

  private async recordPlayback(
    track: ReadonlyTrack,
    sessionId: string,
    position: number,
    event: "STARTED" | "PROGRESS" | "PAUSED" | "COMPLETED",
  ): Promise<void> {
    await this.playback.record(track.id, sessionId, position * 1000, event).catch(() => undefined);
  }

  private async loadTrackWithRetry(track: ReadonlyTrack, controller: AbortController, request: number): Promise<void> {
    let lastError: unknown;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      if (request !== this.loadRequest || controller.signal.aborted) throw abortReason(controller.signal);
      try {
        const grant = await this.playbackGrants.get(track.id, this.stateValue.quality, controller.signal, attempt > 0);
        await this.audio.load(grant.url, controller.signal);
        return;
      } catch (cause) {
        if (controller.signal.aborted || isAbortError(cause)) throw cause;
        lastError = cause;
        this.diagnostics.warn("playback", `${track.title}: playback attempt ${attempt + 1} failed`);
        this.playbackGrants.invalidate(track.id, this.stateValue.quality);
        if (attempt < 2) await this.retryDelay(RETRY_DELAYS[attempt]!, controller.signal);
      }
    }
    throw lastError;
  }

  private async prepareNext(): Promise<void> {
    if (!this.audio.preload || !this.stateValue.queue.length) return;
    this.clearPrefetch();
    if (!this.stateValue.shuffled
      && this.stateValue.repeatMode === "off"
      && this.stateValue.currentIndex >= this.stateValue.queue.length - 1) return;
    const index = this.stateValue.repeatMode === "one" && this.currentTrack
      ? this.stateValue.currentIndex
      : nextTrackIndex(this.stateValue.queue.length, this.stateValue.currentIndex, this.stateValue.shuffled);
    const track = this.stateValue.queue[index];
    if (!track) return;
    const controller = new AbortController();
    this.prefetchController = controller;
    try {
      const grant = await this.playbackGrants.get(track.id, this.stateValue.quality, controller.signal);
      await this.audio.preload(grant.url, controller.signal);
      if (!controller.signal.aborted && this.prefetchController === controller) this.prefetchedIndex = index;
    } catch {
      if (this.prefetchController === controller) this.prefetchedIndex = -1;
    } finally {
      if (this.prefetchController === controller) this.prefetchController = null;
    }
  }

  private async activatePrefetched(fadeSeconds: number, terminalEvent: PlaybackTerminalEvent): Promise<void> {
    if (this.transitioning || this.prefetchedIndex < 0) return;
    const previousSession = this.capturePlaybackSession();
    const request = ++this.loadRequest;
    const index = this.prefetchedIndex;
    const track = this.stateValue.queue[index];
    if (!track) {
      this.clearPrefetch();
      return;
    }
    const nextSessionId = this.sessionIds.next();
    let switched = false;
    this.transitioning = true;
    this.transitionActivated = false;
    this.endedDuringTransition = false;
    this.prefetchedIndex = -1;
    const commitActivation = () => {
      if (switched || request !== this.loadRequest) return;
      switched = true;
      this.transitionActivated = true;
      const snapshot = this.audio.snapshot();
      this.finishPlaybackSession(previousSession, terminalEvent);
      const duration = snapshot.duration || track.duration;
      this.playbackSessionId = nextSessionId;
      this.playbackSessionStarted = true;
      this.lastCheckpoint = snapshot.currentTime;
      this.lastNativePosition = snapshot.currentTime;
      this.lastNativeStatus = "playing";
      this.updateState({
        currentIndex: index,
        duration,
        currentTime: snapshot.currentTime,
        progress: duration > 0 ? snapshot.currentTime / duration * 100 : 0,
        isPlaying: true,
        error: "",
      });
      this.desktopPlayback.setTrack(track);
      void this.recordPlayback(track, nextSessionId, snapshot.currentTime, "STARTED");
      this.announceTrack(track);
      this.desktopPlayback.setPlayback("playing", snapshot.currentTime, duration);
      this.schedulePersistState();
    };
    try {
      const activated = await this.audio.activatePreloaded?.(fadeSeconds, commitActivation);
      if (request !== this.loadRequest) return;
      if (!activated) {
        const fallbackIndex = this.stateValue.queue.indexOf(track);
        if (fallbackIndex >= 0) await this.playQueueIndex(fallbackIndex, terminalEvent);
        return;
      }
      commitActivation();
      if (!this.endedDuringTransition) void this.prepareNext();
    } catch (cause) {
      if (request !== this.loadRequest) return;
      const message = errorMessage(cause, "切换下一首失败");
      this.updateState({ error: message });
      this.diagnostics.error("playback", `${track.title}: ${message}`);
      if (!switched) {
        const fallbackIndex = this.stateValue.queue.indexOf(track);
        if (fallbackIndex >= 0) await this.playQueueIndex(fallbackIndex, terminalEvent);
      }
    } finally {
      this.transitioning = false;
      this.transitionActivated = false;
      const shouldHandleEnded = this.endedDuringTransition && request === this.loadRequest;
      this.endedDuringTransition = false;
      if (shouldHandleEnded) void this.handleEnded();
    }
  }

  private clearPrefetch(): void {
    this.prefetchController?.abort();
    this.prefetchController = null;
    this.prefetchedIndex = -1;
    this.audio.clearPreloaded?.();
  }

  private refreshPrefetch(): void {
    this.clearPrefetch();
    if (this.currentTrack && this.audio.snapshot().duration > 0) void this.prepareNext();
  }

  private advanceQueueRevision(): number {
    this.extendingQueueRevision = null;
    this.resumeWhenQueueExtends = false;
    return this.stateValue.queueVersion + 1;
  }

  private advancePlaybackIntent(): void {
    this.updateState({ playbackIntentVersion: this.stateValue.playbackIntentVersion + 1 });
  }

  private schedulePersistState(): void {
    this.playbackPersistence.scheduleSnapshot(() => this.createPlaybackSnapshot());
  }

  private scheduleProgressCheckpoint(): void {
    this.playbackPersistence.scheduleCheckpoint(() => this.createPlaybackCheckpoint());
  }

  private flushPersistState(): void {
    this.playbackPersistence.flush(
      () => this.createPlaybackSnapshot(),
      () => this.createPlaybackCheckpoint(),
    );
  }

  private flush(): void {
    this.flushPlayerPreferences();
    this.flushPersistState();
  }

  private createPlaybackSnapshot() {
    const ownerKey = this.playbackPersistence.ownerKey;
    if (!ownerKey) return null;
    return {
      ownerKey,
      queue: this.copyQueue(this.stateValue.queue),
      currentIndex: this.stateValue.currentIndex,
      position: this.stateValue.currentTime,
      shuffled: this.stateValue.shuffled,
      repeat: this.stateValue.repeatMode === "one",
      repeatMode: this.stateValue.repeatMode,
      quality: this.stateValue.quality,
      crossfadeSeconds: this.stateValue.crossfadeSeconds,
    };
  }

  private createPlaybackCheckpoint() {
    const track = this.currentTrack;
    const ownerKey = this.playbackPersistence.ownerKey;
    if (!ownerKey || !track) return null;
    return {
      ownerKey,
      currentIndex: this.stateValue.currentIndex,
      trackId: track.id,
      position: this.stateValue.currentTime,
    };
  }

  private announceTrack(track: ReadonlyTrack): void {
    this.diagnostics.info("playback", `Now playing: ${track.title} - ${track.artist}`);
    if (!this.stateValue.notificationsEnabled) return;
    void this.notifier.notify("正在播放", `${track.title} · ${track.artist}`).catch((cause) => {
      this.diagnostics.warn("notification", errorMessage(cause, "无法显示播放通知"));
    });
  }

  private retryDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
    return new Promise((resolve, reject) => {
      let cancelDelay: (() => void) | undefined;
      const cleanup = () => signal.removeEventListener("abort", aborted);
      const completed = () => {
        cleanup();
        resolve();
      };
      const aborted = () => {
        cancelDelay?.();
        cleanup();
        reject(abortReason(signal));
      };
      if (signal.aborted) {
        aborted();
        return;
      }
      signal.addEventListener("abort", aborted, { once: true });
      cancelDelay = this.scheduler.delay(completed, milliseconds);
    });
  }

  private ownQueue(tracks: readonly ReadonlyTrack[]): PlaybackQueue {
    return Object.freeze(tracks.map((track): ReadonlyTrack => Object.freeze({
      ...track,
      artistIds: Object.freeze([...track.artistIds]),
    })));
  }

  private copyQueue(queue: PlaybackQueue): Track[] {
    return queue.map((track) => ({ ...track, artistIds: [...track.artistIds] }));
  }

  private updateState(patch: Partial<PlaybackSessionState>): void {
    const entries = Object.entries(patch) as [keyof PlaybackSessionState, PlaybackSessionState[keyof PlaybackSessionState]][];
    if (!entries.some(([key, value]) => !Object.is(this.stateValue[key], value))) return;
    this.stateValue = Object.freeze({ ...this.stateValue, ...patch });
    for (const listener of this.listeners) listener(this.stateValue);
  }
}

function isAbortError(cause: unknown): boolean {
  return typeof cause === "object"
    && cause !== null
    && "name" in cause
    && (cause as { name?: unknown }).name === "AbortError";
}

function abortReason(signal: AbortSignal): unknown {
  return signal.reason ?? createAbortError("Request cancelled");
}

function createAbortError(message: string): Error {
  const error = new Error(message);
  error.name = "AbortError";
  return error;
}

const RETRY_DELAYS = [300, 900];
const NATIVE_POSITION_INTERVAL_SECONDS = 3;
const PERSIST_POSITION_INTERVAL_SECONDS = 15;
