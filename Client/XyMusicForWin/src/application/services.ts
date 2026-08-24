import type { DesktopLyricsController } from "./ports/DesktopLyricsController";
import type { DesktopWindowController } from "./ports/DesktopWindowController";
import type { Diagnostics } from "./ports/Diagnostics";
import type { PlaybackSession } from "./ports/PlaybackSession";
import type { UserInterfacePreferences } from "./ports/UserInterfacePreferences";
import type { CatalogUseCases } from "./use-cases/CatalogUseCases";
import type { LibraryUseCases } from "./use-cases/LibraryUseCases";
import type { PlaylistUseCases } from "./use-cases/PlaylistUseCases";
import type { SessionUseCases } from "./use-cases/SessionUseCases";

export interface ApplicationServices {
  catalog: CatalogUseCases;
  library: LibraryUseCases;
  playlists: PlaylistUseCases;
  playbackSession: PlaybackSession;
  session: SessionUseCases;
  desktopLyricsController: DesktopLyricsController;
  desktopWindowController: DesktopWindowController;
  diagnostics: Diagnostics;
  uiPreferences: UserInterfacePreferences;
}
