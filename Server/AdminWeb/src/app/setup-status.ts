import type { SetupStatus } from "@/features/setup/domain/models";
import { queryClient } from "@/app/query-client";
import { appQueryKeys } from "@/app/query-keys";
import { useSetup } from "@/app/services/setup";

/**
 * Setup status cache.
 *
 * queryClient is the single source of truth for the status value; the module
 * level timestamp only records when `currentSetupState` last wrote it, so the
 * short-lived staleness windows (5s / 1s) and same-tick request deduplication
 * stay independent from unrelated query cache activity.
 */
const SETUP_STATUS_QUERY_KEY = appQueryKeys.setupStatus;
let setupStateCheckedAt = 0;
let setupRequest: Promise<SetupStatus> | undefined;
const setup = useSetup();

export const SETUP_STATUS_STALE_MS = 5_000;
export const SETUP_REQUIRED_DEDUP_MS = 1_000;

export function canReuseSetupState(
  state: SetupStatus | undefined,
  checkedAt: number,
  now = Date.now(),
): state is SetupStatus {
  if (!state || checkedAt <= 0) return false;
  const age = now - checkedAt;
  if (age < 0) return false;
  return age < (state.setupRequired ? SETUP_REQUIRED_DEDUP_MS : SETUP_STATUS_STALE_MS);
}

export function cacheSetupState(value: SetupStatus): void {
  setupStateCheckedAt = Date.now();
  queryClient.setQueryData(SETUP_STATUS_QUERY_KEY, value);
}

export async function currentSetupState(): Promise<SetupStatus> {
  if (setupRequest) return setupRequest;
  const cached = queryClient.getQueryData<SetupStatus>(SETUP_STATUS_QUERY_KEY);
  if (canReuseSetupState(cached, setupStateCheckedAt)) return cached;
  setupRequest = setup.status().then((value) => {
    cacheSetupState(value);
    return value;
  }).finally(() => { setupRequest = undefined; });
  return setupRequest;
}

export function clearSetupState(): void {
  setupStateCheckedAt = 0;
  queryClient.removeQueries({ queryKey: SETUP_STATUS_QUERY_KEY });
}

export function invalidateSetupState(): void {
  setupStateCheckedAt = 0;
  void queryClient.invalidateQueries({ queryKey: SETUP_STATUS_QUERY_KEY });
}
