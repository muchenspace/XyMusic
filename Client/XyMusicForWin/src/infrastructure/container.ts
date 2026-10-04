import type { ApplicationServices } from "../application/services";
import { CatalogUseCases } from "../application/use-cases/CatalogUseCases";
import { FavoriteUseCases } from "../application/use-cases/FavoriteUseCases";
import { LibraryUseCases } from "../application/use-cases/LibraryUseCases";
import { PlaybackUseCases } from "../application/use-cases/PlaybackUseCases";
import { PlaybackStateUseCases } from "../application/use-cases/PlaybackStateUseCases";
import { PlaylistUseCases } from "../application/use-cases/PlaylistUseCases";
import { QueueLoadingUseCases } from "../application/use-cases/QueueLoadingUseCases";
import { PlaybackGrantCache } from "../application/services/PlaybackGrantCache";
import { PlaybackDesktopIntegration } from "../application/services/PlaybackDesktopIntegration";
import { DesktopWindowController } from "../application/services/DesktopWindowController";
import { DesktopLyricsController } from "../application/services/DesktopLyricsController";
import { HomeFeedService } from "../application/services/HomeFeedService";
import { LibraryBrowserService } from "../application/services/LibraryBrowserService";
import { LyricsCacheService } from "../application/services/LyricsCacheService";
import { LyricsPreferencePersistence } from "../application/services/LyricsPreferencePersistence";
import { PlaybackStatePersistence } from "../application/services/PlaybackStatePersistence";
import { PlaybackPreferences } from "../application/services/PlaybackPreferences";
import { PlaybackSession } from "../application/services/PlaybackSession";
import { SessionUseCases } from "../application/use-cases/SessionUseCases";
import { HtmlAudioPlayer } from "./audio/HtmlAudioPlayer";
import { WindowsMediaBridge } from "./windows/WindowsMediaBridge";
import { TauriDesktopWindow } from "./windows/TauriDesktopWindow";
import { TauriDesktopLyrics } from "./windows/TauriDesktopLyrics";
import { ApiClient } from "./http/ApiClient";
import { HttpCatalogRepository } from "./repositories/HttpCatalogRepository";
import { HttpLibraryRepository } from "./repositories/HttpLibraryRepository";
import { HttpPlaybackRepository } from "./repositories/HttpPlaybackRepository";
import { HttpPlaylistRepository } from "./repositories/HttpPlaylistRepository";
import { HttpSessionRepository } from "./repositories/HttpSessionRepository";
import { LocalPlaybackStateRepository } from "./playback/LocalPlaybackStateRepository";
import { LocalPlayerPreferences } from "./playback/LocalPlayerPreferences";
import { LocalUserInterfacePreferences } from "./preferences/LocalUserInterfacePreferences";
import { TauriNotifier } from "./desktop/TauriNotifier";
import { TauriDiagnostics } from "./diagnostics/TauriDiagnostics";
import { BrowserTaskScheduler } from "./scheduling/BrowserTaskScheduler";
import { BrowserPageLifecycle } from "./scheduling/BrowserPageLifecycle";
import { BrowserSessionIdGenerator } from "./scheduling/BrowserSessionIdGenerator";

export function createApplicationServices(): ApplicationServices {
  const api = new ApiClient();
  const catalog = new HttpCatalogRepository(api);
  const library = new HttpLibraryRepository(api);
  const playlists = new HttpPlaylistRepository(api);
  const playback = new HttpPlaybackRepository(api);
  const playbackUseCases = new PlaybackUseCases(playback);
  const diagnostics = new TauriDiagnostics();
  const playbackState = new PlaybackStateUseCases(new LocalPlaybackStateRepository());
  const scheduler = new BrowserTaskScheduler();
  const audio = new HtmlAudioPlayer();
  const playerPreferences = new LocalPlayerPreferences();
  const playbackPersistence = new PlaybackStatePersistence(playbackState, diagnostics, scheduler);
  const desktopPlayback = new PlaybackDesktopIntegration(new WindowsMediaBridge(), diagnostics);
  const desktopWindow = new TauriDesktopWindow();
  const desktopWindowController = new DesktopWindowController(desktopWindow, diagnostics, scheduler);
  const desktopLyrics = new TauriDesktopLyrics();
  const uiPreferences = new LocalUserInterfacePreferences();
  const pageLifecycle = new BrowserPageLifecycle();
  const catalogUseCases = new CatalogUseCases(catalog, playlists);
  const libraryUseCases = new LibraryUseCases(library);
  const playlistUseCases = new PlaylistUseCases(playlists);
  const playbackSession = new PlaybackSession(
    audio,
    playbackUseCases,
    new PlaybackGrantCache(playbackUseCases),
    playbackPersistence,
    new PlaybackPreferences(audio, playerPreferences, scheduler),
    desktopPlayback,
    desktopWindow,
    diagnostics,
    new TauriNotifier(),
    scheduler,
    pageLifecycle,
    new BrowserSessionIdGenerator(),
  );
  return {
    catalog: catalogUseCases,
    library: libraryUseCases,
    playlists: playlistUseCases,
    playbackSession,
    session: new SessionUseCases(new HttpSessionRepository(api)),
    desktopLyricsController: new DesktopLyricsController(desktopLyrics, uiPreferences, scheduler, pageLifecycle),
    desktopWindowController,
    diagnostics,
    uiPreferences,
    lyricsPreferencePersistence: new LyricsPreferencePersistence(uiPreferences, scheduler, pageLifecycle),
    homeFeed: new HomeFeedService(catalogUseCases, scheduler),
    libraryBrowser: new LibraryBrowserService(playlistUseCases),
    lyricsCache: new LyricsCacheService(catalog),
    queueLoading: new QueueLoadingUseCases(playbackSession, catalogUseCases, playlistUseCases),
    favorites: new FavoriteUseCases(libraryUseCases),
  };
}

/**
 * Composes the isolated desktop-lyrics window entry point so main.ts only
 * selects a window and delegates to a single factory.
 */
export async function createDesktopLyricsWindowServices(): Promise<{
  bootstrapDesktopLyricsApp: typeof import("../desktop-lyrics").bootstrapDesktopLyricsApp;
  options: import("../desktop-lyrics").MountDesktopLyricsAppOptions;
}> {
  const [
    { bootstrapDesktopLyricsApp },
    { TauriDesktopLyricsEventBridge },
    { TauriDesktopLyricsWindowPlacement },
  ] = await Promise.all([
    import("../desktop-lyrics"),
    import("./desktop/TauriDesktopLyricsEventBridge"),
    import("./windows/TauriDesktopLyricsWindowPlacement"),
  ]);
  return {
    bootstrapDesktopLyricsApp,
    options: {
      bridge: new TauriDesktopLyricsEventBridge(),
      placement: new TauriDesktopLyricsWindowPlacement(),
    },
  };
}
