import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type { ThemedReadingAPI, ThemedReadingTask } from "@guizhi/shared/types/themed-reading";

/** 主题页只通过专用白名单访问，页面 iframe 本身不暴露任何 IPC。 */
export const themedReadingApi: ThemedReadingAPI = {
  preview: input => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_PREVIEW,input),
  createView: input => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_VIEW_CREATE,input),
  updateView: input => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_VIEW_UPDATE,input),
  commandView: input => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_VIEW_COMMAND,input),
  destroyView: id => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_VIEW_DESTROY,id),
  onViewEvent: callback => { const receive=(_e:Electron.IpcRendererEvent,value:import("@guizhi/shared/types/reading-page-v3").ReadingViewEvent)=>callback(value);ipcRenderer.on(IPC_CHANNELS.THEMED_READING_VIEW_EVENT,receive);return()=>ipcRenderer.removeListener(IPC_CHANNELS.THEMED_READING_VIEW_EVENT,receive); },
  getState: input => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_STATE, input),
  references: input => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_REFERENCES, input),
  searchConfig: input => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_SEARCH_CONFIG, input),
  continueOffline: taskId => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_OFFLINE, taskId),
  get: (input) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_GET, input),
  generate: (input) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_GENERATE, input),
  cancel: (taskId) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_CANCEL, taskId),
  resume: (taskId) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_RESUME, taskId),
  regenerateAsset: (input) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_REGENERATE_ASSET, input),
  restorePrevious: (input) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_RESTORE_PREVIOUS, input),
  remove: (input) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_REMOVE, input),
  exportHtml: (input) => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_EXPORT, input),
  listTasks: () => ipcRenderer.invoke(IPC_CHANNELS.THEMED_READING_LIST_TASKS),
  onProgress: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, task: ThemedReadingTask) => callback(task);
    ipcRenderer.on(IPC_CHANNELS.THEMED_READING_PROGRESS, listener);
    return () => ipcRenderer.removeListener(IPC_CHANNELS.THEMED_READING_PROGRESS, listener);
  },
};
