import { SetupUseCases } from "@/features/setup/application/setup-use-cases";
import { HttpSetupGateway } from "@/features/setup/infrastructure/http-setup-gateway";

const setup = new SetupUseCases(new HttpSetupGateway());

export function useSetup(): SetupUseCases {
  return setup;
}
