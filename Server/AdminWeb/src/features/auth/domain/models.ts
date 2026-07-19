import type { ArtworkSummary } from "@/shared/domain/artwork";
import type { UserRole, UserStatus } from "@/features/users/domain/models";

export interface AdminProfile {
  id: string;
  username: string;
  displayName: string;
  bio?: string | null;
  role: UserRole;
  status: UserStatus;
  version: number;
  avatar?: ArtworkSummary | null;
}

export interface AdminSession {
  user: AdminProfile;
  csrfToken?: string;
}
