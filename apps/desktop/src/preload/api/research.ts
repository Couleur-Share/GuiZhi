import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  CreateResearchRunInput,
  ImportTask,
  ResearchEvidencePacket,
  ResearchRun,
  ResearchRunDetail,
} from "@guizhi/shared/types";

export const researchApi = {
  list: (): Promise<ResearchRun[]> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_LIST),
  get: (id: string): Promise<ResearchRunDetail | null> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_GET, id),
  create: (input: CreateResearchRunInput): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_CREATE, input),
  cancel: (id: string): Promise<boolean> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_CANCEL, id),
  delete: (id: string): Promise<boolean> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_DELETE, id),
  clone: (id: string): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_CLONE, id),
  beginReport: (id: string): Promise<ResearchEvidencePacket> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_BEGIN_REPORT, id),
  saveReport: (id: string, markdown: string, version: string): Promise<ResearchRunDetail> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_SAVE_REPORT, id, markdown, version),
  failReport: (id: string, error: string): Promise<void> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_FAIL_REPORT, id, error),
  enqueueCandidates: (id: string, candidateIds: string[]): Promise<ImportTask[]> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_ENQUEUE_CANDIDATES, id, candidateIds),
  saveToKnowledge: (id: string): Promise<{ itemId: string; updated: boolean }> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_SAVE_TO_KNOWLEDGE, id),
};
