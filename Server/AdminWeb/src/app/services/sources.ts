import { SourceAdminUseCases } from "@/features/sources/application/source-admin-use-cases";
import { HttpSourceAdminGateway } from "@/features/sources/infrastructure/http-source-admin-gateway";

const sources = new SourceAdminUseCases(new HttpSourceAdminGateway());

export function useSourceAdmin(): SourceAdminUseCases {
  return sources;
}
