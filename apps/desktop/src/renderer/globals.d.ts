import type { DatabaseBackup, ImportSkippedStats } from "./services/database-backup-format";
import type { API } from "../preload";

declare global {
  interface GuiZhiWebContext {
    mode: "self-hosted";
    origin: string;
    username?: string;
    registrationAllowed?: boolean;
    initialized?: boolean;
  }

  interface Window {
    api: API;
    __GUIZHI_WEB__?: boolean;
    __GUIZHI_WEB_CONTEXT__?: GuiZhiWebContext;
    __GUIZHI_WEB_LOGOUT__?: (() => Promise<void>) | (() => void);
    __GUIZHI_E2E_BACKUP__?: {
      exportDatabase: (options?: {
        skipVideoContent?: boolean;
        limitMedia?: boolean;
      }) => Promise<DatabaseBackup>;
      restoreFromBackup: (backup: DatabaseBackup) => Promise<ImportSkippedStats>;
    };
  }
}

export {};
