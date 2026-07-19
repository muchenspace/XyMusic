import { createApp, type App } from "vue";
import DesktopLyricsApp from "./DesktopLyricsApp.vue";
import type { DesktopLyricsBridge } from "./bridge";
import type { DesktopLyricsStatePayload } from "./protocol";
import { observeDesktopLyricsPlacement, restoreDesktopLyricsPlacement } from "./windowPlacement";

export interface MountDesktopLyricsAppOptions {
  bridge?: DesktopLyricsBridge;
  initialState?: DesktopLyricsStatePayload | null;
}

export function mountDesktopLyricsApp(
  target: string | Element = "#app",
  options: MountDesktopLyricsAppOptions = {},
): App<Element> {
  const rootProps: Record<string, unknown> = { ...options };
  const app = createApp(DesktopLyricsApp, rootProps);
  app.mount(target);
  return app;
}

export async function bootstrapDesktopLyricsApp(
  target: string | Element = "#app",
  options: MountDesktopLyricsAppOptions = {},
): Promise<App<Element>> {
  await restoreDesktopLyricsPlacement();
  const app = mountDesktopLyricsApp(target, options);
  const stopObserving = await observeDesktopLyricsPlacement();
  app.onUnmount(stopObserving);
  return app;
}

export { default as DesktopLyricsApp } from "./DesktopLyricsApp.vue";
export * from "./bridge";
export * from "./protocol";
export * from "./timeline";
export * from "./windowPlacement";
