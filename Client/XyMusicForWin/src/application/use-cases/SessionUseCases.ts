import type { ServerConfig, SessionRepository } from "../ports/SessionRepository";

export class SessionUseCases {
  constructor(private readonly repository: SessionRepository) {}

  restore() { return this.repository.restore(); }
  register(server: ServerConfig, username: string, password: string) { return this.repository.register(server, username, password); }
  login(server: ServerConfig, username: string, password: string) { return this.repository.login(server, username, password); }
  updateProfile(input: Parameters<SessionRepository["updateProfile"]>[0]) { return this.repository.updateProfile(input); }
  uploadAvatar(upload: Parameters<SessionRepository["uploadAvatar"]>[0]) { return this.repository.uploadAvatar(upload); }
  logout() { return this.repository.logout(); }
  logoutAll() { return this.repository.logoutAll(); }
  switchServer(server: ServerConfig) { return this.repository.switchServer(server); }
  serverConfig() { return this.repository.serverConfig(); }
}
