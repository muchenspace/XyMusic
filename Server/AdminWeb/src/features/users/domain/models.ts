import type { ArtworkSummary } from "@/shared/domain/artwork";

export type UserStatus = "ACTIVE" | "SUSPENDED" | "DELETED";
export type UserRole = "ADMIN" | "USER";

export interface UserSummary {
  id: string;
  username: string;
  displayName: string;
  bio?: string | null;
  role: UserRole;
  status: UserStatus;
  createdAt: string;
  updatedAt: string;
  avatar?: ArtworkSummary | null;
  version: number;
}

export interface UserSessionSummary {
  id: string;
  installationId: string;
  deviceName: string;
  platform: string;
  appVersion: string;
  active: boolean;
  createdAt: string;
  lastSeenAt: string;
  revokedAt?: string | null;
}

export interface UserDetail extends UserSummary {
  sessions: UserSessionSummary[];
  sessionPage: number;
  sessionPageSize: number;
  sessionTotal: number;
  sessionTotalPages: number;
}

export interface CreateUserInput {
  username: string;
  displayName: string;
  password: string;
  role: UserRole;
}

export interface UpdateUserInput {
  displayName?: string;
  username?: string;
  bio?: string | null;
  role?: UserRole;
  status?: UserStatus;
  expectedVersion: number;
  reason: string;
}

export interface UserListQuery {
  page: number;
  pageSize: number;
  search?: string;
  status?: string;
  role?: string;
}

export interface UserPage {
  items: UserSummary[];
  page: number;
  pageSize: number;
  total: number;
}
