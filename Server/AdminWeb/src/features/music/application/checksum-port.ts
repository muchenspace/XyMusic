/**
 * Port for computing the SHA-256 checksum of an upload file. Implementations
 * may use a Web Worker; the application layer only depends on this contract.
 */
export interface ChecksumPort {
  checksum(file: File, signal?: AbortSignal): Promise<string>;
}
