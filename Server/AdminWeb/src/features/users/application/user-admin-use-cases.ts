import type { UserAdminGateway } from "@/features/users/application/user-admin-gateway";
import type {
  CreateUserInput,
  UpdateUserInput,
  UserDetail,
  UserListQuery,
  UserPage,
} from "@/features/users/domain/models";

export class UserAdminUseCases {
  constructor(private readonly gateway: UserAdminGateway) {}

  list(query: UserListQuery, signal?: AbortSignal): Promise<UserPage> {
    return this.gateway.list(query, signal);
  }

  detail(userId: string, page: number, pageSize: number, signal?: AbortSignal): Promise<UserDetail> {
    return this.gateway.detail(userId, page, pageSize, signal);
  }

  create(input: CreateUserInput): Promise<UserDetail> {
    return this.gateway.create(input);
  }

  update(userId: string, input: UpdateUserInput): Promise<UserDetail> {
    return this.gateway.update(userId, input);
  }

  resetPassword(userId: string, expectedVersion: number, password: string, reason: string): Promise<void> {
    return this.gateway.resetPassword(userId, expectedVersion, password, reason);
  }

  setDeleted(userId: string, expectedVersion: number, deleted: boolean, reason: string): Promise<void | UserDetail> {
    return deleted
      ? this.gateway.delete(userId, expectedVersion, reason)
      : this.gateway.restore(userId, expectedVersion, reason);
  }

  revokeSession(userId: string, sessionId: string, reason: string): Promise<void> {
    return this.gateway.revokeSession(userId, sessionId, reason);
  }
}
