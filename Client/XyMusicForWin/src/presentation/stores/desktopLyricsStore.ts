import { onScopeDispose, ref } from "vue";
import { defineStore } from "pinia";
import type { DesktopLyrics, DesktopLyricsWindowState } from "../../application/ports/DesktopLyrics";
import type { DesktopLyricsFullscreenBehavior } from "../../application/ports/UserInterfacePreferences";
import {
  DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
  DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
} from "../../application/ports/UserInterfacePreferences";
import { SerialTaskQueue } from "../../application/services/SerialTaskQueue";
import { useApplicationServices } from "../services";

export const useDesktopLyricsStore = defineStore("desktop-lyrics", () => {
  const services = useApplicationServices();
  const integration = services.desktopLyrics ?? NOOP_DESKTOP_LYRICS;
  const preferences = services.uiPreferences;
  const stored = preferences.readDesktopLyrics?.() ?? DEFAULT_PREFERENCES;
  const visible = ref(stored.visible);
  const actuallyVisible = ref(false);
  const locked = ref(stored.locked);
  const hiddenForFullscreen = ref(false);
  const fullscreenBehavior = ref<DesktopLyricsFullscreenBehavior>(stored.fullscreenBehavior);
  const fontScale = ref(stored.fontScale);
  const textColor = ref(stored.textColor);
  const highlightColor = ref(stored.highlightColor);
  const wordLyricsEnabled = ref(stored.wordLyricsEnabled ?? true);
  const transitions = new SerialTaskQueue();
  let initialized = false;
  let initializePromise: Promise<void> | null = null;
  let initializeAttempts = 0;
  let initializeRetryTimer = 0;
  let removeWindowState: (() => void) | undefined;
  let disposed = false;
  let visibleRevision = 0;
  let lockedRevision = 0;
  let fullscreenBehaviorRevision = 0;

  function initialize(): Promise<void> {
    if (initialized || disposed) return Promise.resolve();
    if (initializePromise) return initializePromise;
    initializePromise = transitions.run(async () => {
      const requestedVisible = visible.value;
      const requestedLocked = locked.value;
      const requestedFullscreenBehavior = fullscreenBehavior.value;
      let latestState: DesktopLyricsWindowState | null = null;
      let succeeded = await ensureWindowStateListener();
      const fullscreenState = await nativeState(() => integration.setFullscreenBehavior(requestedFullscreenBehavior));
      if (fullscreenState) latestState = fullscreenState;
      else succeeded = false;
      const lockedState = await nativeState(() => integration.setLocked(requestedLocked));
      if (lockedState) latestState = lockedState;
      else succeeded = false;
      const visibleState = await nativeState(() => integration.setVisible(requestedVisible));
      if (visibleState) latestState = visibleState;
      else succeeded = false;
      const refreshedState = await nativeState(() => integration.getWindowState());
      if (refreshedState) latestState = refreshedState;
      else succeeded = false;
      if (latestState) applyWindowState(latestState);
      initialized = succeeded;
      if (!succeeded) scheduleInitializeRetry();
    }).finally(() => {
      initializePromise = null;
    });
    return initializePromise;
  }

  function setVisible(value: boolean): Promise<void> {
    const previous = visible.value;
    const revision = ++visibleRevision;
    visible.value = value;
    preferences.writeDesktopLyricsVisible?.(value);
    return transitions.run(async () => {
      await ensureWindowStateListener();
      const state = await nativeState(() => integration.setVisible(value));
      if (state) {
        if (revision === visibleRevision) applyWindowState(state);
        return;
      }
      if (revision === visibleRevision) await recoverWindowState(() => {
        visible.value = previous;
        preferences.writeDesktopLyricsVisible?.(previous);
      });
    });
  }

  function toggleVisible(): Promise<void> {
    return setVisible(!visible.value);
  }

  function setLocked(value: boolean): Promise<void> {
    const previous = locked.value;
    const revision = ++lockedRevision;
    locked.value = value;
    preferences.writeDesktopLyricsLocked?.(value);
    return transitions.run(async () => {
      await ensureWindowStateListener();
      const state = await nativeState(() => integration.setLocked(value));
      if (state) {
        if (revision === lockedRevision) applyWindowState(state);
        return;
      }
      if (revision === lockedRevision) await recoverWindowState(() => {
        locked.value = previous;
        preferences.writeDesktopLyricsLocked?.(previous);
      });
    });
  }

  function setFullscreenBehavior(value: DesktopLyricsFullscreenBehavior): Promise<void> {
    const previous = fullscreenBehavior.value;
    const revision = ++fullscreenBehaviorRevision;
    fullscreenBehavior.value = value;
    preferences.writeDesktopLyricsFullscreenBehavior?.(value);
    return transitions.run(async () => {
      await ensureWindowStateListener();
      const state = await nativeState(() => integration.setFullscreenBehavior(value));
      if (state) {
        if (revision === fullscreenBehaviorRevision) applyWindowState(state);
        return;
      }
      if (revision === fullscreenBehaviorRevision) await recoverWindowState(() => {
        fullscreenBehavior.value = previous;
        preferences.writeDesktopLyricsFullscreenBehavior?.(previous);
      });
    });
  }

  function setFontScale(value: number): void {
    const normalized = Math.max(0.75, Math.min(1.5, Number(Number(value).toFixed(2))));
    fontScale.value = Number.isFinite(normalized) ? normalized : 1;
    preferences.writeDesktopLyricsFontScale?.(fontScale.value);
  }

  function setTextColor(value: string): void {
    textColor.value = normalizeColor(value, DEFAULT_PREFERENCES.textColor);
    preferences.writeDesktopLyricsTextColor?.(textColor.value);
  }

  function setHighlightColor(value: string): void {
    highlightColor.value = normalizeColor(value, DEFAULT_PREFERENCES.highlightColor);
    preferences.writeDesktopLyricsHighlightColor?.(highlightColor.value);
  }

  function setWordLyricsEnabled(enabled: boolean): void {
    wordLyricsEnabled.value = enabled;
    preferences.writeDesktopLyricsWordLyricsEnabled?.(enabled);
  }

  function applyWindowState(state: DesktopLyricsWindowState): void {
    visible.value = state.requestedVisible;
    actuallyVisible.value = state.visible;
    locked.value = state.locked;
    hiddenForFullscreen.value = state.hiddenForFullscreen;
    fullscreenBehavior.value = state.fullscreenBehavior;
    preferences.writeDesktopLyricsVisible?.(state.requestedVisible);
    preferences.writeDesktopLyricsLocked?.(state.locked);
    preferences.writeDesktopLyricsFullscreenBehavior?.(state.fullscreenBehavior);
  }

  async function ensureWindowStateListener(): Promise<boolean> {
    if (removeWindowState) return true;
    try {
      removeWindowState = await integration.onWindowState(applyWindowState);
      return true;
    } catch {
      return false;
    }
  }

  async function recoverWindowState(fallback: () => void): Promise<void> {
    const state = await nativeState(() => integration.getWindowState());
    if (state) applyWindowState(state);
    else fallback();
  }

  async function nativeState(operation: () => Promise<DesktopLyricsWindowState>): Promise<DesktopLyricsWindowState | null> {
    try {
      return await operation();
    } catch {
      return null;
    }
  }

  function scheduleInitializeRetry(): void {
    if (disposed || initializeRetryTimer || initializeAttempts >= MAX_INITIALIZE_ATTEMPTS) return;
    initializeAttempts += 1;
    initializeRetryTimer = window.setTimeout(() => {
      initializeRetryTimer = 0;
      void initialize();
    }, INITIALIZE_RETRY_BASE_MS * initializeAttempts);
  }

  onScopeDispose(() => {
    disposed = true;
    if (initializeRetryTimer) window.clearTimeout(initializeRetryTimer);
    removeWindowState?.();
  });

  return {
    visible,
    actuallyVisible,
    locked,
    hiddenForFullscreen,
    fullscreenBehavior,
    fontScale,
    textColor,
    highlightColor,
    wordLyricsEnabled,
    initialize,
    setVisible,
    toggleVisible,
    setLocked,
    setFullscreenBehavior,
    setFontScale,
    setTextColor,
    setHighlightColor,
    setWordLyricsEnabled,
  };
});

