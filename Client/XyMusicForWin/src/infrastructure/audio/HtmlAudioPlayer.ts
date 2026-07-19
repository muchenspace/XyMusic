import type { AudioPlayer, AudioSnapshot } from "../../application/ports/AudioPlayer";

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

export class HtmlAudioPlayer implements AudioPlayer {
  private audio = new Audio();
  private preloadAudio = new Audio();
  private loadSequence = 0;
  private transitionSequence = 0;
  private pendingLoad: PendingLoad | null = null;
  private preloadController: AbortController | null = null;
  private transitionController: AbortController | null = null;
  private transitionGains: TransitionGains | null = null;
  private updateFrame: number | null = null;
  private lastAnimationUpdate = 0;
  private preparedUrl = "";
  private configuredVolume = 1;
  private readonly updateListeners = new Set<(snapshot: AudioSnapshot) => void>();
  private readonly endedListeners = new Set<() => void>();
  private readonly errorListeners = new Set<(message: string) => void>();
  private readonly emitUpdate = () => {
    const snapshot = this.snapshot();
    for (const listener of this.updateListeners) listener(snapshot);
  };
  private readonly handlePlay = () => {
    this.emitUpdate();
    this.startUpdateLoop();
  };
  private readonly handlePause = () => {
    this.emitUpdate();
    this.stopUpdateLoop();
  };
  private readonly emitEnded = () => {
    this.stopUpdateLoop();
    this.emitUpdate();
    for (const listener of this.endedListeners) listener();
  };
  private readonly emitError = () => {
    if (this.pendingLoad || !this.audio.hasAttribute("src")) return;
    this.stopUpdateLoop();
    const message = this.audio.error?.message || "音频播放失败";
    for (const listener of this.errorListeners) listener(message);
  };

  constructor() {
    this.bindActiveAudio(this.audio);
  }

  async load(url: string, signal?: AbortSignal): Promise<void> {
    if (!url.trim()) throw new Error("音频地址为空");
    if (signal?.aborted) throw signal.reason ?? abortError();

    this.clearPreloaded();
    this.cancelPendingLoad();
    const id = ++this.loadSequence;
    this.audio.pause();
    this.stopUpdateLoop();
    this.audio.volume = this.configuredVolume;

    await new Promise<void>((resolve, reject) => {
      let timeout: number | undefined;
      const cleanup = () => {
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
        if (id !== this.loadSequence || this.audio.readyState < HAVE_FUTURE_DATA) return;
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
        this.audio.src = url;
        this.audio.load();
        if (this.audio.readyState >= HAVE_FUTURE_DATA) ready();
      } catch (error) {
        settle(() => reject(error));
        this.clearSource();
      }
    });
  }

  async play(): Promise<void> {
    await this.audio.play();
    this.startUpdateLoop();
  }

