import type { ServiceReadiness } from "@/features/health/domain/models";

export interface ReadinessGateway {
  readiness(signal?: AbortSignal): Promise<ServiceReadiness>;
}
