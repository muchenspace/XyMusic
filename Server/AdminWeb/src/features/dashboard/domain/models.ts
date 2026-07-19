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
  recentActivity: Array<{
    id: string;
    action: string;
    targetType: string;
    targetId?: string | null;
    result: "SUCCESS" | "FAILURE";
    traceId: string;
    details?: Record<string, unknown> | null;
    actor: { id: string; username: string; displayName: string } | null;
    createdAt: string;
  }>;
}
