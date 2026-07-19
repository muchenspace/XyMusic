import { sha256Hex } from "@/utils/browser-crypto";

self.onmessage = async (event: MessageEvent<File>) => {
  try {
    const checksum = await sha256Hex(await event.data.arrayBuffer());
    self.postMessage({ checksum });
  } catch (error) {
    self.postMessage({ error: error instanceof Error && /[\u3400-\u9fff]/u.test(error.message) ? error.message : "无法计算封面摘要" });
  }
};
