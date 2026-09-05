import type { CaptureSubmission } from "@guizhi/shared/types/mobile-capture";
export async function drafts<T>(action: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open("guizhi-capture", 1);
    open.onupgradeneeded = () => open.result.createObjectStore("drafts");
    open.onerror = () => reject(open.error);
    open.onsuccess = () => {
      const db = open.result, tx = db.transaction("drafts", "readwrite"), request = action(tx.objectStore("drafts"));
      tx.oncomplete = () => { db.close(); resolve(request.result); };
      tx.onerror = () => { db.close(); reject(tx.error); };
    };
  });
}
export const blankDraft = (): CaptureSubmission => ({ requestId: crypto.randomUUID(), input: "", mode: "auto" });
export async function api<T>(path: string, body?: unknown, method = "GET"): Promise<T> {
  const response = await fetch(`/v1/${path}`, { method, credentials: "same-origin", headers: {
    "Content-Type": "application/json", "X-Guizhi-Csrf": "1", "X-Guizhi-Protocol": "1",
  }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(20000) });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error ?? `HTTP ${response.status}`);
  return data as T;
}
export function newSecret() { const bytes = crypto.getRandomValues(new Uint8Array(32)); return btoa(String.fromCharCode(...bytes)).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", ""); }
