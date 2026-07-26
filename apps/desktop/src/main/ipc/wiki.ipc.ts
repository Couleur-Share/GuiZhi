import { ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { WikiApplyCompilationInput } from "@guizhi/shared/types";
import { WikiDB } from "@guizhi/db";
import type Database from "../database/sqlite";

/**
 * Wiki IPC：目录 / 详情查询与编译结果落库。
 * 编译编排（LLM 调用、链接清洗）在 renderer 侧完成。
 */
export function registerWikiIPC(db: Database.Database): void {
  const wiki = new WikiDB(db);

  ipcMain.handle(IPC_CHANNELS.WIKI_CATALOG, () => wiki.getCatalog());
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
  ipcMain.handle(
    IPC_CHANNELS.WIKI_APPLY_COMPILATION,
    (_event, input: WikiApplyCompilationInput) => {
      wiki.applyCompilation(input);
    },
  );
  ipcMain.handle(IPC_CHANNELS.WIKI_LIST_COMPILABLE, () =>
    wiki.listCompilableItems(),
  );
  ipcMain.handle(IPC_CHANNELS.WIKI_LIST_INGESTIONS, () =>
    wiki.listIngestions(),
  );
  ipcMain.handle(IPC_CHANNELS.WIKI_STATUS, () => wiki.getStatus());
  ipcMain.handle(IPC_CHANNELS.WIKI_GRAPH, () => wiki.getGraph());
  ipcMain.handle(IPC_CHANNELS.WIKI_CLEAR, () => {
    wiki.clearAll();
  });
  ipcMain.handle(
    IPC_CHANNELS.WIKI_RECORD_FAILURE,
    (
      _event,
      itemId: string,
      contentHash: string,
      nextAttemptAt: number | null,
    ) => wiki.recordCompilationFailure(itemId, contentHash, nextAttemptAt),
  );
  ipcMain.handle(IPC_CHANNELS.WIKI_LIST_REVISIONS, (_event, pageId: string) =>
    wiki.listRevisions(pageId),
  );
  ipcMain.handle(
    IPC_CHANNELS.WIKI_RESTORE_REVISION,
    (_event, revisionId: string) => wiki.restoreRevision(revisionId),
  );
}
