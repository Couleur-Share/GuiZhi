import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";

export interface LegacyDetectResult {
  path: string;
  itemCount: number;
}

export interface LegacyMigrationStats {
  collections: number;
  tags: number;
  items: number;
  itemTags: number;
  sources: number;
  wikiPages: number;
  wikiLinks: number;
  wikiSources: number;
  wikiIngestions: number;
}

export const migrationApi = {
  detectLegacy: (): Promise<LegacyDetectResult | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.MIGRATION_DETECT_LEGACY),
  runLegacy: (sourcePath?: string): Promise<LegacyMigrationStats> =>
    ipcRenderer.invoke(IPC_CHANNELS.MIGRATION_RUN_LEGACY, sourcePath),
};
