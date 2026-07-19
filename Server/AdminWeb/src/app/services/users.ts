import { UserAdminUseCases } from "@/features/users/application/user-admin-use-cases";
import { HttpUserAdminGateway } from "@/features/users/infrastructure/http-user-admin-gateway";

const users = new UserAdminUseCases(new HttpUserAdminGateway());

export function useUserAdmin(): UserAdminUseCases {
  return users;
}
