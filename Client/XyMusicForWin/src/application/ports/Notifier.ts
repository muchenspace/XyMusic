export interface Notifier {
  notify(title: string, body: string): Promise<void>;
}
