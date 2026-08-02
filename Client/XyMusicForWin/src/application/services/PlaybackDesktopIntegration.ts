import type {
  DesktopMediaAction,
  DesktopIntegration,
} from "../ports/DesktopIntegration";
import type { Diagnostics } from "../ports/Diagnostics";
import type { Track } from "../../domain/music";
import { DesktopMediaSessionCoordinator, type DesktopMediaOperation } from "./DesktopMediaSessionCoordinator";

export interface PlaybackDesktopCommands {
  play(): void | Promise<void>;
  pause(): void | Promise<void>;
  toggle(): void | Promise<void>;
  previous(): void | Promise<void>;
  next(): void | Promise<void>;
  stop(): void | Promise<void>;
  seekTo(seconds: number): void | Promise<void>;
}

export type PlaybackDesktopStatus = "playing" | "paused" | "stopped";

/**
 * Owns native media-session lifecycle and coalesced IPC outside presentation.
 */
export class PlaybackDesktopIntegration {
  private readonly mediaSession: DesktopMediaSessionCoordinator;
  private commands: PlaybackDesktopCommands | undefined;
  private removeMediaActions: (() => void) | undefined;
  private listening = false;
  private disposed = false;

  constructor(
    private readonly desktop: DesktopIntegration,
    private readonly diagnostics: Diagnostics,
  ) {
    this.mediaSession = new DesktopMediaSessionCoordinator(desktop, (operation, cause) => {
      this.reportMediaSessionError(operation, cause);
    });
  }

  connect(commands: PlaybackDesktopCommands): void {
    if (this.disposed) return;
    this.commands = commands;
    if (this.listening) return;
    this.listening = true;
    void this.desktop.onMediaAction((action, position) => {
      this.handleAction(action, position);
    }).then((remove) => {
      if (this.disposed) remove();
      else this.removeMediaActions = remove;
    }).catch((cause) => {
      this.listening = false;
      this.diagnostics.warn("media-session", `Could not subscribe to native media actions: ${describeError(cause)}`);
    });
  }

  setTrack(track: Readonly<Pick<Track, "title" | "artist" | "album" | "coverUrl">>): void {
    if (this.disposed) return;
    this.mediaSession.updateMetadata({
      title: track.title,
      artist: track.artist,
      album: track.album,
      ...(track.coverUrl ? { artworkUrl: track.coverUrl } : {}),
    });
  }

  setPlayback(status: PlaybackDesktopStatus, position: number, duration: number): void {
    if (!this.disposed) this.mediaSession.updatePlayback({ status, position, duration });
  }

  clear(): void {
    if (!this.disposed) this.mediaSession.clear();
  }

  whenIdle(): Promise<void> {
    return this.mediaSession.whenIdle();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.commands = undefined;
    this.removeMediaActions?.();
    this.removeMediaActions = undefined;
    this.mediaSession.clear();
  }

  private reportMediaSessionError(operation: DesktopMediaOperation, cause: unknown): void {
    const action = operation === "metadata"
      ? "update native media metadata"
      : operation === "clear"
        ? "clear the native media session"
        : "update native playback state";
    this.diagnostics.warn("media-session", `Could not ${action}: ${describeError(cause)}`);
  }

  private handleAction(action: DesktopMediaAction, position?: number): void {
    const commands = this.commands;
    if (!commands || this.disposed) return;
    const command = action === "play"
      ? () => commands.play()
      : action === "pause"
        ? () => commands.pause()
        : action === "toggle"
          ? () => commands.toggle()
          : action === "previous"
            ? () => commands.previous()
            : action === "next"
              ? () => commands.next()
              : action === "stop"
                ? () => commands.stop()
                : position !== undefined && Number.isFinite(position)
                  ? () => commands.seekTo(position)
                  : undefined;
    if (!command) return;
    try {
      void Promise.resolve(command()).catch((cause) => {
        this.diagnostics.warn("media-session", `Could not handle native media action: ${describeError(cause)}`);
      });
    } catch (cause) {
      this.diagnostics.warn("media-session", `Could not handle native media action: ${describeError(cause)}`);
    }
  }
}

function describeError(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}
