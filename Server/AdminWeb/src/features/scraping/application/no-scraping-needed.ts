import { ApiError } from "@/shared/application/api-error";

/**
 * Detects the "nothing to scrape" validation response.
 *
 * The backend currently signals this case with `VALIDATION_ERROR` plus a
 * Chinese detail string ("所选曲目均已包含指定字段，无需刮削") and does not
 * expose a dedicated error code. The check is therefore encapsulated here in
 * the application layer instead of leaking into presentation components.
 * TODO(backend): add a stable machine-readable code for this no-op response.
 */
export function isNoScrapingNeededError(error: unknown): error is ApiError {
  return error instanceof ApiError &&
    error.problem.code === "VALIDATION_ERROR" &&
    error.problem.detail?.includes("无需刮削") === true;
}

export function noScrapingNeededDetail(error: ApiError): string {
  return error.problem.detail?.trim() || "所选曲目均已包含指定字段，无需刮削";
}
