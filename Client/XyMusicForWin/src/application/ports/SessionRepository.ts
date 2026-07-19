export interface UserProfile {
  id: string;
  username: string;
  displayName: string;
  bio: string | null;
  avatarUrl?: string;
  role: "USER" | "ADMIN";
  version: number;
}

export interface UserSession {
  user: UserProfile;
}

export interface RegistrationResult {
  userId: string;
  username: string;
  status: "ACTIVE";
}

export type ServerProtocol = "http" | "https";

export interface ServerConfig {
  protocol: ServerProtocol;
  host: string;
  port: string;
}

export interface SessionActionResult {
  warning: string | null;
}

export interface ServerSwitchResult extends SessionActionResult {
  server: ServerConfig;
}

export interface SessionRepository {
  restore(): Promise<UserSession | null>;
  register(server: ServerConfig, username: string, password: string): Promise<RegistrationResult>;
  login(server: ServerConfig, username: string, password: string): Promise<UserSession>;
  updateProfile(input: { displayName: string; bio: string | null; expectedVersion: number }): Promise<UserSession>;
  uploadAvatar(file: File): Promise<UserSession>;
  logout(): Promise<SessionActionResult>;
  logoutAll(): Promise<SessionActionResult>;
  switchServer(server: ServerConfig): Promise<ServerSwitchResult>;
  serverConfig(): ServerConfig;
}
