export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
    readonly cause?: unknown,
    readonly traceId?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}
