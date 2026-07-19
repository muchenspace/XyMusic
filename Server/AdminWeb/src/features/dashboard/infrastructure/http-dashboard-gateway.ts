import { adminApi } from "@/api/admin";
import type { DashboardGateway } from "@/features/dashboard/application/dashboard-gateway";
import type { DashboardData } from "@/features/dashboard/domain/models";

export class HttpDashboardGateway implements DashboardGateway {
  load(signal?: AbortSignal): Promise<DashboardData> {
    return adminApi.dashboard(signal);
  }
}
