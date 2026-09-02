import type {
  AudioBandwidthSample,
  AudioPlayer,
  AudioSnapshot,
  AudioSourceMetadata,
} from "../../application/ports/AudioPlayer";
import Hls from "hls.js";

interface PendingLoad {
  id: number;
  reject: (reason: unknown) => void;
  cleanup: () => void;
}

interface TransitionGains {
  previousAudio: HTMLAudioElement;
  nextAudio: HTMLAudioElement;
  previous: number;
  next: number;
}

interface NetworkMeasurement {
  bitrate: number;
  lastBufferedEnd: number;
  lastMeasuredAt: number;
  pendingBufferedSeconds: number;
  pendingDurationMs: number;
}

interface NavigatorWithConnection extends Navigator {
  readonly connection?: EventTarget;
}

export class HtmlAudioPlayer implements AudioPlayer {
  private audio = new Audio();
  private preloadAudio = new Audio();
  private loadSequence = 0;
  private transitionSequence = 0;
  private pendingLoad: PendingLoad | null = null;
  private preloadController: AbortController | null = null;
  private transitionController: AbortController | null = null;
  private transitionGains: TransitionGains | null = null;
  private updateTimer: number | null = null;
  private lastProgressUpdateAt = 0;
  private lastEmittedSnapshot: AudioSnapshot | null = null;
  private preparedUrl = "";
  private configuredVolume = 1;
  private readonly updateListeners = new Set<(snapshot: AudioSnapshot) => void>();
  private readonly endedListeners = new Set<() => void>();
  private readonly errorListeners = new Set<(message: string) => void>();
  private readonly bandwidthListeners = new Set<(sample: AudioBandwidthSample) => void>();
  private readonly bufferingListeners = new Set<() => void>();
  private readonly networkMeasurements = new WeakMap<HTMLAudioElement, NetworkMeasurement>();
  private readonly hlsInstances = new WeakMap<HTMLAudioElement, Hls>();
  private readonly knownDurations = new WeakMap<HTMLAudioElement, number>();
  private readonly sourceOffsets = new WeakMap<HTMLAudioElement, number>();
  private readonly sourceProtocols = new WeakMap<HTMLAudioElement, NonNullable<AudioSourceMetadata["streamProtocol"]>>();
  private readonly pendingSeeks = new WeakMap<HTMLAudioElement, number>();
  private readonly emitSnapshot = (): boolean => {
    const snapshot = this.snapshot();
    if (this.lastEmittedSnapshot
      && this.lastEmittedSnapshot.currentTime === snapshot.currentTime
      && this.lastEmittedSnapshot.duration === snapshot.duration
      && this.lastEmittedSnapshot.paused === snapshot.paused) return false;
    this.lastEmittedSnapshot = snapshot;
    for (const listener of this.updateListeners) listener(snapshot);
    return true;
  };
  private readonly emitProgressUpdate = () => {
    if (!this.updateListeners.size || this.audio.paused) return;
    const now = performance.now();
    if (now - this.lastProgressUpdateAt < UPDATE_INTERVAL_MS) return;
    this.lastProgressUpdateAt = now;
    this.emitSnapshot();
  };
  private readonly emitImmediateUpdate = () => {
    if (this.emitSnapshot()) this.lastProgressUpdateAt = performance.now();
  };
  private readonly handlePlay = () => {
    this.emitImmediateUpdate();
    this.startUpdateLoop();
  };
  private readonly handlePause = () => {
    this.emitImmediateUpdate();
    this.stopUpdateLoop();
  };
  private readonly emitEnded = () => {
    this.stopUpdateLoop();
    this.emitImmediateUpdate();
    for (const listener of this.endedListeners) listener();
  };
  private readonly emitError = () => {
    if (this.pendingLoad || (!this.audio.hasAttribute("src") && !this.hlsInstances.has(this.audio))) return;
    this.stopUpdateLoop();
    const message = this.audio.error?.message || "音频播放失败";
    for (const listener of this.errorListeners) listener(message);
  };
  private readonly measureNetworkProgress = (event: Event) => {
    this.measureBufferedProgress(event.currentTarget as HTMLAudioElement);
  };
  private readonly emitBuffering = () => {
    if (this.audio.paused || this.audio.currentTime < MIN_REBUFFER_POSITION_SECONDS || this.audio.readyState >= HAVE_FUTURE_DATA) return;
    for (const listener of this.bufferingListeners) listener();
  };

