import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";

export interface AppErrorLogEntry {
  /** 出错的位置，如 library / wiki-compile */
  scope: string;
  /** 用户视角的动作名，如「批量更新」 */
  action: string;
  message: string;
}

export const logApi = {
  /**
   * 记一条业务失败到 logs/error.log。
   *
   * 单向发送、不等回执：日志失败不该反过来影响业务，也不值得让调用方 await。
   */
  appError: (entry: AppErrorLogEntry): void => {
    ipcRenderer.send(IPC_CHANNELS.LOG_APP_ERROR, entry);
  },
};
