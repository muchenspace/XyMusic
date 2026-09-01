export type PlaybackQuality = "AUTO" | "DATA_SAVER" | "STANDARD" | "HIGH" | "LOSSLESS";
export type ConcretePlaybackQuality = Exclude<PlaybackQuality, "AUTO">;
export type StreamProtocol = "PROGRESSIVE" | "HLS";

export interface Artwork { url: string; cacheKey: string }
export interface UserProfile { id: string; username: string; displayName: string; avatarUrl?: string }
export interface Artist { id: string; name: string; artwork?: Artwork; description?: string }

export interface Track {
  id: string;
  title: string;
  artist: string;
  artistIds: string[];
  album: string;
  albumId?: string;
  coverUrl: string;
  duration: number;
  liked: boolean;
  publishedAt: string;
}

export type ReadonlyTrack = Readonly<Omit<Track, "artistIds">> & {
  readonly artistIds: readonly string[];
};

export interface Album {
  id: string;
  title: string;
  artist: string;
  artistIds: string[];
  coverUrl: string;
  year?: number;
  trackCount: number;
  description?: string;
  accent: string;
}

export type PlaylistVisibility = "PRIVATE" | "UNLISTED" | "PUBLIC";
export interface Playlist {
  id: string;
  title: string;
  description: string;
  coverUrl: string;
  trackCount: number;
  accent: string;
  version: number;
  visibility: PlaylistVisibility;
}

export interface PlaylistEntry { id: string; position: number; track: Track }
export interface PlaylistDetail extends Playlist { entries: PlaylistEntry[]; nextCursor?: string | null }
export interface HomeFeed { featured?: Album; playlists: Playlist[]; tracks: Track[] }
export type SearchScope = "tracks" | "artists" | "albums";
export interface SearchCursors { tracks: string | null; artists: string | null; albums: string | null }
export interface SearchResults { tracks: Track[]; artists: Artist[]; albums: Album[]; nextCursors?: SearchCursors }
export type LyricTiming = "LINE" | "WORD";
export interface LyricWord { time: number; endTime?: number; text: string }
export interface LyricLine { time: number | null; text: string; words?: LyricWord[]; translation?: string }
export interface Lyrics {
  trackId: string;
  lines: LyricLine[];
  source: string;
  synchronized: boolean;
  timing: LyricTiming;
  translationSource?: string;
}
export interface PlaybackGrant {
  streamUrl: string;
  expiresAt: string;
  selectedQuality: ConcretePlaybackQuality;
  streamProtocol?: StreamProtocol;
  durationMs?: number;
  startPositionMs?: number;
  bitrate?: number;
  contentLength?: number;
}

export interface AppPreferences {
  serverUrl: string;
  quality: PlaybackQuality;
  theme: "system" | "dark" | "light";
}
