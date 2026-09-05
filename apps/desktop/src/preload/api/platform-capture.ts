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
  DiscoveryView,
  DiscoveryViewDetail,
  DiscoveryRunResult,
  SaveDiscoveryViewInput,
  DiscoveryCandidateState,
} from "@guizhi/shared/types";

export const platformCaptureApi = {
  getStatuses: (): Promise<PlatformSessionStatus[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_STATUS),
  login: (
    platform: PlatformCapturePlatform,
    forceRelogin = false,
    searchKeyword?: string,
  ): Promise<PlatformSessionStatus> =>
    ipcRenderer.invoke(IPC_CHANNELS.PLATFORM_CAPTURE_LOGIN, { platform, forceRelogin, ...(searchKeyword ? { searchKeyword } : {}) }),
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
  listDiscoveryViews: (): Promise<DiscoveryView[]> =>
    ipcRenderer.invoke(IPC_CHANNELS.DISCOVERY_VIEW_LIST),
  getDiscoveryView: (id: string): Promise<DiscoveryViewDetail | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.DISCOVERY_VIEW_GET, id),
  saveDiscoveryView: (input: SaveDiscoveryViewInput): Promise<DiscoveryView> =>
    ipcRenderer.invoke(IPC_CHANNELS.DISCOVERY_VIEW_SAVE, input),
  deleteDiscoveryView: (id: string): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.DISCOVERY_VIEW_DELETE, id),
  runDiscoveryView: (id: string): Promise<DiscoveryRunResult> =>
    ipcRenderer.invoke(IPC_CHANNELS.DISCOVERY_VIEW_RUN, id),
  resumeDiscoveryAfterLogin: (id: string): Promise<DiscoveryView | null> =>
    ipcRenderer.invoke(IPC_CHANNELS.DISCOVERY_VIEW_RESUME_LOGIN, id),
  setDiscoveryCandidateState: (
    platform: PlatformCapturePlatform,
    externalId: string,
    state: DiscoveryCandidateState,
  ): Promise<boolean> =>
    ipcRenderer.invoke(IPC_CHANNELS.DISCOVERY_CANDIDATE_SET_STATE, {
      platform,
      externalId,
      state,
    }),
};
