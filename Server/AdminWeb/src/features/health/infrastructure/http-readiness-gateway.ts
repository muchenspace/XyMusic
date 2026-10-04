import { serviceReadiness } from "@/api/client";
import type { ReadinessGateway } from "@/features/health/application/readiness-gateway";
import type { ServiceReadiness } from "@/features/health/domain/models";

export class HttpReadinessGateway implements ReadinessGateway {
  readiness(signal?: AbortSignal): Promise<ServiceReadiness> {
    return serviceReadiness(signal);
  }
}
