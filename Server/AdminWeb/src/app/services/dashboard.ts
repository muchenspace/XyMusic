import { LoadDashboard } from "@/features/dashboard/application/load-dashboard";
import { HttpDashboardGateway } from "@/features/dashboard/infrastructure/http-dashboard-gateway";

const dashboard = new LoadDashboard(new HttpDashboardGateway());

export function useDashboard(): LoadDashboard {
  return dashboard;
}
