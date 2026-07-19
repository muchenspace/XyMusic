import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";
import type { Notifier } from "../../application/ports/Notifier";

export class TauriNotifier implements Notifier {
  async notify(title: string, body: string): Promise<void> {
    if (!isTauriRuntime()) return;
    let granted = await isPermissionGranted();
    if (!granted) granted = await requestPermission() === "granted";
    if (granted) sendNotification({ title, body });
  }
}

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}
