import type { AdminSession } from "@/features/auth/domain/models";

export interface AuthGateway {
  session(signal?: AbortSignal): Promise<AdminSession>;
  login(username: string, password: string): Promise<AdminSession>;
  logout(): Promise<void>;
}
