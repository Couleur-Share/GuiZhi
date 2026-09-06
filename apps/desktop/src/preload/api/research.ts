import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  CreateResearchRunInput,
  ImportTask,
  ResearchEvidencePacket,
  ResearchRun,
  ResearchComparison,
  ResearchRunDetail,
} from "@guizhi/shared/types";

export const researchApi = {
  compare: (id: string, baselineId?: string): Promise<ResearchComparison> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_COMPARE, id, baselineId),
  baselines: (id: string): Promise<ResearchRun[]> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_BASELINES, id),
  setBaseline: (id: string, baselineId: string): Promise<void> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_SET_BASELINE, id, baselineId),
  generateReport: (id: string): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_GENERATE_REPORT, id),
  cancelReport: (id: string): Promise<void> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_CANCEL_REPORT, id),
  evidence: (id: string): Promise<ResearchEvidencePacket | null> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_EVIDENCE, id),
  retryReading: (id: string, candidateId: string): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_RETRY_READING, id, candidateId),
  resume: (id: string): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_RESUME, id),
  saveExcerpt: (id: string, candidateId: string): Promise<{ itemId: string; updated: boolean }> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_SAVE_EXCERPT, id, candidateId),
  list: (): Promise<ResearchRun[]> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_LIST),
  get: (id: string): Promise<ResearchRunDetail | null> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_GET, id),
  create: (input: CreateResearchRunInput): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_CREATE, input),
  cancel: (id: string): Promise<boolean> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_CANCEL, id),
  delete: (id: string): Promise<boolean> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_DELETE, id),
  clone: (id: string, replan = false): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_CLONE, id, replan),
  verifyAndRetrySource: (id: string, source: "xiaohongshu" | "douyin"): Promise<ResearchRun> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_VERIFY_AND_RETRY_SOURCE, id, source),
  enqueueCandidates: (id: string, candidateIds: string[]): Promise<ImportTask[]> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_ENQUEUE_CANDIDATES, id, candidateIds),
  saveToKnowledge: (id: string): Promise<{ itemId: string; updated: boolean }> => ipcRenderer.invoke(IPC_CHANNELS.RESEARCH_SAVE_TO_KNOWLEDGE, id),
};
