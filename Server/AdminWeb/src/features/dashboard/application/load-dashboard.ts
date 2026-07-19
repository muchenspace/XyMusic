import type { DashboardGateway } from "@/features/dashboard/application/dashboard-gateway";
import type { DashboardData } from "@/features/dashboard/domain/models";

export class LoadDashboard {
  constructor(private readonly gateway: DashboardGateway) {}

  execute(signal?: AbortSignal): Promise<DashboardData> {
    return this.gateway.load(signal);
  }
}
