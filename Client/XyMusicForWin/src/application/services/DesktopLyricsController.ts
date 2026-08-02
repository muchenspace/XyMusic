import type {
  DesktopLyrics,
  DesktopLyricsAction,
  DesktopLyricsClock,
  DesktopLyricsSnapshot,
  DesktopLyricsWindowState,
} from "../ports/DesktopLyrics";
import type {
  DesktopLyricsController as DesktopLyricsControllerPort,
  DesktopLyricsPlaybackRequest,
  DesktopLyricsControllerState,
} from "../ports/DesktopLyricsController";
import type { PageLifecycle } from "../ports/PageLifecycle";
import type { TaskScheduler } from "../ports/TaskScheduler";
import {
  DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
  DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
  type DesktopLyricsFullscreenBehavior,
  type UserInterfacePreferences,
} from "../ports/UserInterfacePreferences";
import { SerialTaskQueue } from "./SerialTaskQueue";

/**
 * Serializes native desktop-lyrics window updates and keeps its durable
 * preferences outside Vue state. The state is intentionally primitive-only so
 * presentation can cheaply project every native window-state event.
 */
export class DesktopLyricsController implements DesktopLyricsControllerPort {
  private readonly listeners = new Set<(state: DesktopLyricsControllerState) => void>();
  private readonly playbackRequestListeners = new Set<(request: DesktopLyricsPlaybackRequest) => void>();
  private readonly transitions = new SerialTaskQueue();
  private readonly removePageHide: () => void;
  private stateValue: DesktopLyricsControllerState;
  private initialized = false;
  private initializePromise: Promise<void> | null = null;
  private initializeAttempts = 0;
  private cancelInitializeRetry: (() => void) | undefined;
  private removeWindowState: (() => void) | undefined;
  private removeActions: (() => void) | undefined;
  private actionSubscription: Promise<boolean> | undefined;
  private pendingFontScale: number | null = null;
  private cancelFontScaleWrite: (() => void) | undefined;
  private disposed = false;
  private latestNativeStateRevision = -1;
  private pendingVisibleRevision: number | null = null;
  private pendingLockedRevision: number | null = null;
  private pendingFullscreenBehaviorRevision: number | null = null;
  private visibleRevision = 0;
  private lockedRevision = 0;
  private fullscreenBehaviorRevision = 0;

  constructor(
    private readonly integration: DesktopLyrics,
    private readonly preferences: UserInterfacePreferences,
    private readonly scheduler: TaskScheduler,
    pageLifecycle: PageLifecycle,
  ) {
    const stored = preferences.readDesktopLyrics();
    this.stateValue = freezeState({
      visible: stored.visible,
      actuallyVisible: false,
      locked: stored.locked,
      hiddenForFullscreen: false,
      fullscreenBehavior: stored.fullscreenBehavior,
      fontScale: stored.fontScale,
      textColor: stored.textColor,
      highlightColor: stored.highlightColor,
    });
    this.removePageHide = pageLifecycle.onPageHide(() => this.flushPreferences());
  }

  state(): DesktopLyricsControllerState {
    return this.stateValue;
  }

  subscribe(listener: (state: DesktopLyricsControllerState) => void): () => void {
    this.listeners.add(listener);
    listener(this.stateValue);
    return () => this.listeners.delete(listener);
  }

  initialize(): Promise<void> {
    if (this.initialized || this.disposed) return Promise.resolve();
    if (this.initializePromise) return this.initializePromise;
    this.initializePromise = this.transitions.run(async () => {
      if (this.disposed) return;
      const requestedVisible = this.stateValue.visible;
      const requestedLocked = this.stateValue.locked;
      const requestedFullscreenBehavior = this.stateValue.fullscreenBehavior;
      let latestState: DesktopLyricsWindowState | null = null;
      let succeeded = await this.ensureWindowStateListener();
      if (!await this.ensureActionListener()) succeeded = false;
      const fullscreenState = await this.nativeState(() => this.integration.setFullscreenBehavior(requestedFullscreenBehavior));
      if (fullscreenState) latestState = fullscreenState;
      else succeeded = false;
      const lockedState = await this.nativeState(() => this.integration.setLocked(requestedLocked));
      if (lockedState) latestState = lockedState;
      else succeeded = false;
      const visibleState = await this.nativeState(() => this.integration.setVisible(requestedVisible));
      if (visibleState) latestState = visibleState;
      else succeeded = false;
      const refreshedState = await this.nativeState(() => this.integration.getWindowState());
      if (refreshedState) latestState = refreshedState;
      else succeeded = false;
      if (latestState) this.applyWindowState(latestState);
      this.initialized = succeeded;
      if (!succeeded) this.scheduleInitializeRetry();
    }).finally(() => {
      this.initializePromise = null;
    });
    return this.initializePromise;
  }

