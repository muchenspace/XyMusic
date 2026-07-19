import { AuthUseCases } from "@/features/auth/application/auth-use-cases";
import { HttpAuthGateway } from "@/features/auth/infrastructure/http-auth-gateway";

const auth = new AuthUseCases(new HttpAuthGateway());

export function useAuth(): AuthUseCases {
  return auth;
}
