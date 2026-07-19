import type {
  DesktopIntegration,
  DesktopMediaMetadata,
  DesktopPlaybackState,
} from "../ports/DesktopIntegration";

export type DesktopMediaOperation = "metadata" | "playback" | "clear";

export class DesktopMediaSessionCoordinator {
  private pendingMetadata: DesktopMediaMetadata | null = null;
  private pendingPlayback: DesktopPlaybackState | null = null;
  private clearPending = false;
  private processing: Promise<void> | null = null;

  constructor(
    private readonly desktop: DesktopIntegration,
    private readonly reportError: (operation: DesktopMediaOperation, cause: unknown) => void = () => undefined,
  ) {}

  updateMetadata(metadata: DesktopMediaMetadata): void {
    this.pendingMetadata = metadata;
    this.start();
  }

  updatePlayback(state: DesktopPlaybackState): void {
    this.pendingPlayback = state;
    this.start();
  }

  clear(): void {
    this.pendingMetadata = null;
    this.pendingPlayback = null;
    this.clearPending = true;
    this.start();
  }

  async whenIdle(): Promise<void> {
    while (this.processing) await this.processing;
  }

  private start(): void {
    if (this.processing) return;
    this.processing = this.flush().finally(() => {
      this.processing = null;
      if (this.hasPendingWork()) this.start();
    });
  }

  private async flush(): Promise<void> {
    while (this.hasPendingWork()) {
      if (this.clearPending) {
        this.clearPending = false;
        await this.run("clear", () => this.desktop.clearMediaSession());
        continue;
      }

      if (this.pendingMetadata) {
        const metadata = this.pendingMetadata;
        this.pendingMetadata = null;
        await this.run("metadata", () => this.desktop.updateMediaMetadata(metadata));
        continue;
      }

      const playback = this.pendingPlayback;
      this.pendingPlayback = null;
      if (playback) await this.run("playback", () => this.desktop.updateMediaPlayback(playback));
    }
  }

  private hasPendingWork(): boolean {
    return this.clearPending || this.pendingMetadata !== null || this.pendingPlayback !== null;
  }

  private async run(operation: DesktopMediaOperation, task: () => Promise<void>): Promise<void> {
    try {
      await task();
    } catch (cause) {
      this.reportError(operation, cause);
    }
  }
}
