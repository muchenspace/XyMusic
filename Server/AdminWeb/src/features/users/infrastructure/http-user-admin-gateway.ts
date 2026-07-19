import { adminApi } from "@/api/admin";
import type { UserAdminGateway } from "@/features/users/application/user-admin-gateway";
import type {
  CreateUserInput,
  UpdateUserInput,
  UserDetail,
  UserListQuery,
  UserPage,
} from "@/features/users/domain/models";

export class HttpUserAdminGateway implements UserAdminGateway {
  list(query: UserListQuery, signal?: AbortSignal): Promise<UserPage> {
    return adminApi.users(query, signal);
  }

  detail(userId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<UserDetail> {
    return adminApi.user(userId, { page, pageSize }, signal);
  }

  create(input: CreateUserInput): Promise<UserDetail> {
    return adminApi.createUser(input);
  }

  update(userId: string, input: UpdateUserInput): Promise<UserDetail> {
    return adminApi.updateUser(userId, input);
  }

  async resetPassword(userId: string, expectedVersion: number, password: string, reason: string): Promise<void> {
    await adminApi.resetUserPassword(userId, expectedVersion, password, reason);
  }

  async delete(userId: string, expectedVersion: number, reason: string): Promise<void> {
    await adminApi.deleteUser(userId, expectedVersion, reason);
  }

  restore(userId: string, expectedVersion: number, reason: string): Promise<UserDetail> {
    return adminApi.restoreUser(userId, expectedVersion, reason);
  }

  async revokeSession(userId: string, sessionId: string, reason: string): Promise<void> {
    await adminApi.revokeUserSession(userId, sessionId, reason);
  }
}
