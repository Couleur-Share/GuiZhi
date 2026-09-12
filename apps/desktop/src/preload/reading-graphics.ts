import { contextBridge, ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
contextBridge.exposeInMainWorld("readingGraphics", {
  onJob: (callback: (job: unknown) => void) => ipcRenderer.once(IPC_CHANNELS.READING_GRAPHICS_JOB, (_event, job) => callback(job)),
  complete: (result: unknown) => ipcRenderer.send(IPC_CHANNELS.READING_GRAPHICS_RESULT, result),
});
