import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { DesktopIntegration, DesktopMediaAction, DesktopMediaMetadata, DesktopPlaybackState } from "../../application/ports/DesktopIntegration";

interface MediaActionPayload { action?: unknown; position?: unknown }

export class WindowsMediaBridge implements DesktopIntegration {
  async onMediaAction(listener: (action: DesktopMediaAction, position?: number) => void): Promise<() => void> {
    if (!isTauriRuntime()) return () => undefined;
    return listen<MediaActionPayload>("desktop://media-action", ({ payload }) => {
      if (!isMediaAction(payload.action)) return;
      if (payload.action === "seek") {
        if (typeof payload.position === "number" && Number.isFinite(payload.position) && payload.position >= 0) {
          listener(payload.action, payload.position);
        }
        return;
      }
      listener(payload.action);
    });
  }

  updateMediaMetadata(metadata: DesktopMediaMetadata): Promise<void> {
    return this.invokeSafely("update_media_metadata", { metadata });
  }

  updateMediaPlayback(state: DesktopPlaybackState): Promise<void> {
    return this.invokeSafely("update_media_playback", { state });
  }

  clearMediaSession(): Promise<void> {
    return this.invokeSafely("clear_media_session");
  }

  private async invokeSafely(command: string, args?: Record<string, unknown>): Promise<void> {
    if (!isTauriRuntime()) return;
    await invoke(command, args);
  }
}

function isTauriRuntime(): boolean {
  return "__TAURI_INTERNALS__" in window;
}

function isMediaAction(value: unknown): value is DesktopMediaAction {
  return value === "play" || value === "pause" || value === "toggle" || value === "previous"
    || value === "next" || value === "stop" || value === "seek";
}
