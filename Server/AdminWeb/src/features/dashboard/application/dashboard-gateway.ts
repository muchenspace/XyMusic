import type { DashboardData } from "@/features/dashboard/domain/models";

export interface DashboardGateway {
  load(signal?: AbortSignal): Promise<DashboardData>;
}
