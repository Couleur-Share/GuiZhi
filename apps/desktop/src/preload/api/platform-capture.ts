import { ipcRenderer } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type {
  CaptureCommentsInput,
  DiscoverCreatorInput,
  PlatformCapturePlatform,
  PlatformDiscoveryPage,
  PlatformSessionStatus,
  SearchPlatformInput,
  SourceComment,
} from "@guizhi/shared/types";

export const platformCaptureApi = {
  getStatuses: (): Promise<PlatformSessionStatus[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_STATUS),
  login: (
    platform: PlatformCapturePlatform,
    forceRelogin = false,
  ): Promise<PlatformSessionStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_LOGIN, { platform, forceRelogin }),
  cancelLogin: (platform: PlatformCapturePlatform): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_CANCEL_LOGIN, platform),
  logout: (platform: PlatformCapturePlatform): Promise<void> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_LOGOUT, platform),
  clearAllData: (): Promise<void> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_CLEAR_ALL),
  discoverCreator: (input: DiscoverCreatorInput): Promise<PlatformDiscoveryPage> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_DISCOVER_CREATOR, input),
  search: (input: SearchPlatformInput): Promise<PlatformDiscoveryPage> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_SEARCH, input),
  cancelDiscovery: (): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_CANCEL_DISCOVERY),
  listComments: (itemId: string): Promise<SourceComment[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_LIST_COMMENTS, itemId),
  refreshComments: (input: CaptureCommentsInput): Promise<SourceComment[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_REFRESH_COMMENTS, input),
};
