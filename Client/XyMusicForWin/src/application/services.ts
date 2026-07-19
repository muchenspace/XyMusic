import type { AudioPlayer } from "./ports/AudioPlayer";
import type { DesktopIntegration } from "./ports/DesktopIntegration";
import type { DesktopLyrics } from "./ports/DesktopLyrics";
import type { DesktopWindow } from "./ports/DesktopWindow";
import type { Diagnostics } from "./ports/Diagnostics";
import type { Notifier } from "./ports/Notifier";
import type { PlayerPreferences } from "./ports/PlayerPreferences";
import type { UserInterfacePreferences } from "./ports/UserInterfacePreferences";
import type { CatalogUseCases } from "./use-cases/CatalogUseCases";
import type { LibraryUseCases } from "./use-cases/LibraryUseCases";
import type { PlaybackUseCases } from "./use-cases/PlaybackUseCases";
import type { PlaybackStateUseCases } from "./use-cases/PlaybackStateUseCases";
import type { PlaylistUseCases } from "./use-cases/PlaylistUseCases";
import type { SessionUseCases } from "./use-cases/SessionUseCases";
import type { PlaybackGrantCache } from "./services/PlaybackGrantCache";

export interface ApplicationServices {
  catalog: CatalogUseCases;
  library: LibraryUseCases;
  playlists: PlaylistUseCases;
  playback: PlaybackUseCases;
  playbackState: PlaybackStateUseCases;
  playbackGrants: PlaybackGrantCache;
  session: SessionUseCases;
  audio: AudioPlayer;
  desktop: DesktopIntegration;
  desktopLyrics: DesktopLyrics;
  desktopWindow: DesktopWindow;
  diagnostics: Diagnostics;
  notifier: Notifier;
  playerPreferences: PlayerPreferences;
  uiPreferences: UserInterfacePreferences;
}
