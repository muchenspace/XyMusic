import { AuthUseCases } from "@/features/auth/application/auth-use-cases";
import {
  AuthSessionCoordinator,
  type AuthSessionStatePort,
  type SessionCoordinationPort,
} from "@/features/auth/application/auth-session-coordinator";
import { HttpAuthGateway } from "@/features/auth/infrastructure/http-auth-gateway";
import { setCsrfToken } from "@/api/client";
import { clearAdminQueryCache } from "@/app/query-client";

const auth = new AuthUseCases(new HttpAuthGateway());
const sessionCoordination: SessionCoordinationPort = {
  setCsrfToken: (token) => setCsrfToken(token),
  clearQueryCache: () => clearAdminQueryCache(),
};

export function useAuth(): AuthUseCases {
  return auth;
}

export function createAuthSessionCoordinator(state: AuthSessionStatePort): AuthSessionCoordinator {
  return new AuthSessionCoordinator(auth, sessionCoordination, state);
}
