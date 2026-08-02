import { createApp, type App } from "vue";
import type { DesktopLyricsWindowPlacement } from "../application/ports/DesktopLyricsWindowPlacement";
import DesktopLyricsApp from "./DesktopLyricsApp.vue";
import type { DesktopLyricsBridge } from "./bridge";
import type { DesktopLyricsStatePayload } from "./protocol";

export interface MountDesktopLyricsAppOptions {
  bridge: DesktopLyricsBridge;
  placement?: DesktopLyricsWindowPlacement;
  initialState?: DesktopLyricsStatePayload | null;
}

export function mountDesktopLyricsApp(
  target: string | Element = "#app",
  options: MountDesktopLyricsAppOptions,
): App<Element> {
  const rootProps: Record<string, unknown> = { ...options };
  const app = createApp(DesktopLyricsApp, rootProps);
  app.mount(target);
  return app;
}

export async function bootstrapDesktopLyricsApp(
  target: string | Element = "#app",
  options: MountDesktopLyricsAppOptions,
): Promise<App<Element>> {
  const placement = options.placement ?? NOOP_WINDOW_PLACEMENT;
  await placement.restore();
  const app = mountDesktopLyricsApp(target, options);
  let unmounted = false;
  let stopObserving: (() => void) | undefined;
  app.onUnmount(() => {
    unmounted = true;
    const stop = stopObserving;
    stopObserving = undefined;
    stop?.();
  });
  const stop = await placement.observe();
  if (unmounted) {
    stop();
  } else {
    stopObserving = stop;
  }
  return app;
}

export { default as DesktopLyricsApp } from "./DesktopLyricsApp.vue";
export * from "./bridge";
export * from "./protocol";
export * from "./timeline";
export * from "./windowPlacement";

const NOOP_WINDOW_PLACEMENT: DesktopLyricsWindowPlacement = {
  async restore() {},
  async observe() { return () => undefined; },
};
