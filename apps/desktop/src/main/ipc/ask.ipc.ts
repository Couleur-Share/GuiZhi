/**
 * AI 问答会话 IPC：会话元数据列表 + 整行读写。
 */
import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { SaveAskSessionInput } from "@guizhi/shared/types";
import { AskSessionDB } from "@guizhi/db";
import Database from "../database/sqlite";

export function registerAskIPC(db: Database.Database): void {
  const sessions = new AskSessionDB(db);

  ipcMain.handle(IPC_CHANNELS.ASK_SESSION_LIST, () => sessions.list());

  ipcMain.handle(IPC_CHANNELS.ASK_SESSION_GET, (_event, id: string) =>
    sessions.get(id),
  );

  ipcMain.handle(
    IPC_CHANNELS.ASK_SESSION_SAVE,
    (_event, input: SaveAskSessionInput) => {
      if (
        !input ||
        typeof input.id !== "string" ||
        typeof input.title !== "string" ||
        typeof input.messagesJson !== "string"
      ) {
        throw new Error("ask:saveSession 需要 { id, title, messagesJson }");
      }
      return sessions.save(input);
    },
  );

  ipcMain.handle(IPC_CHANNELS.ASK_SESSION_DELETE, (_event, id: string) =>
    sessions.delete(id),
  );
}
