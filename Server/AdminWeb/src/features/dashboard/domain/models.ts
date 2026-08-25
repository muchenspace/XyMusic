import type { AudioStatus } from "@/shared/domain/audio-status";

export interface DashboardData {
  users: { total: number; active: number; administrators: number };
  catalog: {
    artists: number;
    albums: number;
    tracks: Partial<Record<AudioStatus, number>>;
  };
  sources: Record<string, number>;
  jobs: Record<string, number>;
}