  setVisible(value: boolean): Promise<void> {
    if (this.disposed) return Promise.resolve();
    const previous = this.stateValue.visible;
    const revision = ++this.visibleRevision;
    this.pendingVisibleRevision = revision;
    this.updateState({ visible: value });
    if (previous !== value) this.preferences.writeDesktopLyricsVisible(value);
    return this.transitions.run(async () => {
      try {
        if (this.disposed) return;
        await this.ensureWindowStateListener();
        const state = await this.nativeState(() => this.integration.setVisible(value));
        if (state) {
          if (revision === this.visibleRevision) this.applyWindowState(state);
          return;
        }
        if (revision === this.visibleRevision) await this.recoverWindowState(
          () => revision === this.visibleRevision,
          () => {
            const changed = this.stateValue.visible !== previous;
            this.updateState({ visible: previous });
            if (changed) this.preferences.writeDesktopLyricsVisible(previous);
          },
        );
      } finally {
        if (this.pendingVisibleRevision === revision) this.pendingVisibleRevision = null;
      }
    });
  }

  toggleVisible(): Promise<void> {
    return this.setVisible(!this.stateValue.visible);
  }

  setLocked(value: boolean): Promise<void> {
    if (this.disposed) return Promise.resolve();
    const previous = this.stateValue.locked;
    const revision = ++this.lockedRevision;
    this.pendingLockedRevision = revision;
    this.updateState({ locked: value });
    if (previous !== value) this.preferences.writeDesktopLyricsLocked(value);
    return this.transitions.run(async () => {
      try {
        if (this.disposed) return;
        await this.ensureWindowStateListener();
        const state = await this.nativeState(() => this.integration.setLocked(value));
        if (state) {
          if (revision === this.lockedRevision) this.applyWindowState(state);
          return;
        }
        if (revision === this.lockedRevision) await this.recoverWindowState(
          () => revision === this.lockedRevision,
          () => {
            const changed = this.stateValue.locked !== previous;
            this.updateState({ locked: previous });
            if (changed) this.preferences.writeDesktopLyricsLocked(previous);
          },
        );
      } finally {
        if (this.pendingLockedRevision === revision) this.pendingLockedRevision = null;
      }
    });
  }

  setFullscreenBehavior(value: DesktopLyricsFullscreenBehavior): Promise<void> {
    if (this.disposed) return Promise.resolve();
    const previous = this.stateValue.fullscreenBehavior;
    const revision = ++this.fullscreenBehaviorRevision;
    this.pendingFullscreenBehaviorRevision = revision;
    this.updateState({ fullscreenBehavior: value });
    if (previous !== value) this.preferences.writeDesktopLyricsFullscreenBehavior(value);
    return this.transitions.run(async () => {
      try {
        if (this.disposed) return;
        await this.ensureWindowStateListener();
        const state = await this.nativeState(() => this.integration.setFullscreenBehavior(value));
        if (state) {
          if (revision === this.fullscreenBehaviorRevision) this.applyWindowState(state);
          return;
        }
        if (revision === this.fullscreenBehaviorRevision) await this.recoverWindowState(
          () => revision === this.fullscreenBehaviorRevision,
          () => {
            const changed = this.stateValue.fullscreenBehavior !== previous;
            this.updateState({ fullscreenBehavior: previous });
            if (changed) this.preferences.writeDesktopLyricsFullscreenBehavior(previous);
          },
        );
      } finally {
        if (this.pendingFullscreenBehaviorRevision === revision) this.pendingFullscreenBehaviorRevision = null;
      }
    });
  }

