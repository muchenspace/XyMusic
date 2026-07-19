import type { Album, Artist, Playlist, Track } from "../../domain/music";
import type { AlbumDto, ArtistDto, PlaylistDto, TrackDto } from "./musicDtos";

const ACCENTS = ["#c2473a", "#4c7186", "#ad7142", "#806354", "#73546d", "#477186"];

export function mapTrack(value: TrackDto): Track {
  const artists = Array.isArray(value.artists) ? value.artists : [];
  return {
    id: value.id,
    title: value.title,
    artist: artistNames(artists),
    artistIds: artists.map((item) => item.id).filter(Boolean),
    album: value.album?.title || "未知专辑",
    ...(value.album?.id ? { albumId: value.album.id } : {}),
    coverUrl: value.artwork?.url ?? "",
    duration: Number.isFinite(value.durationMs) ? Math.max(0, Math.round(value.durationMs / 1000)) : 0,
    liked: Boolean(value.isFavorite),
    publishedAt: value.publishedAt ?? "",
  };
}

export function mapAlbum(value: AlbumDto, _index?: number): Album {
  const artists = Array.isArray(value.artists) ? value.artists : [];
  const year = releaseYear(value.releaseDate);
  return {
    id: value.id,
    title: value.title,
    artist: artistNames(artists),
    artistIds: artists.map((item) => item.id).filter(Boolean),
    coverUrl: value.cover?.url ?? "",
    ...(year ? { year } : {}),
    trackCount: Math.max(0, Number(value.trackCount) || 0),
    ...(value.description ? { description: value.description } : {}),
    accent: stableAccent(value.id),
  };
}

export function mapArtist(value: ArtistDto): Artist {
  return {
    id: value.id,
    name: value.name,
    ...(value.artwork ? { artwork: value.artwork } : {}),
    ...(value.description ? { description: value.description } : {}),
  };
}

export function mapPlaylist(value: PlaylistDto, _index?: number): Playlist {
  return {
    id: value.id,
    title: value.name,
    description: value.description ?? "",
    coverUrl: value.cover?.url ?? "",
    trackCount: Math.max(0, Number(value.trackCount) || 0),
    accent: stableAccent(value.id, 2),
    version: value.version,
    visibility: value.visibility,
  };
}

function artistNames(artists: Array<{ name: string }>): string {
  return artists.map((item) => item.name).filter(Boolean).join("、") || "未知艺术家";
}

function releaseYear(value: string | null | undefined): number | undefined {
  const match = value ? /^(\d{4})/.exec(value) : null;
  const year = match ? Number(match[1]) : 0;
  return year >= 1 && year <= 9999 ? year : undefined;
}

function stableAccent(id: string, offset = 0): string {
  let hash = 0;
  for (let index = 0; index < id.length; index += 1) hash = (hash * 31 + id.charCodeAt(index)) >>> 0;
  return ACCENTS[(hash + offset) % ACCENTS.length]!;
}
