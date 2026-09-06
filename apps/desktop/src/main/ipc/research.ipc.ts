import { createLocalResearchEvidence } from "../services/research/local-evidence";
import { validateCrawlInput } from "@guizhi/shared/utils/web-scope";
import { getWebCaptureStatus } from "../services/web-capture/web-capture";
import { planResearch, writeResearchReport } from "../services/research/research-ai";
import { createResearchReader } from "../services/research/read-research";
import { readYtDlpPathSetting } from "../services/import/import-service";
import { BrowserWindow, ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import {
  isResearchDayRange,
  isResearchSource,
  type CreateResearchRunInput,
  type ResearchRunDetail,
} from "@guizhi/shared/types";
import type Database from "../database/sqlite";
import { getRegisteredImportService } from "./import.ipc";
import { ResearchService } from "../services/research/research-service";
import { getBrowserCaptureService, PlatformCaptureError } from "../services/platform-capture/browser-capture";
import { readNetworkProxySetting } from "../services/import/import-service";

let service: ResearchService | null = null;

function validCreateInput(value: unknown): value is CreateResearchRunInput {
  if (!value || typeof value !== "object") return false;
  const input = value as CreateResearchRunInput;
  return typeof input.topic === "string" && input.topic.trim().length > 0 && input.topic.trim().length <= 100
    && (input.includeComments === undefined || typeof input.includeComments === "boolean")
    && isResearchDayRange(input.dayRange)
    && (input.depth === "quick" || input.depth === "deep")
    && Array.isArray(input.sources) && input.sources.length > 0
    && input.sources.length <= 4 && input.sources.every(isResearchSource)
    && (input.timeScope === undefined || input.timeScope === "all" || input.timeScope === "recent")
    && new Set(input.sources).size === input.sources.length
    && (input.knowledgeScope === undefined || (input.knowledgeScope != null && typeof input.knowledgeScope === "object" && (input.knowledgeScope.kind === "all" || (input.knowledgeScope.kind === "collection" && typeof input.knowledgeScope.collectionId === "string" && input.knowledgeScope.collectionId.length > 0))));
}

function id(value: unknown): string {
  if (typeof value !== "string" || !value.trim()) throw new Error("研究记录 ID 不合法");
  return value;
}

function broadcast(detail: ResearchRunDetail): void {
  for (const window of BrowserWindow.getAllWindows()) {
    if (!window.isDestroyed()) window.webContents.send(IPC_CHANNELS.RESEARCH_CHANGED, detail);
  }
}

export function registerResearchIPC(db: Database.Database): void {
  const browser = getBrowserCaptureService({ getNetworkProxy: () => readNetworkProxySetting(db) });
  service = new ResearchService(db, {
    localEvidence: createLocalResearchEvidence(db),
    plan: planResearch, report: writeResearchReport,
    read: createResearchReader(browser, () => readYtDlpPathSetting(db)),
    onChanged: broadcast,
    enqueueImports: (inputs) => getRegisteredImportService().queue.enqueue(inputs),
  });
  ipcMain.handle(IPC_CHANNELS.RESEARCH_LIST, () => service!.list());
  ipcMain.handle(IPC_CHANNELS.RESEARCH_GET, (_event, runId: unknown) => service!.getDetail(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_CREATE, async (_event, input: unknown) => {
    if (!validCreateInput(input)) throw new Error("研究参数不合法");
    if (input.sources.includes("web")) {
      input.webSeeds=validateCrawlInput({purpose:"research",seeds:input.webSeeds!}).seeds;
      const status=await getWebCaptureStatus();if(!status.available) throw new Error(status.reason);
    }
    return service!.createAndRun({ ...input, topic: input.topic.trim(), sources: [...input.sources] });
  });
  ipcMain.handle(IPC_CHANNELS.RESEARCH_CANCEL, (_event, runId: unknown) => service!.cancel(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_DELETE, (_event, runId: unknown) => service!.delete(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_CLONE, (_event, runId: unknown, replan: unknown) => { if (replan !== undefined && typeof replan !== "boolean") throw new Error("重新规划参数不合法"); return service!.cloneAndRun(id(runId), replan === true); });
  ipcMain.handle(IPC_CHANNELS.RESEARCH_VERIFY_AND_RETRY_SOURCE, (event, runId: unknown, source: unknown) => {
    if (source !== "douyin" && source !== "xiaohongshu") throw new Error("该平台不支持登录验证");
    const parent = BrowserWindow.fromWebContents(event.sender);
    return service!.verifyAndRetrySource(id(runId), source, async (topic, signal) => {
      const browser = getBrowserCaptureService({ getNetworkProxy: () => readNetworkProxySetting(db) });
      const status = await browser.login(source, false, parent, source === "douyin" ? topic : undefined, signal);
      if (!status.loggedIn) throw new PlatformCaptureError("login_required", "尚未确认登录，请完成平台验证后重试");
    });
  });
  ipcMain.handle(IPC_CHANNELS.RESEARCH_COMPARE, (_event, runId: unknown, baseline: unknown) => service!.compare(id(runId), baseline === undefined ? undefined : id(baseline)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_BASELINES, (_event, runId: unknown) => service!.baselines(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_SET_BASELINE, (_event, runId: unknown, baseline: unknown) => service!.setBaseline(id(runId), id(baseline)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_GENERATE_REPORT, (_event, runId: unknown) => service!.generateReport(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_CANCEL_REPORT, (_event, runId: unknown) => service!.cancelReport(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_EVIDENCE, (_event, runId: unknown) => service!.evidence(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_RETRY_READING, (_event, runId: unknown, candidateId: unknown) => service!.retryReading(id(runId), id(candidateId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_RESUME, (_event, runId: unknown) => service!.resume(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_SAVE_EXCERPT, (_event, runId: unknown, candidateId: unknown) => service!.saveExcerpt(id(runId), id(candidateId)));
  ipcMain.handle(
    IPC_CHANNELS.RESEARCH_ENQUEUE_CANDIDATES,
    (_event, runId: unknown, candidateIds: unknown) => {
      if (!Array.isArray(candidateIds) || candidateIds.length === 0 || candidateIds.length > 60 || candidateIds.some((entry) => typeof entry !== "string" || !entry)) {
        throw new Error("候选列表不合法");
      }
      return service!.enqueueCandidates(id(runId), candidateIds);
    },
  );
  ipcMain.handle(IPC_CHANNELS.RESEARCH_SAVE_TO_KNOWLEDGE, (_event, runId: unknown) => service!.saveReportToKnowledge(id(runId)));
}

export async function shutdownResearch(): Promise<void> { await service?.shutdown(); }
