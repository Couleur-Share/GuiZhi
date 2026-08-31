import type {
  PlatformCapturePlatform,
  PlatformDiscoveryItem,
} from "./platform-capture";

export const DISCOVERY_INTERVALS = [360, 720, 1440, 4320, 10080] as const;
export type DiscoveryIntervalMinutes = (typeof DISCOVERY_INTERVALS)[number];
export type DiscoveryMode = "creator" | "keyword";
export type DiscoveryViewState =
  | "ready"
  | "running"
  | "login_required"
  | "backoff"
  | "paused";

export interface DiscoveryView {
  id: string;
  name: string;
  platform: PlatformCapturePlatform;
  mode: DiscoveryMode;
  query: string;
  intervalMinutes: DiscoveryIntervalMinutes;
  enabled: boolean;
  lastRunAt: number | null;
  nextRunAt: number | null;
  state: DiscoveryViewState;
  createdAt: number;
  updatedAt: number;
}

export interface SaveDiscoveryViewInput {
  id?: string;
  name: string;
  platform: PlatformCapturePlatform;
  mode: DiscoveryMode;
  query: string;
  intervalMinutes?: DiscoveryIntervalMinutes;
  enabled?: boolean;
}

export type DiscoveryCandidateState = "new" | "dismissed" | "imported";

export interface DiscoveryCandidate {
  viewId: string;
  externalId: string;
  item: PlatformDiscoveryItem;
  state: DiscoveryCandidateState;
  contentHash: string | null;
  firstSeenAt: number;
  lastSeenAt: number;
}

export type DiscoveryRunState = "running" | "completed" | "failed" | "canceled";

export interface DiscoveryRun {
  id: string;
  viewId: string;
  state: DiscoveryRunState;
  cursor: string | null;
  pagesScanned: number;
  candidatesFound: number;
  error: string | null;
  startedAt: number;
  finishedAt: number | null;
  updatedAt: number;
}

export interface DiscoveryViewDetail {
  view: DiscoveryView;
  candidates: DiscoveryCandidate[];
  runs: DiscoveryRun[];
}

export interface DiscoveryRunResult {
  view: DiscoveryView;
  run: DiscoveryRun;
  newCandidates: number;
}

export function isDiscoveryInterval(
  value: unknown,
): value is DiscoveryIntervalMinutes {
  return DISCOVERY_INTERVALS.includes(value as DiscoveryIntervalMinutes);
}
