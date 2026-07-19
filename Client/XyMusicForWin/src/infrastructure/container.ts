import type { ApplicationServices } from "../application/services";
import { CatalogUseCases } from "../application/use-cases/CatalogUseCases";
import { LibraryUseCases } from "../application/use-cases/LibraryUseCases";
import { PlaybackUseCases } from "../application/use-cases/PlaybackUseCases";
import { PlaybackStateUseCases } from "../application/use-cases/PlaybackStateUseCases";
import { PlaybackGrantCache } from "../application/services/PlaybackGrantCache";
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

export function createApplicationServices(): ApplicationServices {
  const api = new ApiClient();
  const catalog = new HttpCatalogRepository(api);
  const library = new HttpLibraryRepository(api);
  const playlists = new HttpPlaylistRepository(api);
  const playback = new HttpPlaybackRepository(api);
  const playbackUseCases = new PlaybackUseCases(playback);
  return {
    catalog: new CatalogUseCases(catalog, playlists),
    library: new LibraryUseCases(library),
    playlists: new PlaylistUseCases(playlists),
    playback: playbackUseCases,
    playbackState: new PlaybackStateUseCases(new LocalPlaybackStateRepository()),
    playbackGrants: new PlaybackGrantCache(playbackUseCases),
    session: new SessionUseCases(new HttpSessionRepository(api)),
    audio: new HtmlAudioPlayer(),
    desktop: new WindowsMediaBridge(),
    desktopLyrics: new TauriDesktopLyrics(),
    desktopWindow: new TauriDesktopWindow(),
    diagnostics: new TauriDiagnostics(),
    notifier: new TauriNotifier(),
    playerPreferences: new LocalPlayerPreferences(),
    uiPreferences: new LocalUserInterfacePreferences(),
  };
}
