import { ipcMain, shell } from "electron";
import { IPC_CHANNELS as C } from "@guizhi/shared/constants/ipc-channels";
import { KnowledgeItemDB, WebSourceDB, webContentHash } from "@guizhi/db";
import type {
  AdoptWebVersionInput,
  CreateCrawlJobInput,
} from "@guizhi/shared/types";
import type Database from "../database/sqlite";
import { CrawlService } from "../services/web-capture/crawl-service";
import {
  getWebCaptureStatus,
  shutdownWebCapture,
} from "../services/web-capture/web-capture";
let service: CrawlService | undefined;
const id = (value: unknown): string => {
  if (typeof value !== "string" || !value || value.length > 100)
    throw new Error("记录 ID 无效");
  return value;
};
async function response<T>(
  action: () => T | Promise<T>,
): Promise<{ ok: boolean; data?: T; error?: string }> {
  try {
    return { ok: true, data: await action() };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : "网页操作失败",
    };
  }
}
export function getCrawlService(): CrawlService {
  if (!service) throw new Error("网页采集服务尚未就绪");
  return service;
}
export function registerWebCaptureIPC(db: Database.Database): void {
  void service?.close();
  service = new CrawlService(db);
  ipcMain.handle(C.WEB_STATUS, () => response(getWebCaptureStatus));
  ipcMain.handle(C.WEB_REPAIR, () =>
    response(async () => {
      // 修复入口只定位当前版本安装包；不执行 pip 或在线替换组件。
      const { app } = await import("electron");
      await shell.openExternal(
        `https://github.com/Couleur-Share/GuiZhi/releases/tag/v${app.getVersion()}`,
      );
    }),
  );
  ipcMain.handle(C.CRAWL_CREATE, (_e, input: CreateCrawlJobInput) =>
    response(() => {
      if (input?.purpose !== "documents")
        throw new Error("网页研究批次必须通过研究流程创建");
      return getCrawlService().create(input);
    }),
  );
  ipcMain.handle(C.CRAWL_LIST, () =>
    response(() => getCrawlService().jobs.list()),
  );
  ipcMain.handle(C.CRAWL_GET, (_e, value: unknown) =>
    response(() => ({
      job: getCrawlService().jobs.get(id(value)),
      pages: getCrawlService().jobs.pages(id(value)),
    })),
  );
  ipcMain.handle(C.CRAWL_PAUSE, (_e, value: unknown) =>
    response(() => getCrawlService().pause(id(value))),
  );
  ipcMain.handle(C.CRAWL_RESUME, (_e, value: unknown) =>
    response(() => {
      const job = getCrawlService().jobs.get(id(value));
      if (!job || job.status === "canceled")
        throw new Error("批次不存在或已经取消");
      void getCrawlService()
        .resume(job.id)
        .catch((error) =>
          getCrawlService().jobs.setStatus(job.id, "paused", String(error)),
        );
    }),
  );
  ipcMain.handle(C.CRAWL_CANCEL, (_e, value: unknown) =>
    response(() => getCrawlService().cancel(id(value))),
  );
  ipcMain.handle(C.CRAWL_RETRY, (_e, value: unknown) =>
    response(() => {
      getCrawlService().jobs.retry(id(value));
      void getCrawlService().resume(id(value));
    }),
  );
  ipcMain.handle(C.WEB_VERSIONS, (_e, value: unknown) =>
    response(() => {
      const itemId = id(value),
        item = new KnowledgeItemDB(db).get(itemId);
      if (!item) throw new Error("条目不存在");
      const source = new WebSourceDB(db);
      const versions = source.versions(itemId),
        baseline = source.baseline(itemId);
      const latest = versions.find(
        (version) => version.kind === "remote" && version.complete,
      );
      return {
        versions,
        content: item.content,
        title: item.title,
        contentHash: webContentHash(item.content),
        summaryStale: !!baseline?.summary_stale,
        pendingVersion: !!latest && latest.id !== baseline?.version_id,
      };
    }),
  );
  ipcMain.handle(C.WEB_ADOPT, (_e, input: AdoptWebVersionInput) =>
    response(() => {
      id(input?.itemId);
      id(input?.versionId);
      if (
        !/^[a-f0-9]{64}$/.test(input.expectedContentHash) ||
        typeof input.expectedTitle !== "string"
      )
        throw new Error("版本比较基线无效");
      new WebSourceDB(db).adopt(input);
    }),
  );
}
export async function closeWebCapture(): Promise<void> {
  await service?.close();
  await shutdownWebCapture();
}
