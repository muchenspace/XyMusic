import type { Track } from "./music";

export interface QueueSelection {
  tracks: Track[];
  currentIndex: number;
}

export function selectTrack(track: Track, source: readonly Track[]): QueueSelection {
  const tracks = source.length ? [...source] : [track];
  let currentIndex = tracks.findIndex((item) => item.id === track.id);
  if (currentIndex < 0) {
    tracks.unshift(track);
    currentIndex = 0;
  }
  return { tracks, currentIndex };
}

export function nextTrackIndex(length: number, currentIndex: number, shuffled: boolean, random: () => number = Math.random): number {
  if (length <= 0) return -1;
  if (!shuffled || length === 1) return (currentIndex + 1) % length;
  const candidate = Math.floor(random() * (length - 1));
  return candidate >= currentIndex ? candidate + 1 : candidate;
}

export function previousTrackIndex(length: number, currentIndex: number): number {
  return length > 0 ? (currentIndex - 1 + length) % length : -1;
}

export function removeTrackFromQueue(queue: readonly Track[], currentIndex: number, trackId: string): QueueSelection {
  const removeIndex = queue.findIndex((item) => item.id === trackId);
  return removeTrackAtIndex(queue, currentIndex, removeIndex);
}

export function removeTrackAtIndex(queue: readonly Track[], currentIndex: number, removeIndex: number): QueueSelection {
  if (removeIndex < 0 || removeIndex === currentIndex) return { tracks: [...queue], currentIndex };
  const tracks = queue.filter((_, index) => index !== removeIndex);
  return { tracks, currentIndex: removeIndex < currentIndex ? currentIndex - 1 : currentIndex };
}

export function keepCurrentTrack(queue: readonly Track[], currentIndex: number): QueueSelection {
  const current = currentIndex >= 0 ? queue[currentIndex] : undefined;
  return current ? { tracks: [current], currentIndex: 0 } : { tracks: [], currentIndex: -1 };
}
