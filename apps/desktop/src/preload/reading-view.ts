import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";

/** 仅在受信外壳安装；不向任何网页暴露 ipcRenderer 或函数。 */
ipcRenderer.on(IPC_CHANNELS.READING_VIEW_COMMAND, (_event, command) => {
  const frame = document.getElementById("reading") as HTMLIFrameElement | null;
  frame?.contentWindow?.postMessage({ reading: 3, command }, "*");
});
window.addEventListener("message", (event) => {
  const frame = document.getElementById("reading") as HTMLIFrameElement | null;
  if (
    event.source !== frame?.contentWindow ||
    event.origin !== "null" ||
    event.data?.reading !== 3
  )
    return;
  const { type, value } = event.data;
  if (
    ![
      "ready",
      "layout",
      "selection",
      "find",
      "image",
      "fault",
      "heartbeat",
      "key",
    ].includes(type)
  )
    return;
  try {
    if (JSON.stringify(value).length > 64000) return;
  } catch {
    return;
  }
  ipcRenderer.send(IPC_CHANNELS.READING_VIEW_BRIDGE, { type, value });
});