  constructor() {
    this.audio.addEventListener("progress", this.measureNetworkProgress);
    this.preloadAudio.addEventListener("progress", this.measureNetworkProgress);
    this.bindActiveAudio(this.audio);
  }

  async load(url: string, signal?: AbortSignal, metadata?: AudioSourceMetadata): Promise<void> {
    if (!url.trim()) throw new Error("音频地址为空");
    if (signal?.aborted) throw signal.reason ?? abortError();

    this.clearPreloaded();
    this.cancelPendingLoad();
    const id = ++this.loadSequence;
    this.audio.pause();
    this.stopUpdateLoop();
    this.audio.volume = this.configuredVolume;
    this.startNetworkMeasurement(this.audio, metadata?.bitrate);

    await new Promise<void>((resolve, reject) => {
      let timeout: number | undefined;
      let removeHlsReady: () => void = () => undefined;
      const cleanup = () => {
        removeHlsReady();
        removeHlsReady = () => undefined;
        this.audio.removeEventListener("loadedmetadata", ready);
        this.audio.removeEventListener("canplay", ready);
        this.audio.removeEventListener("error", failed);
        signal?.removeEventListener("abort", aborted);
        if (timeout !== undefined) window.clearTimeout(timeout);
      };
      const settle = (action: () => void) => {
        if (this.pendingLoad?.id !== id) return;
        this.pendingLoad = null;
        cleanup();
        action();
      };
      const ready = () => {
        if (id !== this.loadSequence || this.audio.readyState < HAVE_METADATA) return;
        settle(resolve);
      };
      const hlsReady = () => {
        if (id !== this.loadSequence) return;
        settle(resolve);
      };
      const failed = () => {
        if (id !== this.loadSequence) return;
        settle(() => reject(new Error(this.audio.error?.message || "音频加载失败")));
      };
      const aborted = () => {
        if (id !== this.loadSequence) return;
        settle(() => reject(signal?.reason ?? abortError()));
        this.audio.pause();
        this.clearSource();
      };

      this.pendingLoad = { id, reject, cleanup };
      this.audio.addEventListener("loadedmetadata", ready);
      this.audio.addEventListener("canplay", ready);
      this.audio.addEventListener("error", failed);
      signal?.addEventListener("abort", aborted, { once: true });
      timeout = window.setTimeout(() => {
        if (id !== this.loadSequence) return;
        settle(() => reject(new Error("音频加载超时，请重试")));
        this.audio.pause();
        this.clearSource();
      }, AUDIO_LOAD_TIMEOUT_MS);

      try {
        removeHlsReady = this.setSource(this.audio, url, metadata, hlsReady);
        if (this.audio.readyState >= HAVE_METADATA) ready();
      } catch (error) {
        settle(() => reject(error));
        this.clearSource();
      }
    });
    this.measureBufferedProgress(this.audio);
  }

  async play(): Promise<void> {
    await this.audio.play();
    this.startUpdateLoop();
  }

