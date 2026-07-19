import { ApiError } from "./ApiError";

export function normalizeServerUrl(value: string): string {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch (error) {
    throw new Error(`请输入有效的服务器地址：${error instanceof Error ? error.message : "格式错误"}`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("服务器地址必须使用 HTTP 或 HTTPS");
  if (url.username || url.password) throw new Error("服务器地址不能包含用户名或密码");
  if (url.search || url.hash) throw new Error("服务器地址不能包含查询参数或片段");
  if (url.pathname !== "/") throw new Error("服务器地址不能包含路径");
  return url.toString().replace(/\/+$/, "");
}

export function resolveApiUrl(path: string, serverUrl: string): URL {
  const base = new URL(`${serverUrl}/`);
  const resolved = new URL(path, base);
  if (resolved.origin !== base.origin) throw new ApiError("请求地址不属于当前服务器", 0, "INVALID_REQUEST_URL");
  return resolved;
}
