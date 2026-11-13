import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type {
  CaptureCommentsInput,
  PlatformCapturePlatform,
  PlatformSessionStatus,
  SourceComment,
} from "@guizhi/shared/types";

export const platformCaptureApi = {
  getStatuses: (): Promise<PlatformSessionStatus[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_STATUS),
  login: (
    platform: PlatformCapturePlatform,
    forceRelogin = false,
    searchKeyword?: string,
    verificationUrl?: string,
  ): Promise<PlatformSessionStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_LOGIN, { platform, forceRelogin, ...(searchKeyword ? { searchKeyword } : {}), ...(verificationUrl ? { verificationUrl } : {}) }),
  cancelLogin: (platform: PlatformCapturePlatform): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_CANCEL_LOGIN, platform),
  logout: (platform: PlatformCapturePlatform): Promise<void> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_LOGOUT, platform),
  clearAllData: (): Promise<void> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_CLEAR_ALL),
  listComments: (itemId: string): Promise<SourceComment[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_LIST_COMMENTS, itemId),
  refreshComments: (input: CaptureCommentsInput): Promise<SourceComment[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_REFRESH_COMMENTS, input),
};