  async preload(url: string, signal?: AbortSignal, metadata?: AudioSourceMetadata): Promise<void> {
    if (!url.trim()) return;
    this.clearPreloaded();
    if (signal?.aborted) throw signal.reason ?? abortError();
    const controller = new AbortController();
    const forwardAbort = () => controller.abort(signal?.reason ?? abortError());
    signal?.addEventListener("abort", forwardAbort, { once: true });
    this.preloadController = controller;
    this.startNetworkMeasurement(this.preloadAudio, metadata?.bitrate);
    try {
      await waitUntilPlayable(
        this.preloadAudio,
        controller.signal,
          () => this.setSource(this.preloadAudio, url, metadata),
      );
      this.measureBufferedProgress(this.preloadAudio);
      if (this.preloadController !== controller || controller.signal.aborted) throw controller.signal.reason ?? abortError();
      this.preparedUrl = url;
    } catch (cause) {
      this.clearSource(this.preloadAudio);
      throw cause;
    } finally {
      signal?.removeEventListener("abort", forwardAbort);
      if (this.preloadController === controller) this.preloadController = null;
    }
  }

  async activatePreloaded(fadeSeconds: number, onActivated?: () => void): Promise<boolean> {
    const url = this.preparedUrl;
    if (!url) return false;
    this.releaseTransitionAudio();
    const transition = ++this.transitionSequence;
    const controller = new AbortController();
    this.transitionController = controller;
    this.preparedUrl = "";
    const previousAudio = this.audio;
    const nextAudio = this.preloadAudio;
    const fadeMs = previousAudio.paused ? 0 : Math.max(0, Math.min(5, fadeSeconds)) * 1_000;
    const gains: TransitionGains | null = fadeMs
      ? { previousAudio, nextAudio, previous: 1, next: 0 }
      : null;
    if (gains) {
      this.transitionGains = gains;
      this.applyTransitionVolumes(gains);
    } else nextAudio.volume = this.configuredVolume;
    try {
      await nextAudio.play();
    } catch {
      nextAudio.pause();
      this.clearSource(nextAudio);
      nextAudio.volume = this.configuredVolume;
      if (this.transitionGains === gains) this.transitionGains = null;
      if (this.transitionController === controller) this.transitionController = null;
      return false;
    }
    if (controller.signal.aborted
      || this.transitionSequence !== transition
      || (!this.hlsInstances.has(nextAudio) && !nextAudio.hasAttribute("src"))) {
      nextAudio.pause();
      this.clearSource(nextAudio);
      nextAudio.volume = this.configuredVolume;
      if (this.transitionGains === gains) this.transitionGains = null;
      return false;
    }

    this.stopUpdateLoop();
    this.unbindActiveAudio(previousAudio);
    this.audio = nextAudio;
    this.preloadAudio = previousAudio;
    this.bindActiveAudio(this.audio);
    onActivated?.();
    this.emitImmediateUpdate();
    this.startUpdateLoop();

    if (gains) {
      this.applyTransitionVolumes(gains);
      await crossfadeVolume(fadeMs, controller.signal, (progress) => {
        if (this.transitionGains !== gains) return;
        gains.previous = 1 - progress;
        gains.next = progress;
        this.applyTransitionVolumes(gains);
      });
    }
    if (!controller.signal.aborted && this.transitionSequence === transition) {
      previousAudio.pause();
      this.clearSource(previousAudio);
      previousAudio.volume = this.configuredVolume;
      this.audio.volume = this.configuredVolume;
    }
    if (this.transitionGains?.previousAudio === previousAudio) this.transitionGains = null;
    if (this.transitionController === controller) this.transitionController = null;
    return true;
  }

  clearPreloaded(): void {
    this.preloadController?.abort(abortError());
    this.preloadController = null;
    this.cancelTransition();
    this.preparedUrl = "";
    this.preloadAudio.pause();
    this.clearSource(this.preloadAudio);
    this.preloadAudio.volume = this.configuredVolume;
    this.audio.volume = this.configuredVolume;
  }

  pause(): void {
    this.releaseTransitionAudio();
    this.audio.pause();
    this.stopUpdateLoop();
  }

  stop(): void {
    this.cancelTransition();
    this.loadSequence += 1;
    this.cancelPendingLoad();
    this.preloadController?.abort(abortError());
    this.preloadController = null;
    this.audio.pause();
    this.stopUpdateLoop();
    this.clearSource();
    this.clearPreloaded();
  }

