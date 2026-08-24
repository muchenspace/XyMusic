import type { ApplicationServices } from "../application/services";
import { CatalogUseCases } from "../application/use-cases/CatalogUseCases";
import { LibraryUseCases } from "../application/use-cases/LibraryUseCases";
import { PlaybackUseCases } from "../application/use-cases/PlaybackUseCases";
import { PlaybackStateUseCases } from "../application/use-cases/PlaybackStateUseCases";
import { PlaybackGrantCache } from "../application/services/PlaybackGrantCache";
import { PlaybackDesktopIntegration } from "../application/services/PlaybackDesktopIntegration";
import { DesktopWindowController } from "../application/services/DesktopWindowController";
import { DesktopLyricsController } from "../application/services/DesktopLyricsController";
import { PlaybackStatePersistence } from "../application/services/PlaybackStatePersistence";
import { PlaybackPreferences } from "../application/services/PlaybackPreferences";
import { PlaybackSession } from "../application/services/PlaybackSession";
import { PlaylistUseCases } from "../application/use-cases/PlaylistUseCases";
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
  return {
    catalog: new CatalogUseCases(catalog, playlists),
    library: new LibraryUseCases(library),
    playlists: new PlaylistUseCases(playlists),
    playbackSession: new PlaybackSession(
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
    ),
    session: new SessionUseCases(new HttpSessionRepository(api)),
    desktopLyricsController: new DesktopLyricsController(desktopLyrics, uiPreferences, scheduler, pageLifecycle),
    desktopWindowController,
    diagnostics,
    uiPreferences,
  };
}
