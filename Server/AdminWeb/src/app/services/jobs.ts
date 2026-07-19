import { JobAdminUseCases } from "@/features/jobs/application/job-admin-use-cases";
import { HttpJobAdminGateway } from "@/features/jobs/infrastructure/http-job-admin-gateway";

const jobs = new JobAdminUseCases(new HttpJobAdminGateway());

export function useJobAdmin(): JobAdminUseCases {
  return jobs;
}
