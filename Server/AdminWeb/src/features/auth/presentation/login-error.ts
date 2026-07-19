import { ApiConnectionError, ApiError } from "@/shared/application/api-error";

type ErrorReporter = (message: string, error: unknown) => void;

export function loginErrorMessage(
  error: unknown,
  report: ErrorReporter = (message, value) => console.error(message, value),
): string {
  if (error instanceof ApiError) {
    if (error.status === 401) return "用户名或密码错误";
    if (error.status === 403) return "当前账号没有管理员权限";
    if (error.status === 429) return "登录尝试过于频繁，请稍后重试";
    return error.message;
  }
  if (error instanceof ApiConnectionError) {
    if (error.kind === "timeout") return "连接服务器超时，请稍后重试";
    if (error.kind === "aborted") return "登录请求已取消，请重试";
    return "无法连接到服务器，请检查网络和服务地址";
  }
  report("Unexpected administrator login failure", error);
  return "登录时发生客户端异常，请刷新页面后重试";
}
