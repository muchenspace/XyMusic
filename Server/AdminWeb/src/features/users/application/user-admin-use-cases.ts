import type { UserAdminGateway } from "@/features/users/application/user-admin-gateway";
import type { CreateUserInput, UpdateUserInput, UserDetail, UserListQuery, UserPage } from "@/features/users/domain/models";
import type { UserSaveCommand } from "@/features/users/application/user-editor";

export class UserAdminUseCases {
  constructor(private readonly gateway: UserAdminGateway) {}

  list(query: UserListQuery, signal?: AbortSignal): Promise<UserPage> {
    return this.gateway.list(query, signal);
  }

  detail(userId: string, query: Pick<UserListQuery, "page" | "pageSize" | "cursor" | "cursorMode">, signal?: AbortSignal): Promise<UserDetail> {
    return this.gateway.detail(userId, query, signal);
  }

  create(input: CreateUserInput): Promise<UserDetail> {
    return this.gateway.create(input);
  }

  update(userId: string, input: UpdateUserInput): Promise<UserDetail> {
    return this.gateway.update(userId, input);
  }

  /** Executes a validated editor command, routing to create or update. */
  save(command: UserSaveCommand): Promise<UserDetail> {
    return command.kind === "create"
      ? this.gateway.create(command.input)
      : this.gateway.update(command.userId, command.input);
  }

  resetPassword(userId: string, expectedVersion: number, password: string, reason = ""): Promise<void> {
    return this.gateway.resetPassword(userId, expectedVersion, password, reason);
  }

  setDeleted(userId: string, expectedVersion: number, deleted: boolean, reason = ""): Promise<void | UserDetail> {
    return deleted
      ? this.gateway.delete(userId, expectedVersion, reason)
      : this.gateway.restore(userId, expectedVersion, reason);
  }

  revokeSession(userId: string, sessionId: string, reason = ""): Promise<void> {
    return this.gateway.revokeSession(userId, sessionId, reason);
  }
}
