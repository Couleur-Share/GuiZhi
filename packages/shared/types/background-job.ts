/** 持久化后台任务契约。主进程持有调度权，Renderer 只领取需要其执行的任务。 */
export const BACKGROUND_JOB_KINDS = [
  "backup",
  "platform-discovery",
  "wiki-compile",
  "semantic-index",
] as const;

export type BackgroundJobKind = (typeof BACKGROUND_JOB_KINDS)[number];

export const BACKGROUND_JOB_STATES = [
  "scheduled",
  "running",
  "retry_wait",
  "paused",
  "succeeded",
  "failed",
] as const;

export type BackgroundJobState = (typeof BACKGROUND_JOB_STATES)[number];

export interface BackgroundJob {
  id: string;
  kind: BackgroundJobKind;
  scopeId: string;
  state: BackgroundJobState;
  payload: Record<string, unknown>;
  intervalMinutes: number | null;
  nextRunAt: number | null;
  attempt: number;
  leaseOwner: string | null;
  leaseUntil: number | null;
  lastError: string | null;
  lastSuccessAt: number | null;
  createdAt: number;
  updatedAt: number;
}

export interface BackgroundJobScheduleInput {
  kind: BackgroundJobKind;
  scopeId?: string;
  payload?: Record<string, unknown>;
  intervalMinutes?: number | null;
  nextRunAt?: number | null;
  enabled?: boolean;
}

export interface BackgroundJobSyncInput {
  wikiEnabled: boolean;
  semanticEnabled: boolean;
}

export interface BackgroundJobResult {
  success: boolean;
  error?: string;
}

export interface BackgroundJobFailureInput {
  id: string;
  error: string;
  pause?: boolean;
}
