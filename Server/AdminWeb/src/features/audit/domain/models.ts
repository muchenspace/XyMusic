export interface AuditEntry {
  id: string;
  actor: { id: string; displayName: string; username: string } | null;
  action: string;
  resourceType: string;
  resourceId?: string | null;
  result: "SUCCESS" | "FAILURE";
  traceId: string;
  metadata?: unknown;
  createdAt: string;
}

export interface AuditListQuery {
  page: number;
  pageSize: number;
  search?: string;
  action?: string;
  result?: string;
  actorId?: string;
  from?: string;
  to?: string;
  sort?: string;
  order?: "asc" | "desc";
}

export interface AuditPage {
  items: AuditEntry[];
  page: number;
  pageSize: number;
  total: number;
}
