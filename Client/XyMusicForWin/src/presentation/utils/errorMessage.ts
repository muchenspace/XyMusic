import { ApiError } from "../../application/errors/ApiError";

export function errorMessage(cause: unknown, fallback = "操作失败，请稍后重试"): string {
  if (cause instanceof DOMException) {
    if (cause.name === "AbortError") return "操作已取消";
    if (cause.name === "TimeoutError") return "请求超时，请稍后重试";
  }
  if (cause instanceof ApiError) return apiErrorMessage(cause, fallback);
  if (cause instanceof TypeError && /fetch|network|load failed/i.test(cause.message)) {
    return "无法连接服务器，请检查网络和服务地址";
  }
  if (cause instanceof Error && containsChinese(cause.message)) return cause.message;
  return fallback;
}

function apiErrorMessage(error: ApiError, fallback: string): string {
  if (containsChinese(error.message)) return error.traceId
    ? `${error.message}（追踪 ID：${error.traceId}）`
    : error.message;
  switch (error.code) {
    case "NETWORK_ERROR": return "无法连接服务器，请检查网络和服务地址";
    case "REQUEST_TIMEOUT": return "请求超时，请稍后重试";
    case "SESSION_EXPIRED": return "登录已失效，请重新登录";
    case "NO_SESSION": return "尚未连接服务器，请先登录";
    case "INVALID_AUTH_RESPONSE": return "服务器返回的登录信息无效";
    case "INVALID_RESPONSE": return "服务器返回了无效响应";
    case "INVALID_PAGINATION": return "服务器返回的分页数据无效";
    case "CREDENTIAL_WRITE_FAILED": return "无法安全保存登录凭据";
    case "CREDENTIAL_DELETE_FAILED": return "无法从 Windows 凭据管理器清除本机登录凭据";
  }
  if (error.status === 400) return "请求参数无效，请检查后重试";
  if (error.status === 401) return "用户名或密码错误，或登录已失效";
  if (error.status === 403) return "当前账号没有执行此操作的权限";
  if (error.status === 404) return "请求的内容不存在或已被删除";
  if (error.status === 409) return "数据已被其他操作更新，请刷新后重试";
  if (error.status === 413) return "上传文件过大";
  if (error.status === 429) return "请求过于频繁，请稍后重试";
  if (error.status >= 500) return "服务器暂时无法完成请求，请稍后重试";
  return fallback;
}

function containsChinese(value: string): boolean {
  return /[\u3400-\u9fff]/u.test(value);
}