  setFontScale(value: number): void {
    if (this.disposed) return;
    const normalized = normalizeFontScale(value);
    if (normalized === this.stateValue.fontScale) return;
    this.updateState({ fontScale: normalized });
    this.pendingFontScale = normalized;
    this.cancelFontScaleWrite?.();
    this.cancelFontScaleWrite = this.scheduler.delay(() => this.flushFontScale(), FONT_SCALE_PERSIST_DEBOUNCE_MS);
  }

  setTextColor(value: string): void {
    if (this.disposed) return;
    const normalized = normalizeColor(value, DEFAULT_DESKTOP_LYRICS_TEXT_COLOR);
    if (normalized === this.stateValue.textColor) return;
    this.updateState({ textColor: normalized });
    this.preferences.writeDesktopLyricsTextColor(normalized);
  }

  setHighlightColor(value: string): void {
    if (this.disposed) return;
    const normalized = normalizeColor(value, DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR);
    if (normalized === this.stateValue.highlightColor) return;
    this.updateState({ highlightColor: normalized });
    this.preferences.writeDesktopLyricsHighlightColor(normalized);
  }

  subscribePlaybackRequests(listener: (request: DesktopLyricsPlaybackRequest) => void): () => void {
    this.playbackRequestListeners.add(listener);
    void this.ensureActionListener();
    return () => this.playbackRequestListeners.delete(listener);
  }

  sendSnapshot(snapshot: DesktopLyricsSnapshot): Promise<void> {
    return this.disposed ? Promise.resolve() : this.integration.sendSnapshot(snapshot);
  }

  sendClock(clock: DesktopLyricsClock): Promise<void> {
    return this.disposed ? Promise.resolve() : this.integration.sendClock(clock);
  }

  dispose(): void {
    if (this.disposed) return;
    this.flushPreferences();
    this.disposed = true;
    this.cancelInitializeRetry?.();
    this.cancelInitializeRetry = undefined;
    this.removePageHide();
    this.removeWindowState?.();
    this.removeWindowState = undefined;
    this.listeners.clear();
    this.playbackRequestListeners.clear();
    this.removeActions?.();
    this.removeActions = undefined;
  }

  private applyWindowState(state: DesktopLyricsWindowState, reconcileIntent = false): void {
    if (this.disposed) return;
    const nativeRevision = normalizeNativeStateRevision(state.revision);
    if (nativeRevision < this.latestNativeStateRevision) return;
    this.latestNativeStateRevision = nativeRevision;
    const previous = this.stateValue;
    const acceptsVisibleIntent = reconcileIntent
      || this.pendingVisibleRevision === null
      || state.requestedVisible === previous.visible;
    const acceptsLockedIntent = reconcileIntent
      || this.pendingLockedRevision === null
      || state.locked === previous.locked;
    const acceptsFullscreenIntent = reconcileIntent
      || this.pendingFullscreenBehaviorRevision === null
      || state.fullscreenBehavior === previous.fullscreenBehavior;
    const acceptsActualVisibility = acceptsVisibleIntent && acceptsFullscreenIntent;
    if (acceptsVisibleIntent && previous.visible !== state.requestedVisible) {
      this.preferences.writeDesktopLyricsVisible(state.requestedVisible);
    }
    if (acceptsLockedIntent && previous.locked !== state.locked) {
      this.preferences.writeDesktopLyricsLocked(state.locked);
    }
    if (acceptsFullscreenIntent && previous.fullscreenBehavior !== state.fullscreenBehavior) {
      this.preferences.writeDesktopLyricsFullscreenBehavior(state.fullscreenBehavior);
    }
    this.updateState({
      visible: acceptsVisibleIntent ? state.requestedVisible : previous.visible,
      actuallyVisible: acceptsActualVisibility ? state.visible : previous.actuallyVisible,
      locked: acceptsLockedIntent ? state.locked : previous.locked,
      hiddenForFullscreen: acceptsActualVisibility ? state.hiddenForFullscreen : previous.hiddenForFullscreen,
      fullscreenBehavior: acceptsFullscreenIntent ? state.fullscreenBehavior : previous.fullscreenBehavior,
    });
  }

