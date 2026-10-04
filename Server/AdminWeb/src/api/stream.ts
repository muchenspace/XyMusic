import { buildApiUrl } from "@/api/http-core";

export function openEventStream(path: string): EventSource {
  return new EventSource(buildApiUrl(path), { withCredentials: true });
}
