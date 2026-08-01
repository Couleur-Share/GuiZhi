import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import type {
  ConfigApplyResult,
  ConfigExportResult,
  ConfigReadResult,
  ConfigApplySelection,
} from "@guizhi/shared/types";

export interface ConfigExportRequest {
  settings: Record<string, unknown>;
  settingsVersion?: number;
  uiLayout?: Record<string, unknown>;
  includeSecrets: boolean;
  password?: string;
  includeUiLayout?: boolean;
  includeIllustrationStyles?: boolean;
  includeShortcuts?: boolean;
  includeMcpScope?: boolean;
}

export const configTransferApi = {
  export: (request: ConfigExportRequest): Promise<ConfigExportResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.CONFIG_EXPORT, request),
  /** 只读预览，不落地任何改动 */
  read: (): Promise<ConfigReadResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.CONFIG_READ),
  apply: (filePath: string, password?: string, selection?: ConfigApplySelection): Promise<ConfigApplyResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.CONFIG_APPLY, filePath, password, selection),
};