  private async ensureWindowStateListener(): Promise<boolean> {
    if (this.disposed) return false;
    if (this.removeWindowState) return true;
    try {
      const remove = await this.integration.onWindowState((state) => this.applyWindowState(state));
      if (this.disposed) {
        remove();
        return false;
      }
      this.removeWindowState = remove;
      return true;
    } catch {
      return false;
    }
  }

  private ensureActionListener(): Promise<boolean> {
    if (this.disposed) return Promise.resolve(false);
    if (this.removeActions) return Promise.resolve(true);
    if (this.actionSubscription) return this.actionSubscription;
    let subscription!: Promise<boolean>;
    subscription = this.integration.onAction((action) => this.handleAction(action)).then((remove) => {
      if (this.disposed) {
        remove();
        return false;
      }
      this.removeActions = remove;
      return true;
    }).catch(() => false).finally(() => {
      if (this.actionSubscription === subscription) this.actionSubscription = undefined;
    });
    this.actionSubscription = subscription;
    return subscription;
  }

  private async recoverWindowState(isCurrent: () => boolean, fallback: () => void): Promise<void> {
    const state = await this.nativeState(() => this.integration.getWindowState());
    if (!isCurrent() || this.disposed) return;
    if (state) this.applyWindowState(state, true);
    else fallback();
  }

  private async nativeState(operation: () => Promise<DesktopLyricsWindowState>): Promise<DesktopLyricsWindowState | null> {
    if (this.disposed) return null;
    try {
      const state = await operation();
      return this.disposed ? null : state;
    } catch {
      return null;
    }
  }

  private scheduleInitializeRetry(): void {
    if (this.disposed || this.cancelInitializeRetry || this.initializeAttempts >= MAX_INITIALIZE_ATTEMPTS) return;
    this.initializeAttempts += 1;
    this.cancelInitializeRetry = this.scheduler.delay(() => {
      this.cancelInitializeRetry = undefined;
      void this.initialize();
    }, INITIALIZE_RETRY_BASE_MS * this.initializeAttempts);
  }

  private handleAction(action: DesktopLyricsAction): void {
    if (this.disposed) return;
    if (action.action === "set-font-scale") {
      this.setFontScale(action.value);
      return;
    }
    if (action.action === "set-text-color") {
      this.setTextColor(action.value);
      return;
    }
    if (action.action === "set-highlight-color") {
      this.setHighlightColor(action.value);
      return;
    }
    if (action.action === "lock") {
      void this.setLocked(true);
      return;
    }
    if (action.action === "close") {
      void this.setVisible(false);
      return;
    }
    for (const listener of this.playbackRequestListeners) listener(action.action);
  }

  private flushPreferences(): void {
    this.flushFontScale();
  }

  private flushFontScale(): void {
    this.cancelFontScaleWrite?.();
    this.cancelFontScaleWrite = undefined;
    const value = this.pendingFontScale;
    this.pendingFontScale = null;
    if (value !== null) this.preferences.writeDesktopLyricsFontScale(value);
  }

  private updateState(patch: Partial<DesktopLyricsControllerState>): void {
    const entries = Object.entries(patch) as [keyof DesktopLyricsControllerState, DesktopLyricsControllerState[keyof DesktopLyricsControllerState]][];
    if (!entries.some(([key, value]) => !Object.is(this.stateValue[key], value))) return;
    this.stateValue = freezeState({ ...this.stateValue, ...patch });
    for (const listener of this.listeners) listener(this.stateValue);
  }
}

function freezeState(state: DesktopLyricsControllerState): DesktopLyricsControllerState {
  return Object.freeze(state);
}

function normalizeFontScale(value: number): number {
  const rounded = Number(Number(value).toFixed(2));
  return Number.isFinite(rounded) ? Math.max(0.75, Math.min(1.5, rounded)) : 1;
}

function normalizeColor(value: string, fallback: string): string {
  return /^#[0-9a-f]{6}$/iu.test(value) ? value.toLowerCase() : fallback;
}

function normalizeNativeStateRevision(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

const MAX_INITIALIZE_ATTEMPTS = 3;
const INITIALIZE_RETRY_BASE_MS = 600;
export const FONT_SCALE_PERSIST_DEBOUNCE_MS = 180;
