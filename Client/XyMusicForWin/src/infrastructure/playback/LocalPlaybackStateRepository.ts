import type { PlaybackStateRepository } from "../../application/ports/PlaybackStateRepository";
import type { PersistedPlaybackState, PlaybackProgressCheckpoint } from "../../domain/playbackState";

export class LocalPlaybackStateRepository implements PlaybackStateRepository {
  read(ownerKey: string): PersistedPlaybackState | null {
    const snapshot = readSnapshot(SNAPSHOT_STORAGE_KEY, ownerKey) ?? this.migrateLegacy(ownerKey);
    if (!snapshot) return null;
    const checkpoint = readCheckpoint(ownerKey);
    if (!checkpoint || checkpoint.snapshotSavedAt !== snapshot.savedAt) return snapshot;
    if (checkpoint.currentIndex !== snapshot.currentIndex || snapshot.queue[checkpoint.currentIndex]?.id !== checkpoint.trackId) return snapshot;
    return { ...snapshot, position: checkpoint.position };
  }

  write(state: PersistedPlaybackState): void {
    localStorage.setItem(SNAPSHOT_STORAGE_KEY, JSON.stringify(encodeSnapshot(state)));
    localStorage.removeItem(CHECKPOINT_STORAGE_KEY);
    removeOwnedValue(LEGACY_STORAGE_KEY, state.ownerKey);
  }

  writeCheckpoint(checkpoint: PlaybackProgressCheckpoint): void {
    localStorage.setItem(CHECKPOINT_STORAGE_KEY, JSON.stringify(checkpoint));
  }

  clear(ownerKey: string): void {
    removeOwnedValue(SNAPSHOT_STORAGE_KEY, ownerKey);
    removeOwnedValue(CHECKPOINT_STORAGE_KEY, ownerKey);
    removeOwnedValue(LEGACY_STORAGE_KEY, ownerKey);
  }

  private migrateLegacy(ownerKey: string): PersistedPlaybackState | null {
    const legacy = readSnapshot(LEGACY_STORAGE_KEY, ownerKey);
    if (!legacy) return null;
    try { this.write(legacy); }
    catch { /* Keep the readable legacy value when migration cannot be persisted. */ }
    return legacy;
  }
}

function readSnapshot(key: string, ownerKey: string): PersistedPlaybackState | null {
  try {
    const value = JSON.parse(localStorage.getItem(key) ?? "null") as Partial<PersistedPlaybackState> | CompactPlaybackState | null;
    if (isCompactSnapshot(value)) return decodeSnapshot(value, ownerKey);
    if (!value || value.ownerKey !== ownerKey || !Array.isArray(value.queue)) return null;
    return {
      ownerKey,
      queue: value.queue.filter(isTrack),
      currentIndex: Number.isInteger(value.currentIndex) ? Number(value.currentIndex) : -1,
      position: finiteNonNegative(value.position),
      shuffled: Boolean(value.shuffled),
      repeat: value.repeatMode === "one" || Boolean(value.repeat),
      repeatMode: isRepeatMode(value.repeatMode) ? value.repeatMode : Boolean(value.repeat) ? "one" : "off",
      quality: isQuality(value.quality) ? value.quality : "AUTO",
      crossfadeSeconds: Math.min(5, finiteNonNegative(value.crossfadeSeconds)),
      savedAt: typeof value.savedAt === "string" ? value.savedAt : new Date(0).toISOString(),
    };
  } catch {
    return null;
  }
}

function encodeSnapshot(state: PersistedPlaybackState): CompactPlaybackState {
  return {
    v: COMPACT_SNAPSHOT_VERSION,
    o: state.ownerKey,
    q: state.queue.map((track) => [
      track.id,
      track.title,
      track.artist,
      track.artistIds,
      track.album,
      track.albumId ?? null,
      track.coverUrl,
      track.duration,
      track.liked,
      track.publishedAt,
    ]),
    i: state.currentIndex,
    p: state.position,
    s: state.shuffled,
    r: state.repeatMode,
    y: state.quality,
    c: state.crossfadeSeconds,
    a: state.savedAt,
  };
}

