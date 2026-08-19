import type {
  PlaybackSession,
  PlaybackSessionState,
  PlaybackTerminalEvent,
  QueueStart,
} from "../../src/application/ports/PlaybackSession";
import type { PlaybackQuality, Track } from "../../src/domain/music";
import type { PlayMode } from "../../src/domain/playbackState";
import { splitPlayMode } from "../../src/domain/playbackState";

interface FakePlaybackSessionOptions {
  state?: Partial<PlaybackSessionState>;
  restore?: (ownerKey: string) => Partial<PlaybackSessionState> | null;
  onSetVolume?: (value: number) => number;
  onFlushPlayerPreferences?: () => void;
  onClearGrants?: () => void;
}

export class FakePlaybackSession implements PlaybackSession {
  private readonly listeners = new Set<(state: PlaybackSessionState) => void>();
  private readonly restoreHandler: ((ownerKey: string) => Partial<PlaybackSessionState> | null) | undefined;
  private readonly onSetVolume: ((value: number) => number) | undefined;
  private readonly onFlushPlayerPreferences: (() => void) | undefined;
  private readonly onClearGrants: (() => void) | undefined;
  private stateValue: PlaybackSessionState;

  constructor(options: FakePlaybackSessionOptions = {}) {
    this.restoreHandler = options.restore;
    this.onSetVolume = options.onSetVolume;
    this.onFlushPlayerPreferences = options.onFlushPlayerPreferences;
    this.onClearGrants = options.onClearGrants;
    this.stateValue = {
      queue: [],
      queueVersion: 0,
      playbackIntentVersion: 0,
      positionDiscontinuityVersion: 0,
      currentIndex: -1,
      isPlaying: false,
      loading: false,
      progress: 0,
      currentTime: 0,
      duration: 0,
      volume: 72,
      shuffled: false,
      repeatMode: "off",
      quality: "AUTO",
      crossfadeSeconds: 0,
      notificationsEnabled: false,
      miniMode: false,
      error: "",
      ...options.state,
    };
  }

  state(): PlaybackSessionState {
    return this.stateValue;
  }

  subscribe(listener: (state: PlaybackSessionState) => void): () => void {
    this.listeners.add(listener);
    listener(this.stateValue);
    return () => this.listeners.delete(listener);
  }

  seed(tracks: Track[]): void {
    if (this.stateValue.queue.length || !tracks.length) return;
    this.update({ queue: [...tracks], queueVersion: this.stateValue.queueVersion + 1 });
  }

  restoreState(ownerKey: string): boolean {
    const restored = this.restoreHandler?.(ownerKey);
    if (!restored) return false;
    this.update(restored);
    return true;
  }

  clearPersistedState(): void {}
  clearGrants(): void { this.onClearGrants?.(); }
  flushPlayerPreferences(): void { this.onFlushPlayerPreferences?.(); }

  async play(track: Track, tracks: Track[] = this.stateValue.queue): Promise<void> {
    const index = tracks.findIndex((candidate) => candidate.id === track.id);
    await this.startQueue(tracks, index >= 0 ? index : 0)?.playback;
  }

  async playAt(index: number): Promise<void> {
    if (index < 0 || index >= this.stateValue.queue.length) return;
    this.update({
      currentIndex: index,
      positionDiscontinuityVersion: this.stateValue.positionDiscontinuityVersion + 1,
    });
  }

  async playFromIndex(tracks: Track[], index: number, terminalEvent?: PlaybackTerminalEvent | null): Promise<void> {
    await this.startQueue(tracks, index, terminalEvent)?.playback;
  }

  startQueue(tracks: Track[], index: number): QueueStart | null {
    if (index < 0 || index >= tracks.length) return null;
    const revision = this.stateValue.queueVersion + 1;
    this.update({
      queue: [...tracks],
      queueVersion: revision,
      currentIndex: index,
      positionDiscontinuityVersion: this.stateValue.positionDiscontinuityVersion + 1,
    });
    return { revision, playback: Promise.resolve(true) };
  }

  appendToQueue(revision: number, tracks: Track[]): boolean {
    if (revision !== this.stateValue.queueVersion) return false;
    this.update({ queue: [...this.stateValue.queue, ...tracks] });
    return true;
  }

  setQueueExtending(): void {}
  async toggle(): Promise<void> { this.update({ isPlaying: !this.stateValue.isPlaying }); }
  async next(): Promise<void> { await this.playAt(this.stateValue.currentIndex + 1); }
  async previous(): Promise<void> { await this.playAt(Math.max(0, this.stateValue.currentIndex - 1)); }
  seek(percent: number): void { this.seekTo(this.stateValue.duration * percent / 100); }
  seekTo(seconds: number): void {
    const currentTime = Math.max(0, Math.min(this.stateValue.duration, seconds));
    this.update({
      currentTime,
      progress: this.stateValue.duration > 0 ? currentTime / this.stateValue.duration * 100 : 0,
      positionDiscontinuityVersion: this.stateValue.positionDiscontinuityVersion + 1,
    });
  }

  removeFromQueue(trackId: string): void {
    this.update({ queue: this.stateValue.queue.filter((track) => track.id !== trackId) });
  }

  removeFromQueueAt(index: number): void {
    this.update({ queue: this.stateValue.queue.filter((_, candidate) => candidate !== index) });
  }

  clearQueue(): void {
    const currentTrack = this.stateValue.queue[this.stateValue.currentIndex];
    this.update({ queue: currentTrack ? [currentTrack] : [], currentIndex: currentTrack ? 0 : -1 });
  }

  stopPlayback(): void { this.update({ isPlaying: false, currentTime: 0, progress: 0 }); }

  reset(): void {
    this.update({
      queue: [],
      queueVersion: this.stateValue.queueVersion + 1,
      positionDiscontinuityVersion: this.stateValue.positionDiscontinuityVersion + 1,
      currentIndex: -1,
      isPlaying: false,
      currentTime: 0,
      progress: 0,
      duration: 0,
    });
  }

  async setMiniMode(enabled: boolean): Promise<void> { this.update({ miniMode: enabled }); }

  setPlayMode(mode: PlayMode): void {
    const { repeatMode, shuffled } = splitPlayMode(mode);
    this.update({ repeatMode, shuffled });
  }

  cyclePlayMode(): void {}

  setVolume(value: number): void {
    const normalized = this.onSetVolume?.(value) ?? Math.max(0, Math.min(100, value));
    this.update({ volume: normalized });
  }

  setQuality(value: PlaybackQuality): void { this.update({ quality: value }); }
  setCrossfadeSeconds(value: number): void { this.update({ crossfadeSeconds: value }); }
  setNotificationsEnabled(value: boolean): void { this.update({ notificationsEnabled: value }); }
  dispose(): void { this.listeners.clear(); }

  update(patch: Partial<PlaybackSessionState>): void {
    this.stateValue = { ...this.stateValue, ...patch };
    for (const listener of this.listeners) listener(this.stateValue);
  }
}