  seek(seconds: number): void {
    if (!Number.isFinite(seconds)) return;
    this.releaseTransitionAudio();
    const target = Math.max(0, seconds);
    if (this.sourceProtocols.get(this.audio) === "HLS") {
      this.pendingSeeks.set(this.audio, target);
      this.applyPendingSeek(this.audio);
    } else {
      this.pendingSeeks.delete(this.audio);
      this.audio.currentTime = Math.max(0, target - this.sourceOffset(this.audio));
    }
    this.emitImmediateUpdate();
  }

  setVolume(volume: number): void {
    if (!Number.isFinite(volume)) return;
    this.configuredVolume = Math.max(0, Math.min(1, volume));
    if (this.transitionGains) this.applyTransitionVolumes(this.transitionGains);
    else {
      this.audio.volume = this.configuredVolume;
      this.preloadAudio.volume = this.configuredVolume;
    }
  }

  snapshot(): AudioSnapshot {
    const pendingSeek = this.pendingSeeks.get(this.audio);
    const nativeDuration = finiteValue(this.audio.duration);
    const knownDuration = finiteValue(this.knownDurations.get(this.audio) ?? 0);
    const offset = this.sourceOffset(this.audio);
    return {
      currentTime: pendingSeek ?? offset + finiteValue(this.audio.currentTime),
      duration: knownDuration || (nativeDuration ? offset + nativeDuration : 0),
      paused: this.audio.paused,
    };
  }

  onUpdate(listener: (snapshot: AudioSnapshot) => void): () => void {
    this.updateListeners.add(listener);
    if (!this.audio.paused) this.startUpdateLoop();
    return () => {
      this.updateListeners.delete(listener);
      if (!this.updateListeners.size) this.stopUpdateLoop();
    };
  }

  onEnded(listener: () => void): () => void {
    this.endedListeners.add(listener);
    return () => this.endedListeners.delete(listener);
  }

  onError(listener: (message: string) => void): () => void {
    this.errorListeners.add(listener);
    return () => this.errorListeners.delete(listener);
  }

  onBandwidthSample(listener: (sample: AudioBandwidthSample) => void): () => void {
    this.bandwidthListeners.add(listener);
    return () => this.bandwidthListeners.delete(listener);
  }

  onBuffering(listener: () => void): () => void {
    this.bufferingListeners.add(listener);
    return () => this.bufferingListeners.delete(listener);
  }

  onNetworkChange(listener: () => void): () => void {
    const connection = (navigator as NavigatorWithConnection).connection;
    window.addEventListener("online", listener);
    window.addEventListener("offline", listener);
    connection?.addEventListener("change", listener);
    return () => {
      window.removeEventListener("online", listener);
      window.removeEventListener("offline", listener);
      connection?.removeEventListener("change", listener);
    };
  }

  private cancelPendingLoad(): void {
    const pending = this.pendingLoad;
    if (!pending) return;
    this.pendingLoad = null;
    pending.cleanup();
    pending.reject(abortError());
  }

