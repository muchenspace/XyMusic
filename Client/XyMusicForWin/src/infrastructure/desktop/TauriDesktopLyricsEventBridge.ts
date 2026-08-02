import { emit, listen } from "@tauri-apps/api/event";
import {
  DESKTOP_LYRICS_EVENTS,
  type DesktopLyricsBridge,
  type DesktopLyricsUnlisten,
  type DesktopLyricsActionPayload,
  type DesktopLyricsClockPayload,
  type DesktopLyricsStatePayload,
} from "../../application/ports/DesktopLyricsBridge";

export interface DesktopLyricsEventTransport {
  listen<T>(eventName: string, listener: (event: { payload: T }) => void): Promise<DesktopLyricsUnlisten>;
  emit<T>(eventName: string, payload: T): Promise<void>;
}

/**
 * Infrastructure adapter for the desktop lyrics window event protocol.
 * Browser CustomEvents preserve preview behavior only when the Tauri host is
 * absent. They cannot cross between native webviews.
 */
export class TauriDesktopLyricsEventBridge implements DesktopLyricsBridge {
  private actionQueue: Promise<void> = Promise.resolve();
  private readyAction: Extract<DesktopLyricsActionPayload, { action: "ready" }> | null = null;
  private stateListenerCount = 0;

  constructor(
    private readonly events: DesktopLyricsEventTransport = tauriDesktopLyricsEvents,
    private readonly browserEvents: EventTarget | undefined = browserEventTarget(),
  ) {}

  async onState(listener: (state: DesktopLyricsStatePayload) => void): Promise<DesktopLyricsUnlisten> {
    this.stateListenerCount += 1;
    try {
      const unlisten = await this.listenEvent(
        DESKTOP_LYRICS_EVENTS.state,
        listener,
        () => this.replayReadyAction(),
      );
      let stopped = false;
      return () => {
        if (stopped) return;
        stopped = true;
        this.stateListenerCount -= 1;
        if (!this.stateListenerCount) this.readyAction = null;
        unlisten();
      };
    } catch (error) {
      this.stateListenerCount -= 1;
      throw error;
    }
  }

  onClock(listener: (clock: DesktopLyricsClockPayload) => void): Promise<DesktopLyricsUnlisten> {
    return this.listenEvent(DESKTOP_LYRICS_EVENTS.clock, listener);
  }

  async emitAction(action: DesktopLyricsActionPayload): Promise<void> {
    if (action.action === "ready") this.readyAction = action;
    // Tauri event delivery is ordered per webview, but a renderer can become
    // ready a fraction after the overlay mounts. Keep actions ordered while
    // bounded retries bridge that startup window.
    await this.enqueueAction(action);
  }

  private async listenEvent<T>(
    eventName: string,
    listener: (payload: T) => void,
    onRecovered?: () => void,
  ): Promise<DesktopLyricsUnlisten> {
    if (!isTauriRuntime()) return listenBrowserEvent(this.browserEvents, eventName, listener);

    let disposed = false;
    let retryTimer: number | undefined;
    let retryDelay = TAURI_LISTEN_RETRY_INITIAL_DELAY_MS;
    let tauriUnlisten: DesktopLyricsUnlisten | undefined;
    let recovering = false;

    const stop = () => {
      if (disposed) return;
      disposed = true;
      if (retryTimer !== undefined) window.clearTimeout(retryTimer);
      retryTimer = undefined;
      const activeTauriUnlisten = tauriUnlisten;
      tauriUnlisten = undefined;
      unlistenSafely(activeTauriUnlisten);
    };

    const retry = () => {
      if (disposed || retryTimer !== undefined || !isTauriRuntime()) return;
      const delay = retryDelay;
      retryDelay = Math.min(retryDelay * 2, TAURI_LISTEN_RETRY_MAX_DELAY_MS);
      retryTimer = window.setTimeout(() => {
        retryTimer = undefined;
        void subscribe();
      }, delay);
    };

    const subscribe = async (): Promise<void> => {
      try {
        const unlisten = await this.events.listen<T>(eventName, ({ payload }) => listener(payload));
        if (disposed) {
          unlistenSafely(unlisten);
          return;
        }
        tauriUnlisten = unlisten;
        retryDelay = TAURI_LISTEN_RETRY_INITIAL_DELAY_MS;
        const shouldResynchronize = recovering;
        recovering = false;
        if (shouldResynchronize) onRecovered?.();
      } catch {
        if (disposed) return;
        // CustomEvents are local to one webview. They are a useful browser
        // preview transport, but cannot repair a failed Tauri subscription
        // between the main and overlay webviews.
        recovering = true;
        retry();
      }
    };

    await subscribe();
    return stop;
  }

