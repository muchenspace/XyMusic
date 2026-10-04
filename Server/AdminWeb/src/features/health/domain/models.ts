export interface ServiceReadiness {
  status: "ready" | "unavailable";
  reason: "runtime_unavailable" | "worker_unavailable" | null;
  runtime: {
    phase: string;
    source: string;
    generation: number;
    startedAt: string | null;
  };
  worker: {
    mode: "inline" | "external";
    state: string;
    responsive: boolean;
    synchronized: boolean;
    available: boolean;
    updatedAt: string | null;
  } | null;
}
