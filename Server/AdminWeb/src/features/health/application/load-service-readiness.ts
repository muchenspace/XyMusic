import type { ReadinessGateway } from "@/features/health/application/readiness-gateway";
import type { ServiceReadiness } from "@/features/health/domain/models";

export class LoadServiceReadiness {
  constructor(private readonly gateway: ReadinessGateway) {}

  execute(signal?: AbortSignal): Promise<ServiceReadiness> {
    return this.gateway.readiness(signal);
  }
}