  private enqueueAction(action: DesktopLyricsActionPayload): Promise<void> {
    const delivery = this.actionQueue
      .catch(() => undefined)
      .then(() => this.emitEvent(DESKTOP_LYRICS_EVENTS.action, action));
    this.actionQueue = delivery.catch(() => undefined);
    return delivery;
  }

  private replayReadyAction(): void {
    const action = this.readyAction;
    if (!action || !this.stateListenerCount || !isTauriRuntime()) return;
    // `ready` is idempotent: after a listener reconnects it asks the main
    // window to resend a snapshot that may have been emitted during downtime.
    void this.enqueueAction(action).catch(() => undefined);
  }

  private async emitEvent<T>(eventName: string, payload: T): Promise<void> {
    if (!isTauriRuntime()) {
      dispatchBrowserEvent(this.browserEvents, eventName, payload);
      return;
    }

    let failure: unknown;
    for (let attempt = 0; attempt <= TAURI_EMIT_RETRY_DELAYS_MS.length; attempt += 1) {
      if (!isTauriRuntime()) {
        dispatchBrowserEvent(this.browserEvents, eventName, payload);
        return;
      }
      try {
        await this.events.emit(eventName, payload);
        return;
      } catch (error) {
        failure = error;
        const delay = TAURI_EMIT_RETRY_DELAYS_MS[attempt];
        if (delay === undefined) break;
        await wait(delay);
      }
    }
    throw failure;
  }
}

const tauriDesktopLyricsEvents: DesktopLyricsEventTransport = {
  async listen<T>(
    eventName: string,
    listener: (event: { payload: T }) => void,
  ): Promise<DesktopLyricsUnlisten> {
    return listen<T>(eventName, ({ payload }) => listener({ payload }));
  },
  emit<T>(eventName: string, payload: T): Promise<void> {
    return emit(eventName, payload);
  },
};

function listenBrowserEvent<T>(
  eventTarget: EventTarget | undefined,
  eventName: string,
  listener: (payload: T) => void,
): DesktopLyricsUnlisten {
  if (!eventTarget) return noopUnlisten;
  const handleEvent = (event: Event) => listener((event as CustomEvent<T>).detail);
  eventTarget.addEventListener(eventName, handleEvent);
  return () => eventTarget.removeEventListener(eventName, handleEvent);
}

function dispatchBrowserEvent<T>(eventTarget: EventTarget | undefined, eventName: string, payload: T): void {
  if (!eventTarget || typeof CustomEvent === "undefined") return;
  eventTarget.dispatchEvent(new CustomEvent(eventName, { detail: payload }));
}

function unlistenSafely(unlisten: DesktopLyricsUnlisten | undefined): void {
  try {
    unlisten?.();
  } catch {
    // Teardown must not prevent other listeners from being released.
  }
}

function browserEventTarget(): EventTarget | undefined {
  return typeof window === "undefined" ? undefined : window;
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

const noopUnlisten: DesktopLyricsUnlisten = () => undefined;
const TAURI_LISTEN_RETRY_INITIAL_DELAY_MS = 250;
const TAURI_LISTEN_RETRY_MAX_DELAY_MS = 4_000;
const TAURI_EMIT_RETRY_DELAYS_MS = [75, 250, 750] as const;

function wait(delayMs: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, delayMs));
}
