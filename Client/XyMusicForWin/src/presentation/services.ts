import { inject, type InjectionKey } from "vue";
import type { ApplicationServices } from "../application/services";

export const applicationServicesKey: InjectionKey<ApplicationServices> = Symbol("application-services");

export function useApplicationServices(): ApplicationServices {
  const services = inject(applicationServicesKey);
  if (!services) throw new Error("Application services are not configured");
  return services;
}
