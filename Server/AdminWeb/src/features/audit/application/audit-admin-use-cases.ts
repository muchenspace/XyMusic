import type { AuditAdminGateway } from "@/features/audit/application/audit-admin-gateway";
import type { AuditListQuery, AuditPage } from "@/features/audit/domain/models";

export class AuditAdminUseCases {
  constructor(private readonly gateway: AuditAdminGateway) {}

  list(query: AuditListQuery, signal?: AbortSignal): Promise<AuditPage> {
    return this.gateway.list(query, signal);
  }
}
