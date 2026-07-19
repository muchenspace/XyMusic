import type { AuthGateway } from "@/features/auth/application/auth-gateway";
import type { AdminSession } from "@/features/auth/domain/models";

export class AuthUseCases {
  constructor(private readonly gateway: AuthGateway) {}

  session(signal?: AbortSignal): Promise<AdminSession> {
    return this.gateway.session(signal);
  }

  login(username: string, password: string): Promise<AdminSession> {
    return this.gateway.login(username, password);
  }

  logout(): Promise<void> {
    return this.gateway.logout();
  }
}
