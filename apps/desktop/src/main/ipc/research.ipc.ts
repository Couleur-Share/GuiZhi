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

let service: ResearchService | null = null;

function validCreateInput(value: unknown): value is CreateResearchRunInput {
  if (!value || typeof value !== "object") return false;
  const input = value as CreateResearchRunInput;
  return typeof input.topic === "string" && input.topic.trim().length > 0 && input.topic.trim().length <= 100
    && isResearchDayRange(input.dayRange)
    && (input.depth === "quick" || input.depth === "deep")
    && Array.isArray(input.sources) && input.sources.length > 0
    && input.sources.length <= 3 && input.sources.every(isResearchSource)
    && new Set(input.sources).size === input.sources.length;
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
  service = new ResearchService(db, {
    onChanged: broadcast,
    enqueueImports: (inputs) => getRegisteredImportService().queue.enqueue(inputs),
  });
  ipcMain.handle(IPC_CHANNELS.RESEARCH_LIST, () => service!.list());
  ipcMain.handle(IPC_CHANNELS.RESEARCH_GET, (_event, runId: unknown) => service!.getDetail(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_CREATE, (_event, input: unknown) => {
    if (!validCreateInput(input)) throw new Error("研究参数不合法");
    return service!.createAndRun({ ...input, topic: input.topic.trim(), sources: [...input.sources] });
  });
  ipcMain.handle(IPC_CHANNELS.RESEARCH_CANCEL, (_event, runId: unknown) => service!.cancel(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_DELETE, (_event, runId: unknown) => service!.delete(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_CLONE, (_event, runId: unknown) => service!.cloneAndRun(id(runId)));
  ipcMain.handle(IPC_CHANNELS.RESEARCH_BEGIN_REPORT, (_event, runId: unknown) => service!.beginReport(id(runId)));
  ipcMain.handle(
    IPC_CHANNELS.RESEARCH_SAVE_REPORT,
    (_event, runId: unknown, markdown: unknown, version: unknown) => {
      if (typeof markdown !== "string" || !markdown.trim() || typeof version !== "string" || !version.trim()) {
        throw new Error("研究报告参数不合法");
      }
      return service!.saveReport(id(runId), markdown, version);
    },
  );
  ipcMain.handle(IPC_CHANNELS.RESEARCH_FAIL_REPORT, (_event, runId: unknown, error: unknown) => {
    service!.failReport(id(runId), typeof error === "string" && error.trim() ? error : "报告生成失败");
  });
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