  private setSource(
    audio: HTMLAudioElement,
    url: string,
    metadata?: AudioSourceMetadata,
    onHlsReady?: () => void,
  ): () => void {
    const protocol = metadata?.streamProtocol ?? "PROGRESSIVE";
    this.clearHls(audio);
    this.pendingSeeks.delete(audio);
    this.sourceProtocols.set(audio, protocol);
    const duration = finiteValue(metadata?.duration ?? 0);
    const offset = finiteValue(metadata?.startOffset ?? 0);
    this.sourceOffsets.set(audio, offset);
    if (duration > 0) this.knownDurations.set(audio, duration);
    else this.knownDurations.delete(audio);
    if (protocol === "HLS" && Hls.isSupported()) {
      const hls = new Hls({
        backBufferLength: 90,
        lowLatencyMode: false,
      });
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) audio.dispatchEvent(new Event("error"));
      });
      hls.on(Hls.Events.LEVEL_UPDATED, () => {
        this.applyPendingSeek(audio);
        if (audio === this.audio) this.emitImmediateUpdate();
      });
      if (onHlsReady) hls.once(Hls.Events.MANIFEST_PARSED, onHlsReady);
      this.hlsInstances.set(audio, hls);
      hls.attachMedia(audio);
      hls.loadSource(url);
      return onHlsReady
        ? () => hls.off(Hls.Events.MANIFEST_PARSED, onHlsReady)
        : () => undefined;
    }
    audio.preload = "auto";
    audio.src = url;
    audio.load();
    return () => undefined;
  }

  private clearSource(audio: HTMLAudioElement = this.audio): void {
    this.clearHls(audio);
    this.pendingSeeks.delete(audio);
    this.knownDurations.delete(audio);
    this.sourceOffsets.delete(audio);
    this.sourceProtocols.delete(audio);
    try {
      audio.currentTime = 0;
    } catch {
      // Some engines reject seeking while no media is attached.
    }
    audio.removeAttribute("src");
    try {
      audio.load();
    } catch {
      // The source is already detached; a browser-specific load error is harmless here.
    }
  }

  private clearHls(audio: HTMLAudioElement): void {
    const hls = this.hlsInstances.get(audio);
    if (!hls) return;
    this.hlsInstances.delete(audio);
    hls.destroy();
  }

  private bindActiveAudio(audio: HTMLAudioElement): void {
    audio.addEventListener("timeupdate", this.emitProgressUpdate);
    audio.addEventListener("durationchange", this.handleDurationChange);
    audio.addEventListener("play", this.handlePlay);
    audio.addEventListener("pause", this.handlePause);
    audio.addEventListener("ended", this.emitEnded);
    audio.addEventListener("error", this.emitError);
    audio.addEventListener("waiting", this.emitBuffering);
  }

  private unbindActiveAudio(audio: HTMLAudioElement): void {
    audio.removeEventListener("timeupdate", this.emitProgressUpdate);
    audio.removeEventListener("durationchange", this.handleDurationChange);
    audio.removeEventListener("play", this.handlePlay);
    audio.removeEventListener("pause", this.handlePause);
    audio.removeEventListener("ended", this.emitEnded);
    audio.removeEventListener("error", this.emitError);
    audio.removeEventListener("waiting", this.emitBuffering);
  }

  private readonly handleDurationChange = (event: Event): void => {
    const audio = event.currentTarget as HTMLAudioElement;
    this.applyPendingSeek(audio);
    if (audio === this.audio) this.emitImmediateUpdate();
  };

  private applyPendingSeek(audio: HTMLAudioElement): void {
    const target = this.pendingSeeks.get(audio);
    if (target === undefined) return;
    const localTarget = Math.max(0, target - this.sourceOffset(audio));
    const nativeDuration = audio.duration;
    if (localTarget > 0 && Number.isFinite(nativeDuration) && nativeDuration > 0 && localTarget > nativeDuration + HLS_SEEK_TOLERANCE_SECONDS) {
      return;
    }
    try {
      audio.currentTime = localTarget;
      this.pendingSeeks.delete(audio);
    } catch {
      // The media element may not have attached its first HLS buffer yet.
    }
  }

  private startNetworkMeasurement(audio: HTMLAudioElement, bitrate: number | undefined): void {
    if (!Number.isFinite(bitrate) || Number(bitrate) <= 0) {
      this.networkMeasurements.delete(audio);
      return;
    }
    this.networkMeasurements.set(audio, {
      bitrate: Number(bitrate),
      lastBufferedEnd: 0,
      lastMeasuredAt: performance.now(),
      pendingBufferedSeconds: 0,
      pendingDurationMs: 0,
    });
  }

  private measureBufferedProgress(audio: HTMLAudioElement): void {
    const measurement = this.networkMeasurements.get(audio);
    if (!measurement) return;
    const bufferedEnd = furthestBufferedEnd(audio);
    if (bufferedEnd <= measurement.lastBufferedEnd) return;
    const now = performance.now();
    const elapsedMs = now - measurement.lastMeasuredAt;
    if (elapsedMs > MAX_ACTIVE_TRANSFER_GAP_MS) {
      // A long idle gap means the pending window belongs to an earlier burst
      // (paused playback or a network switch). Start fresh so stale bytes
      // cannot skew the new estimate.
      measurement.pendingBufferedSeconds = 0;
      measurement.pendingDurationMs = 0;
      measurement.lastBufferedEnd = bufferedEnd;
      measurement.lastMeasuredAt = now;
      return;
    }
    const bufferedSeconds = bufferedEnd - measurement.lastBufferedEnd;
    measurement.lastBufferedEnd = bufferedEnd;
    measurement.lastMeasuredAt = now;
    // Short windows are accumulated instead of discarded: on a fast link the
    // initial fill completes in a few tens of milliseconds, and dropping those
    // bytes used to starve the estimator until the network slowed down.
    measurement.pendingBufferedSeconds += bufferedSeconds;
    measurement.pendingDurationMs += elapsedMs;
    if (measurement.pendingDurationMs < MIN_TRANSFER_SAMPLE_MS) return;
    if (measurement.pendingBufferedSeconds <= 0 || measurement.pendingDurationMs <= 0) return;
    const durationMs = measurement.pendingDurationMs;
    const mergedBufferedSeconds = measurement.pendingBufferedSeconds;
    measurement.pendingBufferedSeconds = 0;
    measurement.pendingDurationMs = 0;
    const bitsPerSecond = mergedBufferedSeconds * measurement.bitrate / (durationMs / 1_000);
    if (!Number.isFinite(bitsPerSecond) || bitsPerSecond <= 0) return;
    // LAN/local-deployment bursts can instant-fill the initial buffer; those
    // readings are equivalent for quality selection (the top tier needs well
    // under 10% of this) and stay below the estimator's 100Mbps sanity filter.
    const sample = { bitsPerSecond: Math.min(bitsPerSecond, MAX_MEANINGFUL_BPS), durationMs };
    for (const listener of this.bandwidthListeners) listener(sample);
  }

  private startUpdateLoop(): void {
    if (this.updateTimer !== null || this.audio.paused || !this.updateListeners.size) return;
    this.lastProgressUpdateAt = performance.now();
    const tick = () => {
      this.updateTimer = null;
      if (this.audio.paused || !this.updateListeners.size) return;
      this.emitProgressUpdate();
      // A listener can synchronously pause playback or unsubscribe itself.
      // Do not leave a one-shot timer behind after that state transition.
      if (this.audio.paused || !this.updateListeners.size) return;
      const remaining = Math.max(1, UPDATE_INTERVAL_MS - (performance.now() - this.lastProgressUpdateAt));
      this.updateTimer = window.setTimeout(tick, remaining);
    };
    this.updateTimer = window.setTimeout(tick, UPDATE_INTERVAL_MS);
  }

  private stopUpdateLoop(): void {
    if (this.updateTimer !== null) window.clearTimeout(this.updateTimer);
    this.updateTimer = null;
  }

  private cancelTransition(): void {
    this.transitionSequence += 1;
    this.transitionController?.abort(abortError());
    this.transitionController = null;
    this.transitionGains = null;
  }

  private releaseTransitionAudio(): void {
    if (!this.transitionController) return;
    this.cancelTransition();
    this.preloadAudio.pause();
    this.clearSource(this.preloadAudio);
    this.preloadAudio.volume = this.configuredVolume;
    this.audio.volume = this.configuredVolume;
  }

  private applyTransitionVolumes(gains: TransitionGains): void {
    gains.previousAudio.volume = normalizedAudioVolume(this.configuredVolume * gains.previous);
    gains.nextAudio.volume = normalizedAudioVolume(this.configuredVolume * gains.next);
  }

  private sourceOffset(audio: HTMLAudioElement): number {
    return finiteValue(this.sourceOffsets.get(audio) ?? 0);
  }
}

