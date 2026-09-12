import { ipcMain, type IpcMainInvokeEvent } from "electron";
import type Database from "../database/sqlite";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { articleContext } from "../services/article-ask-context";
import { searchArticleWeb } from "../services/article-ask-search";
import { logAppError } from "../diagnostic-log";

const running = new Map<string, AbortController>();
function sender(event: IpcMainInvokeEvent) {
  if (!event.senderFrame || event.senderFrame !== event.sender.mainFrame) throw new Error("本文问答仅允许应用主界面调用");
}
function requestKey(event: IpcMainInvokeEvent, id: string) {
  sender(event);
  if (typeof id !== "string" || !/^[\w-]{1,100}$/.test(id)) throw new Error("请求标识无效");
  return `${event.sender.id}:${id}`;
}
export function registerArticleAskIPC(db: Database.Database) {
  for (const controller of running.values()) controller.abort();
  running.clear();
  const wrap = (action: string, run: (event: IpcMainInvokeEvent, input: any) => any) => async (event: IpcMainInvokeEvent, input: any) => {
    try { sender(event); return await run(event, input); }
    catch (error) {
      const message = error instanceof Error ? error.message : "本文问答失败";
      logAppError({ scope: "articleAsk", action, message });
      return { success: false, error: message };
    }
  };
  ipcMain.handle(IPC_CHANNELS.ARTICLE_ASK_CONTEXT, wrap("context", (_event, input) => ({ success: true, context: articleContext(db, input) })));
  ipcMain.handle(IPC_CHANNELS.ARTICLE_ASK_CANCEL, wrap("cancel", (event, id) => {
    running.get(requestKey(event, id))?.abort();
    return { success: true };
  }));
  ipcMain.handle(IPC_CHANNELS.ARTICLE_ASK_SEARCH, wrap("search", async (event, input) => {
    const key = requestKey(event, input?.requestId);
    if (!Array.isArray(input.queries) || !input.queries.length || input.queries.length > 2 || input.queries.some(q => typeof q !== "string" || !q.trim() || q.length > 400)) throw new Error("搜索词无效");
    if ([...running.keys()].some(k => k.startsWith(`${event.sender.id}:`))) throw new Error("已有本文搜索正在执行，请稍后重试");
    const controller = new AbortController(); running.set(key, controller);
    const destroy = () => controller.abort();
    event.sender.once("destroyed", destroy);
    try { return await searchArticleWeb([...new Set<string>(input.queries)], AbortSignal.any([controller.signal, AbortSignal.timeout(90000)])); }
    finally { if (running.get(key) === controller) running.delete(key); event.sender.removeListener("destroyed", destroy); }
  }));
}
