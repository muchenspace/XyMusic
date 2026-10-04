import { LoadServiceReadiness } from "@/features/health/application/load-service-readiness";
import { HttpReadinessGateway } from "@/features/health/infrastructure/http-readiness-gateway";

const health = new LoadServiceReadiness(new HttpReadinessGateway());

export function useHealth(): LoadServiceReadiness {
  return health;
}
