import { registerWikiCompilerIPC } from "./wiki-compiler.ipc";
import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import { WikiDB } from "@guizhi/db";
import type Database from "../database/sqlite";

/**
 * Wiki IPC：目录 / 详情查询、页面编辑与版本回滚。
 * 全文编译经 wiki:compiler 落库；模型调用与链接清洗在 renderer 侧完成。
 */
export function registerWikiIPC(db: Database.Database): void {
  const wiki = new WikiDB(db);
  registerWikiCompilerIPC(db);

  ipcMain.handle(IPC_CHANNELS.WIKI_CATALOG, () => wiki.getCatalog());
  ipcMain.handle(IPC_CHANNELS.WIKI_BACKLINK_COUNTS, () =>
    wiki.getBacklinkCounts(),
  );
  ipcMain.handle(
    IPC_CHANNELS.WIKI_SEARCH,
    (_event, query: string, limit: number) =>
      wiki.searchPages(String(query ?? ""), Number(limit) || 5),
  );
  ipcMain.handle(IPC_CHANNELS.WIKI_GET_PAGE, (_event, id: string) =>
    wiki.getPage(id),
  );
  ipcMain.handle(
    IPC_CHANNELS.WIKI_UPDATE_PAGE,
    (
      _event,
      input: {
        pageId: string;
        body: string;
        linkTargets: string[];
        releaseToAuto?: boolean;
      },
    ) =>
      wiki.updatePageBody({
        pageId: String(input?.pageId ?? ""),
        body: String(input?.body ?? ""),
        linkTargets: Array.isArray(input?.linkTargets)
          ? input.linkTargets.filter(
              (target): target is string => typeof target === "string",
            )
          : [],
        releaseToAuto: input?.releaseToAuto === true,
      }),
  );
  ipcMain.handle(IPC_CHANNELS.WIKI_DELETE_PAGE, (_event, pageId: string) =>
    wiki.deletePage(String(pageId ?? "")),
  );
  ipcMain.handle(IPC_CHANNELS.WIKI_STATUS, () => wiki.getStatus());
  ipcMain.handle(IPC_CHANNELS.WIKI_GRAPH, () => wiki.getGraph());
  ipcMain.handle(IPC_CHANNELS.WIKI_LIST_REVISIONS, (_event, pageId: string) =>
    wiki.listRevisions(pageId),
  );
  ipcMain.handle(
    IPC_CHANNELS.WIKI_RESTORE_REVISION,
    (_event, revisionId: string) => wiki.restoreRevision(revisionId),
  );
}