  async preload(url: string, signal?: AbortSignal): Promise<void> {
    if (!url.trim()) return;
    this.clearPreloaded();
    if (signal?.aborted) throw signal.reason ?? abortError();
    const controller = new AbortController();
    const forwardAbort = () => controller.abort(signal?.reason ?? abortError());
    signal?.addEventListener("abort", forwardAbort, { once: true });
    this.preloadController = controller;
    try {
      await waitUntilPlayable(this.preloadAudio, url, controller.signal);
      if (this.preloadController !== controller || controller.signal.aborted) throw controller.signal.reason ?? abortError();
      this.preparedUrl = url;
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
      clearSource(nextAudio);
      nextAudio.volume = this.configuredVolume;
      if (this.transitionGains === gains) this.transitionGains = null;
      if (this.transitionController === controller) this.transitionController = null;
      return false;
    }
    if (controller.signal.aborted || this.transitionSequence !== transition || !nextAudio.hasAttribute("src")) {
      nextAudio.pause();
      clearSource(nextAudio);
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
    this.emitUpdate();
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
      clearSource(previousAudio);
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
    clearSource(this.preloadAudio);
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
    const duration = finiteValue(this.audio.duration);
    this.audio.currentTime = Math.max(0, Math.min(seconds, duration > 0 ? duration : seconds));
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
    return {
      currentTime: finiteValue(this.audio.currentTime),
      duration: finiteValue(this.audio.duration),
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

  private cancelPendingLoad(): void {
    const pending = this.pendingLoad;
    if (!pending) return;
    this.pendingLoad = null;
    pending.cleanup();
    pending.reject(abortError());
  }

  private clearSource(): void {
    try {
      this.audio.currentTime = 0;
    } catch {
      // Some engines reject seeking while no media is attached.
    }
    this.audio.removeAttribute("src");
    try {
      this.audio.load();
    } catch {
      // The source is already detached; a browser-specific load error is harmless here.
    }
  }

  private bindActiveAudio(audio: HTMLAudioElement): void {
    audio.addEventListener("timeupdate", this.emitUpdate);
    audio.addEventListener("durationchange", this.emitUpdate);
    audio.addEventListener("play", this.handlePlay);
    audio.addEventListener("pause", this.handlePause);
    audio.addEventListener("ended", this.emitEnded);
    audio.addEventListener("error", this.emitError);
  }

  private unbindActiveAudio(audio: HTMLAudioElement): void {
    audio.removeEventListener("timeupdate", this.emitUpdate);
    audio.removeEventListener("durationchange", this.emitUpdate);
    audio.removeEventListener("play", this.handlePlay);
    audio.removeEventListener("pause", this.handlePause);
    audio.removeEventListener("ended", this.emitEnded);
    audio.removeEventListener("error", this.emitError);
  }

  private startUpdateLoop(): void {
    if (this.updateFrame !== null || this.audio.paused || !this.updateListeners.size) return;
    this.lastAnimationUpdate = performance.now();
    const tick = (now: number) => {
      this.updateFrame = null;
      if (this.audio.paused || !this.updateListeners.size) return;
      if (now - this.lastAnimationUpdate >= UPDATE_INTERVAL_MS) {
        this.lastAnimationUpdate = now;
        this.emitUpdate();
      }
      this.updateFrame = requestFrame(tick);
    };
    this.updateFrame = requestFrame(tick);
  }

  private stopUpdateLoop(): void {
    if (this.updateFrame !== null) cancelFrame(this.updateFrame);
    this.updateFrame = null;
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
    clearSource(this.preloadAudio);
    this.preloadAudio.volume = this.configuredVolume;
    this.audio.volume = this.configuredVolume;
  }

  private applyTransitionVolumes(gains: TransitionGains): void {
    gains.previousAudio.volume = normalizedAudioVolume(this.configuredVolume * gains.previous);
    gains.nextAudio.volume = normalizedAudioVolume(this.configuredVolume * gains.next);
  }
}

function finiteValue(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0;
}

function abortError(): DOMException {
  return new DOMException("音频加载已取消", "AbortError");
}

const HAVE_FUTURE_DATA = 3;
const AUDIO_LOAD_TIMEOUT_MS = 30_000;
const UPDATE_INTERVAL_MS = 1_000 / 30;

async function waitUntilPlayable(audio: HTMLAudioElement, url: string, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) throw signal.reason ?? abortError();
  await new Promise<void>((resolve, reject) => {
    let settled = false;
    const cleanup = () => {
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
      clearSource(audio);
      reject(cause);
    });
    const ready = () => settle(resolve);
    const failed = () => rejectAndClear(new Error(audio.error?.message || "下一首预加载失败"));
    const aborted = () => rejectAndClear(signal?.reason ?? abortError());
    const timer = window.setTimeout(() => rejectAndClear(new Error("下一首预加载超时，请重试")), AUDIO_LOAD_TIMEOUT_MS);
    audio.addEventListener("canplay", ready, { once: true });
    audio.addEventListener("error", failed, { once: true });
    signal?.addEventListener("abort", aborted, { once: true });
    try {
      audio.preload = "auto";
      audio.src = url;
      audio.load();
      if (audio.readyState >= HAVE_FUTURE_DATA) ready();
    } catch (cause) {
      rejectAndClear(cause);
    }
  });
}

function clearSource(audio: HTMLAudioElement): void {
  try { audio.currentTime = 0; } catch { /* Some engines reject seeking without media. */ }
  audio.removeAttribute("src");
  try { audio.load(); } catch { /* Source is already detached. */ }
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
