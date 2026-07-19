export interface ProblemDetails {
  type?: string;
  title: string;
  status: number;
  detail?: string;
  suggestion?: string;
  instance?: string;
  code?: string;
  traceId?: string;
  errors?: Record<string, string[]>;
  fieldErrors?: Record<string, string[]>;
  expectedVersion?: number;
  currentVersion?: number;
  retryAfterSeconds?: number;
  conflictResourceType?: string;
  conflictResourceId?: string;
  albumId?: string;
  trackId?: string;
  setupStage?: string;
  decisionResource?: string;
  databaseState?: string;
  rollbackIncomplete?: boolean;
  destructiveStageStarted?: boolean;
  migrationRequired?: boolean;
  reusable?: string[];
  missing?: string[];
  conflictType?: string;
  duplicateAlbums?: Array<{ id: string; title: string; version: number }>;
}

export class ApiError extends Error {
  readonly status: number;
  readonly problem: ProblemDetails;

  constructor(problem: ProblemDetails) {
    super(problemMessage(problem));
    this.name = "ApiError";
    this.status = problem.status;
    this.problem = problem;
  }
}

export type ApiConnectionFailure = "network" | "timeout" | "aborted";

export class ApiConnectionError extends Error {
  readonly kind: ApiConnectionFailure;
  readonly cause: unknown;

  constructor(kind: ApiConnectionFailure, cause: unknown) {
    super(kind === "timeout"
      ? "请求超时，服务器未在限定时间内完成处理。\n处理建议：确认服务端和依赖服务运行正常，避免重复提交后再重试。"
      : kind === "aborted"
        ? "请求已取消，未确认服务端是否完成处理。"
        : "无法连接服务器，请检查网络和服务地址。\n处理建议：确认后端已启动，并检查域名解析、端口、防火墙和反向代理配置。");
    this.name = "ApiConnectionError";
    this.kind = kind;
    this.cause = cause;
  }
}

export function apiErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) return error.message;
  return fallback;
}

function problemMessage(problem: ProblemDetails): string {
  const resourceConflict = resourceConflictMessage(problem);
  if (resourceConflict) return detailedProblemMessage(resourceConflict, problem);
  const raw = problem.detail?.trim() || problem.title?.trim() || "";
  if (raw && /[\u3400-\u9fff]/u.test(raw)) return detailedProblemMessage(raw, problem);
  switch (problem.code) {
    case "AUTHENTICATION_FAILED":
    case "INVALID_CREDENTIALS":
      return detailedProblemMessage("用户名或密码错误", problem);
    case "SESSION_EXPIRED":
    case "SESSION_REVOKED":
      return detailedProblemMessage("登录已失效，请重新登录", problem);
    case "VERSION_CONFLICT":
    case "CONFLICT":
      return detailedProblemMessage("数据已被其他操作更新，请刷新后重试", problem);
    case "RATE_LIMITED":
      return detailedProblemMessage("请求过于频繁，请稍后重试", problem);
  }
  if (problem.status === 400) return detailedProblemMessage("请求参数无效，请检查后重试", problem);
  if (problem.status === 401) return detailedProblemMessage("登录已失效，请重新登录", problem);
  if (problem.status === 403) return detailedProblemMessage("当前账号没有执行此操作的权限", problem);
  if (problem.status === 404) return detailedProblemMessage("请求的资源不存在或已被删除", problem);
  if (problem.status === 409) return detailedProblemMessage("数据状态已发生变化，请刷新后重试", problem);
  if (problem.status === 413) return detailedProblemMessage("上传内容过大，请缩小文件后重试", problem);
  if (problem.status === 422) return detailedProblemMessage("提交内容未通过校验，请检查输入", problem);
  if (problem.status === 429) return detailedProblemMessage("请求过于频繁，请稍后重试", problem);
  if (problem.status >= 500) return detailedProblemMessage("服务器暂时无法完成请求，请稍后重试", problem);
  return detailedProblemMessage("请求失败，请稍后重试", problem);
}

function resourceConflictMessage(problem: ProblemDetails): string | undefined {
  if (problem.code !== "RESOURCE_CONFLICT") return undefined;
  switch (problem.conflictResourceType) {
    case "library_scan":
      return "音源扫描仍在进行，请等待扫描完成后重试";
    case "media_job":
      return "媒体处理仍在进行，请等待任务结束后重试";
    case "metadata_writeback_job":
      return "Tag 写回仍在进行，请等待任务结束后重试";
    default:
      return undefined;
  }
}

function detailedProblemMessage(message: string, problem: ProblemDetails): string {
  const parts = [message.trim()];
  const suggestion = problem.suggestion?.trim();
  if (suggestion && suggestion !== message.trim()) parts.push(`处理建议：${suggestion}`);
  if (problem.traceId) parts.push(`追踪 ID：${problem.traceId}`);
  return parts.join("\n");
}
