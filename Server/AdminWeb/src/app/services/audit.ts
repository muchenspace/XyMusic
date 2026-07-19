import { AuditAdminUseCases } from "@/features/audit/application/audit-admin-use-cases";
import { HttpAuditAdminGateway } from "@/features/audit/infrastructure/http-audit-admin-gateway";

const auditAdmin = new AuditAdminUseCases(new HttpAuditAdminGateway());

export function useAuditAdmin(): AuditAdminUseCases {
  return auditAdmin;
}
