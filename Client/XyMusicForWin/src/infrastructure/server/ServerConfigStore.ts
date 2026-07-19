import type { ServerConfig, ServerProtocol } from "../../application/ports/SessionRepository";

export class ServerConfigStore {
  constructor(private readonly storage: Pick<Storage, "getItem" | "setItem"> = localStorage) {}

  read(): ServerConfig {
    try {
      const raw = this.storage.getItem(SERVER_CONFIG_KEY);
      if (!raw) return emptyServerConfig();
      const value = JSON.parse(raw) as Partial<ServerConfig>;
      return normalizeServerConfig(value);
    } catch {
      return emptyServerConfig();
    }
  }

  write(config: ServerConfig): ServerConfig {
    const normalized = normalizeServerConfig(config);
    try {
      this.storage.setItem(SERVER_CONFIG_KEY, JSON.stringify(normalized));
    } catch {
      // The active session still carries its server URL when optional UI storage is unavailable.
    }
    return normalized;
  }
}

export function emptyServerConfig(): ServerConfig {
  return { protocol: "http", host: "", port: "" };
}

export function normalizeServerConfig(value: Partial<ServerConfig>): ServerConfig {
  const protocol = normalizeProtocol(value.protocol);
  const host = normalizeHost(value.host);
  const port = normalizePort(value.port);
  buildServerUrl({ protocol, host, port });
  return { protocol, host, port };
}

export function buildServerUrl(config: ServerConfig): string {
  const protocol = normalizeProtocol(config.protocol);
  const host = normalizeHost(config.host);
  const port = normalizePort(config.port);
  const formattedHost = host.includes(":") ? `[${host}]` : host;
  let url: URL;
  try {
    url = new URL(`${protocol}://${formattedHost}:${port}`);
  } catch {
    throw new Error("请输入有效的服务器 IP 或域名");
  }
  if (!url.hostname || url.username || url.password) throw new Error("请输入有效的服务器 IP 或域名");
  return url.origin;
}

export function parseServerUrl(value: string): ServerConfig {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return emptyServerConfig();
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return emptyServerConfig();
  const protocol = url.protocol.slice(0, -1) as ServerProtocol;
  const host = url.hostname.replace(/^\[|\]$/g, "");
  const port = url.port || (protocol === "https" ? "443" : "80");
  try {
    return normalizeServerConfig({ protocol, host, port });
  } catch {
    return emptyServerConfig();
  }
}

function normalizeProtocol(value: unknown): ServerProtocol {
  if (value === "http" || value === "https") return value;
  throw new Error("请选择服务器协议");
}

function normalizeHost(value: unknown): string {
  if (typeof value !== "string" || !value.trim()) throw new Error("请输入服务器 IP 或域名");
  const host = value.trim().replace(/^\[|\]$/g, "");
  if (!host || /[\s/@?#]/.test(host) || host.includes("://")) throw new Error("服务器 IP 或域名格式错误");
  return host;
}

function normalizePort(value: unknown): string {
  const port = typeof value === "number" ? String(value) : typeof value === "string" ? value.trim() : "";
  if (!/^\d+$/.test(port)) throw new Error("请输入服务器端口");
  const parsed = Number(port);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 65_535) throw new Error("服务器端口必须在 1-65535 之间");
  return String(parsed);
}

const SERVER_CONFIG_KEY = "xymusic.desktop.server.v1";
