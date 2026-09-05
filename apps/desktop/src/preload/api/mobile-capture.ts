import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { CaptureDevice, CapturePairing, MobileCaptureSettings } from "@guizhi/shared/types/mobile-capture";
async function invoke<T>(action: string, args?: Record<string, unknown>): Promise<T> {
  const result = await ipcRenderer.invoke(IPC_CHANNELS.MOBILE_CAPTURE, action, args);
  if (!result.success) throw new Error(result.error ?? "手机收集操作失败");
  return result.data;
}
export const mobileCaptureApi = {
  status: () => invoke<MobileCaptureSettings>("status"),
  activate: (origin: string, invite: string) => invoke<MobileCaptureSettings>("activate", { origin, invite }),
  configure: (paused: boolean, collectionId: string | null) => invoke<MobileCaptureSettings>("configure", { paused, collectionId }),
  pair: () => invoke<CapturePairing & { url: string }>("pair"),
  pairings: () => invoke<CapturePairing[]>("pairings"),
  devices: () => invoke<CaptureDevice[]>("devices"),
  confirm: (id: string, deviceId: string) => invoke("confirm", { id, deviceId }),
  revoke: (id: string) => invoke("revoke", { id }),
  disable: () => invoke<MobileCaptureSettings>("disable"),
  fetch: () => invoke<MobileCaptureSettings>("fetch"),
};