const DEFAULT_PREFERENCES = {
  visible: false,
  locked: false,
  fullscreenBehavior: "show" as const,
  fontScale: 1,
  textColor: DEFAULT_DESKTOP_LYRICS_TEXT_COLOR,
  highlightColor: DEFAULT_DESKTOP_LYRICS_HIGHLIGHT_COLOR,
  wordLyricsEnabled: true,
};

const MAX_INITIALIZE_ATTEMPTS = 3;
const INITIALIZE_RETRY_BASE_MS = 600;

const NOOP_DESKTOP_LYRICS: DesktopLyrics = {
  async getWindowState() { return noopState(); },
  async setVisible(value) { return { ...noopState(), requestedVisible: value, visible: value }; },
  async toggleVisible() { return noopState(); },
  async setLocked(value) { return { ...noopState(), locked: value }; },
  async setFullscreenBehavior(value) { return { ...noopState(), fullscreenBehavior: value }; },
  async sendSnapshot() {},
  async sendClock() {},
  async onAction() { return () => undefined; },
  async onWindowState() { return () => undefined; },
};

function noopState(): DesktopLyricsWindowState {
  return {
    requestedVisible: false,
    visible: false,
    locked: false,
    hiddenForFullscreen: false,
    fullscreenBehavior: "show",
  };
}

function normalizeColor(value: string, fallback: string): string {
  return /^#[0-9a-f]{6}$/iu.test(value) ? value.toLowerCase() : fallback;
}
