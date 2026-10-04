/**
 * Central query-key factories for the users feature. Values must stay
 * identical to the keys previously inlined in pages.
 */

export interface UserListQueryKeyParams {
  page: number;
  pageSize: number;
  query: string;
  status: string;
  role: string;
  cursor: string;
}

export interface UserSessionsQueryKeyParams {
  page: number;
  pageSize: number;
  cursor: string;
}

export const userQueryKeys = {
  list: (params: UserListQueryKeyParams) => ["admin", "users", params] as const,
  usersPrefix: ["admin", "users"] as const,
  sessions: (userId: string | undefined, params: UserSessionsQueryKeyParams) => ["admin", "users", userId, "sessions", params] as const,
};
