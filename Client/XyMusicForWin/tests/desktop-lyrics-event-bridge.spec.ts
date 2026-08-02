import { readFileSync } from "node:fs";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DesktopLyricsEventTransport } from "../src/infrastructure/desktop/TauriDesktopLyricsEventBridge";
import { TauriDesktopLyricsEventBridge } from "../src/infrastructure/desktop/TauriDesktopLyricsEventBridge";
import type {
  DesktopLyricsActionPayload,
  DesktopLyricsClockPayload,
  DesktopLyricsStatePayload,
} from "../src/desktop-lyrics/protocol";
import { DESKTOP_LYRICS_EVENTS } from "../src/desktop-lyrics/protocol";

const projectRoot = path.resolve(import.meta.dirname, "..");

describe("desktop lyrics event adapter", () => {
  afterEach(() => {
    delete (window as Window & { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__;
    vi.useRealTimers();
  });

  it("maps state, clock, and action events through the Tauri transport", async () => {
    (window as Window & { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__ = {};
    const removeStateListener = vi.fn();
    let stateEventListener: ((event: { payload: DesktopLyricsStatePayload }) => void) | undefined;
    const listenSpy = vi.fn();
    const emitSpy = vi.fn();
    const events: DesktopLyricsEventTransport = {
      async listen<T>(
        eventName: string,
        listener: (event: { payload: T }) => void,
      ) {
        listenSpy(eventName, listener);
        if (eventName === DESKTOP_LYRICS_EVENTS.state) {
          stateEventListener = listener as (event: { payload: DesktopLyricsStatePayload }) => void;
        }
        return removeStateListener;
      },
      async emit<T>(eventName: string, payload: T) {
        emitSpy(eventName, payload);
      },
    };
    const bridge = new TauriDesktopLyricsEventBridge(events);
    const receivedState = vi.fn();
    const state = desktopLyricsState();
    const action = desktopLyricsAction();

    const unlisten = await bridge.onState(receivedState);
    stateEventListener?.({ payload: state });
    await bridge.emitAction(action);

    expect(listenSpy).toHaveBeenCalledWith(DESKTOP_LYRICS_EVENTS.state, expect.any(Function));
    expect(receivedState).toHaveBeenCalledWith(state);
    expect(emitSpy).toHaveBeenCalledWith(DESKTOP_LYRICS_EVENTS.action, action);
    unlisten();
    expect(removeStateListener).toHaveBeenCalledOnce();
  });

  it("uses browser events outside Tauri", async () => {
    const failedListen = vi.fn();
    const failedEmit = vi.fn();
    const events: DesktopLyricsEventTransport = {
      async listen<T>(eventName: string, listener: (event: { payload: T }) => void) {
        failedListen(eventName, listener);
        throw new Error("event transport unavailable");
      },
      async emit<T>(eventName: string, payload: T) {
        failedEmit(eventName, payload);
        throw new Error("event transport unavailable");
      },
    };
    const bridge = new TauriDesktopLyricsEventBridge(events);
    const receivedClock = vi.fn();
    const receivedAction = vi.fn();
    const clock = desktopLyricsClock();
    const action = desktopLyricsAction();
    const removeActionListener = (event: Event) => receivedAction((event as CustomEvent<DesktopLyricsActionPayload>).detail);
    window.addEventListener(DESKTOP_LYRICS_EVENTS.action, removeActionListener);

    const unlisten = await bridge.onClock(receivedClock);
    window.dispatchEvent(new CustomEvent(DESKTOP_LYRICS_EVENTS.clock, { detail: clock }));
    await bridge.emitAction(action);

    expect(receivedClock).toHaveBeenCalledWith(clock);
    expect(receivedAction).toHaveBeenCalledWith(action);
    expect(failedListen).not.toHaveBeenCalled();
    expect(failedEmit).not.toHaveBeenCalled();

    unlisten();
    window.removeEventListener(DESKTOP_LYRICS_EVENTS.action, removeActionListener);
    expect(failedListen).not.toHaveBeenCalled();
    expect(failedEmit).not.toHaveBeenCalled();
  });

  it("retries Tauri delivery without treating webview-local browser events as a fallback", async () => {
    vi.useFakeTimers();
    (window as Window & { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__ = {};
    const nativeUnlisten = vi.fn();
    let nativeStateListener: ((event: { payload: DesktopLyricsStatePayload }) => void) | undefined;
    let listenAttempts = 0;
    let emitAttempts = 0;
    const emitted = vi.fn();
    const recoveredState = desktopLyricsState({ revision: 2 });
    const events: DesktopLyricsEventTransport = {
      async listen<T>(
        _eventName: string,
        listener: (event: { payload: T }) => void,
      ) {
        listenAttempts += 1;
        if (listenAttempts === 1) throw new Error("event transport unavailable");
        nativeStateListener = listener as (event: { payload: DesktopLyricsStatePayload }) => void;
        return nativeUnlisten;
      },
      async emit<T>(eventName: string, payload: T) {
        emitAttempts += 1;
        if (emitAttempts === 1) throw new Error("event transport unavailable");
        emitted(eventName, payload);
        nativeStateListener?.({ payload: recoveredState });
      },
    };
    const bridge = new TauriDesktopLyricsEventBridge(events);
    const received = vi.fn();
    const unlisten = await bridge.onState(received);
    const fallbackState = desktopLyricsState({ revision: 1 });

    window.dispatchEvent(new CustomEvent(DESKTOP_LYRICS_EVENTS.state, { detail: fallbackState }));
    expect(received).not.toHaveBeenCalled();

    const action = desktopLyricsAction();
    const delivery = bridge.emitAction(action);
    await Promise.resolve();
    await vi.advanceTimersByTimeAsync(75);
    await delivery;
    expect(emitted).toHaveBeenCalledWith(DESKTOP_LYRICS_EVENTS.action, action);

    await vi.advanceTimersByTimeAsync(175);
    await waitFor(() => emitted.mock.calls.length === 2);
    expect(listenAttempts).toBe(2);
    expect(received).toHaveBeenLastCalledWith(recoveredState);

    window.dispatchEvent(new CustomEvent(DESKTOP_LYRICS_EVENTS.state, { detail: desktopLyricsState({ revision: 3 }) }));
    expect(received).toHaveBeenLastCalledWith(recoveredState);

    unlisten();
    expect(nativeUnlisten).toHaveBeenCalledOnce();
  });

  it("cancels a pending Tauri listener retry when the consumer unlistens", async () => {
    vi.useFakeTimers();
    (window as Window & { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__ = {};
    const listen = vi.fn(async () => { throw new Error("event transport unavailable"); });
    const events: DesktopLyricsEventTransport = {
      listen,
      async emit() {},
    };
    const bridge = new TauriDesktopLyricsEventBridge(events);

    const unlisten = await bridge.onState(vi.fn());
    unlisten();
    await vi.runOnlyPendingTimersAsync();

    expect(listen).toHaveBeenCalledOnce();
  });

  it("does not dispatch failed native control actions to the overlay's local window", async () => {
    vi.useFakeTimers();
    (window as Window & { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__ = {};
    const receivedLocalAction = vi.fn();
    const receiveLocalAction = (event: Event) => receivedLocalAction((event as CustomEvent<DesktopLyricsActionPayload>).detail);
    window.addEventListener(DESKTOP_LYRICS_EVENTS.action, receiveLocalAction);
    const events: DesktopLyricsEventTransport = {
      async listen() { return () => undefined; },
      async emit() { throw new Error("event transport unavailable"); },
    };
    const bridge = new TauriDesktopLyricsEventBridge(events);

    try {
      const delivery = bridge.emitAction({ version: 4, action: "next", issuedAtMs: 1 });
      const rejected = expect(delivery).rejects.toThrow("event transport unavailable");
      await Promise.resolve();
      await vi.advanceTimersByTimeAsync(1_075);

      await rejected;
      expect(receivedLocalAction).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener(DESKTOP_LYRICS_EVENTS.action, receiveLocalAction);
    }
  });

  it("keeps desktop lyrics presentation free of concrete event transports and injects the adapter at startup", () => {
    const bridgeSource = readSource("src/desktop-lyrics/bridge.ts");
    const placementSource = readSource("src/desktop-lyrics/windowPlacement.ts");
    const adapterSource = readSource("src/infrastructure/desktop/TauriDesktopLyricsEventBridge.ts");
    const placementAdapterSource = readSource("src/infrastructure/windows/TauriDesktopLyricsWindowPlacement.ts");
    const desktopLyricsAdapterSource = readSource("src/infrastructure/windows/TauriDesktopLyrics.ts");
    const mainSource = readSource("src/main.ts");

    expect(bridgeSource).not.toContain("@tauri-apps/api/event");
    expect(bridgeSource).not.toContain("window.addEventListener");
    expect(bridgeSource).not.toContain("window.dispatchEvent");
    expect(placementSource).not.toContain("@tauri-apps/api/window");
    expect(placementSource).not.toContain("localStorage");
    expect(adapterSource).toContain('from "@tauri-apps/api/event"');
    expect(desktopLyricsAdapterSource).toContain('from "../../application/ports/DesktopLyricsBridge"');
    expect(desktopLyricsAdapterSource).toContain("DESKTOP_LYRICS_EVENTS.state");
    expect(desktopLyricsAdapterSource).toContain("DESKTOP_LYRICS_EVENTS.clock");
    expect(desktopLyricsAdapterSource).toContain("DESKTOP_LYRICS_EVENTS.action");
    expect(desktopLyricsAdapterSource).not.toContain("xy-music://desktop-lyrics/state");
    expect(placementAdapterSource).toContain('@tauri-apps/api/window');
    expect(placementAdapterSource).toContain("localStorage");
    expect(mainSource).toContain('import("./infrastructure/desktop/TauriDesktopLyricsEventBridge")');
    expect(mainSource).toContain('import("./infrastructure/windows/TauriDesktopLyricsWindowPlacement")');
    expect(mainSource).toContain("bridge: new TauriDesktopLyricsEventBridge()");
    expect(mainSource).toContain("placement: new TauriDesktopLyricsWindowPlacement()");
  });
});

function desktopLyricsState(overrides: Partial<DesktopLyricsStatePayload> = {}): DesktopLyricsStatePayload {
  return {
    version: 4,
    transportEpoch: "test-main-window",
    revision: 1,
    track: null,
    lyrics: null,
    isPlaying: false,
    positionSeconds: 0,
    anchoredAtMs: 1,
    offsetSeconds: 0,
    showTranslation: false,
    locked: false,
    fontScale: 1,
    ...overrides,
  };
}

function desktopLyricsClock(): DesktopLyricsClockPayload {
  return {
    version: 4,
    transportEpoch: "test-main-window",
    trackId: null,
    isPlaying: false,
    positionSeconds: 0,
    anchoredAtMs: 1,
  };
}

function desktopLyricsAction(): DesktopLyricsActionPayload {
  return { version: 4, action: "ready", issuedAtMs: 1 };
}

function readSource(relativePath: string): string {
  return readFileSync(path.join(projectRoot, relativePath), "utf8");
}

async function waitFor(condition: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    if (condition()) return;
    await Promise.resolve();
  }
  throw new Error("Expected condition to become true");
}
