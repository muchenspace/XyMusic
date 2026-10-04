import type { ArtworkSummary } from "@/shared/domain/artwork";
import type { Page, PageQuery } from "@/shared/domain/pagination";
import type { UserRole, UserStatus } from "@/shared/domain/user-role";

export type { UserRole, UserStatus } from "@/shared/domain/user-role";

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
  nextSessionCursor?: string;
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
  reason?: string;
}

export type UserListQuery = PageQuery & {
  page: number;
  pageSize: number;
  status?: string;
  role?: string;
};

export type UserPage = Page<UserSummary>;
