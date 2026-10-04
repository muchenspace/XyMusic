import type { DesktopLyricsController } from "./ports/DesktopLyricsController";
import type { DesktopWindowController } from "./ports/DesktopWindowController";
import type { Diagnostics } from "./ports/Diagnostics";
import type { PlaybackSession } from "./ports/PlaybackSession";
import type { UserInterfacePreferences } from "./ports/UserInterfacePreferences";
import type { HomeFeedService } from "./services/HomeFeedService";
import type { LibraryBrowserService } from "./services/LibraryBrowserService";
import type { LyricsCacheService } from "./services/LyricsCacheService";
import type { LyricsPreferencePersistence } from "./services/LyricsPreferencePersistence";
import type { CatalogUseCases } from "./use-cases/CatalogUseCases";
import type { FavoriteUseCases } from "./use-cases/FavoriteUseCases";
import type { LibraryUseCases } from "./use-cases/LibraryUseCases";
import type { PlaylistUseCases } from "./use-cases/PlaylistUseCases";
import type { QueueLoadingUseCases } from "./use-cases/QueueLoadingUseCases";
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
  lyricsPreferencePersistence: LyricsPreferencePersistence;
  homeFeed: HomeFeedService;
  libraryBrowser: LibraryBrowserService;
  lyricsCache: LyricsCacheService;
  queueLoading: QueueLoadingUseCases;
  favorites: FavoriteUseCases;
}
