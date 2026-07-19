import { adminApi } from "@/api/admin";
import type { AuditAdminGateway } from "@/features/audit/application/audit-admin-gateway";
import type { AuditListQuery, AuditPage } from "@/features/audit/domain/models";

export class HttpAuditAdminGateway implements AuditAdminGateway {
  list(query: AuditListQuery, signal?: AbortSignal): Promise<AuditPage> {
    return adminApi.audit(query, signal);
  }
}
