import type { AudioStatus } from "@/shared/domain/audio-status";

export const ACTIVE_AUDIO_REFRESH_MS = 5_000;
export const STABLE_DASHBOARD_REFRESH_MS = 60_000;

interface DashboardAudioData {
  catalog: { tracks: Partial<Record<AudioStatus, number>> };
}

interface TrackListAudioData {
  items: readonly { audioStatus: AudioStatus }[];
}

export function dashboardAudioRefetchInterval(data: DashboardAudioData | undefined): number {
  if (!data || (data.catalog.tracks.PROCESSING ?? 0) > 0) return ACTIVE_AUDIO_REFRESH_MS;
  return STABLE_DASHBOARD_REFRESH_MS;
}

export function trackListAudioRefetchInterval(data: TrackListAudioData | undefined): number | false {
  return data?.items.some((track) => track.audioStatus === "PROCESSING") ? ACTIVE_AUDIO_REFRESH_MS : false;
}