function decodeSnapshot(value: CompactPlaybackState, ownerKey: string): PersistedPlaybackState | null {
  if (value.o !== ownerKey || !Array.isArray(value.q)) return null;
  const queue = value.q.filter(isCompactTrack).map((track) => ({
    id: track[0],
    title: track[1],
    artist: track[2],
    artistIds: track[3],
    album: track[4],
    ...(track[5] ? { albumId: track[5] } : {}),
    coverUrl: track[6],
    duration: finiteNonNegative(track[7]),
    liked: track[8],
    publishedAt: track[9],
  }));
  const repeatMode = isRepeatMode(value.r) ? value.r : "off";
  return {
    ownerKey,
    queue,
    currentIndex: Number.isInteger(value.i) ? Number(value.i) : -1,
    position: finiteNonNegative(value.p),
    shuffled: Boolean(value.s),
    repeat: repeatMode === "one",
    repeatMode,
    quality: isQuality(value.y) ? value.y : "AUTO",
    crossfadeSeconds: Math.min(5, finiteNonNegative(value.c)),
    savedAt: typeof value.a === "string" ? value.a : new Date(0).toISOString(),
  };
}

function isCompactSnapshot(value: unknown): value is CompactPlaybackState {
  return Boolean(value && typeof value === "object" && (value as { v?: unknown }).v === COMPACT_SNAPSHOT_VERSION);
}

function isCompactTrack(value: unknown): value is CompactTrack {
  return Array.isArray(value)
    && value.length >= 10
    && typeof value[0] === "string"
    && typeof value[1] === "string"
    && typeof value[2] === "string"
    && Array.isArray(value[3])
    && value[3].every((id) => typeof id === "string")
    && typeof value[4] === "string"
    && (value[5] === null || typeof value[5] === "string")
    && typeof value[6] === "string"
    && typeof value[7] === "number"
    && typeof value[8] === "boolean"
    && typeof value[9] === "string";
}

function readCheckpoint(ownerKey: string): PlaybackProgressCheckpoint | null {
  try {
    const value = JSON.parse(localStorage.getItem(CHECKPOINT_STORAGE_KEY) ?? "null") as Partial<PlaybackProgressCheckpoint> | null;
    if (!value || value.ownerKey !== ownerKey || !Number.isInteger(value.currentIndex)) return null;
    if (typeof value.trackId !== "string" || typeof value.savedAt !== "string" || typeof value.snapshotSavedAt !== "string") return null;
    return {
      ownerKey,
      currentIndex: Number(value.currentIndex),
      trackId: value.trackId,
      position: finiteNonNegative(value.position),
      savedAt: value.savedAt,
      snapshotSavedAt: value.snapshotSavedAt,
    };
  } catch {
    return null;
  }
}

function removeOwnedValue(key: string, ownerKey: string): void {
  try {
    const value = JSON.parse(localStorage.getItem(key) ?? "null") as { ownerKey?: unknown } | null;
    if (value?.ownerKey === ownerKey) localStorage.removeItem(key);
  } catch {
    // Invalid values cannot be attributed safely to the requested owner.
  }
}

function isTrack(value: unknown): value is PersistedPlaybackState["queue"][number] {
  if (!value || typeof value !== "object") return false;
  const track = value as { id?: unknown; title?: unknown };
  return typeof track.id === "string" && typeof track.title === "string";
}

function isQuality(value: unknown): value is PersistedPlaybackState["quality"] {
  return value === "AUTO" || value === "DATA_SAVER" || value === "STANDARD" || value === "HIGH" || value === "LOSSLESS";
}

function isRepeatMode(value: unknown): value is PersistedPlaybackState["repeatMode"] {
  return value === "off" || value === "all" || value === "one";
}

function finiteNonNegative(value: unknown): number {
  const number = Number(value);
  return Number.isFinite(number) ? Math.max(0, number) : 0;
}

const SNAPSHOT_STORAGE_KEY = "xymusic.desktop.playback-state.snapshot.v2";
const CHECKPOINT_STORAGE_KEY = "xymusic.desktop.playback-state.checkpoint.v2";
const LEGACY_STORAGE_KEY = "xymusic.desktop.playback-state.v1";
const COMPACT_SNAPSHOT_VERSION = 3;

type CompactTrack = [string, string, string, string[], string, string | null, string, number, boolean, string];

interface CompactPlaybackState {
  v: typeof COMPACT_SNAPSHOT_VERSION;
  o: string;
  q: CompactTrack[];
  i: number;
  p: number;
  s: boolean;
  r: PersistedPlaybackState["repeatMode"];
  y: PersistedPlaybackState["quality"];
  c: number;
  a: string;
}