function finiteValue(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0;
}

function furthestBufferedEnd(audio: HTMLAudioElement): number {
  if (!audio.buffered.length) return 0;
  try {
    return finiteValue(audio.buffered.end(audio.buffered.length - 1));
  } catch {
    return 0;
  }
}

function abortError(): DOMException {
  return new DOMException("音频加载已取消", "AbortError");
}

const HAVE_FUTURE_DATA = 3;
const HAVE_METADATA = 1;
const AUDIO_LOAD_TIMEOUT_MS = 30_000;
const UPDATE_INTERVAL_MS = 1_000 / 15;
const MIN_REBUFFER_POSITION_SECONDS = 3;
const MIN_TRANSFER_SAMPLE_MS = 30;
const MAX_ACTIVE_TRANSFER_GAP_MS = 5_000;
const MAX_MEANINGFUL_BPS = 50_000_000;
const HLS_SEEK_TOLERANCE_SECONDS = 0.25;

async function waitUntilPlayable(
  audio: HTMLAudioElement,
  signal: AbortSignal | undefined,
  startSource: () => void,
): Promise<void> {
  if (signal?.aborted) throw signal.reason ?? abortError();
  await new Promise<void>((resolve, reject) => {
    let settled = false;
    const cleanup = () => {
      audio.removeEventListener("loadedmetadata", ready);
      audio.removeEventListener("canplay", ready);
      audio.removeEventListener("error", failed);
      signal?.removeEventListener("abort", aborted);
      window.clearTimeout(timer);
    };
    const settle = (action: () => void) => {
      if (settled) return;
      settled = true;
      cleanup();
      action();
    };
    const rejectAndClear = (cause: unknown) => settle(() => {
      audio.pause();
      audio.removeAttribute("src");
      reject(cause);
    });
    const ready = () => settle(resolve);
    const failed = () => rejectAndClear(new Error(audio.error?.message || "下一首预加载失败"));
    const aborted = () => rejectAndClear(signal?.reason ?? abortError());
    const timer = window.setTimeout(() => rejectAndClear(new Error("下一首预加载超时，请重试")), AUDIO_LOAD_TIMEOUT_MS);
    audio.addEventListener("loadedmetadata", ready, { once: true });
    audio.addEventListener("canplay", ready, { once: true });
    audio.addEventListener("error", failed, { once: true });
    signal?.addEventListener("abort", aborted, { once: true });
    try {
      audio.preload = "auto";
      startSource();
      if (audio.readyState >= HAVE_METADATA) ready();
    } catch (cause) {
      rejectAndClear(cause);
    }
  });
}

