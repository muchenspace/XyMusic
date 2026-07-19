import type {
  CreateUserInput,
  UpdateUserInput,
  UserDetail,
  UserListQuery,
  UserPage,
} from "@/features/users/domain/models";

export interface UserAdminGateway {
  list(query: UserListQuery, signal?: AbortSignal): Promise<UserPage>;
  detail(userId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<UserDetail>;
  create(input: CreateUserInput): Promise<UserDetail>;
  update(userId: string, input: UpdateUserInput): Promise<UserDetail>;
  resetPassword(userId: string, expectedVersion: number, password: string, reason: string): Promise<void>;
  delete(userId: string, expectedVersion: number, reason: string): Promise<void>;
  restore(userId: string, expectedVersion: number, reason: string): Promise<UserDetail>;
  revokeSession(userId: string, sessionId: string, reason: string): Promise<void>;
}
