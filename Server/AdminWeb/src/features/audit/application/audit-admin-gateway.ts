import type { AuditListQuery, AuditPage } from "@/features/audit/domain/models";

export interface AuditAdminGateway {
  list(query: AuditListQuery, signal?: AbortSignal): Promise<AuditPage>;
}