async function crossfadeVolume(
  durationMs: number,
  signal: AbortSignal,
  update: (progress: number) => void,
): Promise<boolean> {
  if (signal.aborted) return false;
  const startedAt = performance.now();
  return await new Promise<boolean>((resolve) => {
    let frame: number | null = null;
    let settled = false;
    const finish = (completed: boolean) => {
      if (settled) return;
      settled = true;
      if (frame !== null) cancelFrame(frame);
      signal.removeEventListener("abort", aborted);
      resolve(completed);
    };
    const aborted = () => finish(false);
    const step = (now: number) => {
      frame = null;
      if (signal.aborted) { finish(false); return; }
      const progress = Math.max(0, Math.min(1, (now - startedAt) / durationMs));
      update(progress);
      if (progress >= 1) finish(true);
      else frame = requestFrame(step);
    };
    signal.addEventListener("abort", aborted, { once: true });
    frame = requestFrame(step);
  });
}

export function normalizedAudioVolume(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(1, value));
}

function requestFrame(callback: FrameRequestCallback): number {
  return typeof window.requestAnimationFrame === "function"
    ? window.requestAnimationFrame(callback)
    : window.setTimeout(() => callback(performance.now()), UPDATE_INTERVAL_MS);
}

function cancelFrame(frame: number): void {
  if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(frame);
  else window.clearTimeout(frame);
}
