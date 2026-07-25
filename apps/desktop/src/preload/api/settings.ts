import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { Settings } from "@guizhi/shared/types";

export const settingsApi = {
  get: () => ipcRenderer.invoke(IPC_CHANNELS.SETTINGS_GET),
  set: (settings: Partial<Settings>) =>
    ipcRenderer.invoke(IPC_CHANNELS.SETTINGS_SET, settings),
};
