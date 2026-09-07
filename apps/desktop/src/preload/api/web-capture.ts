import { ipcRenderer } from "electron";
import { IPC_CHANNELS as C } from "@guizhi/shared/constants/ipc-channels";
import type {
  AdoptWebVersionInput,
  CreateCrawlJobInput,
  CrawlJob,
  CrawlPage,
  WebRuntimeStatus,
  WebSourceVersion,
  WebSnapshotView,
  ImportTask,
} from "@guizhi/shared/types";
export interface WebResponse<T> {
  ok: boolean;
  data?: T;
  error?: string;
}
const invoke = <T>(
  channel: string,
  ...args: unknown[]
): Promise<WebResponse<T>> => ipcRenderer.invoke(channel, ...args);
export const webCaptureApi = {
  snapshot: (itemId: string, versionId?: string) => invoke<WebSnapshotView>(C.WEB_SNAPSHOT,itemId,versionId),
  supplement: (ids: string[]) => invoke<ImportTask[]>(C.WEB_SNAPSHOT_ENQUEUE,ids),
  exportHtml: (itemId: string, versionId: string) => invoke<{canceled?:boolean;path?:string;incomplete?:boolean}>(C.WEB_SNAPSHOT_EXPORT,itemId,versionId),
  status: () => invoke<WebRuntimeStatus>(C.WEB_STATUS),
  repair: () => invoke<void>(C.WEB_REPAIR),
  create: (input: CreateCrawlJobInput) => invoke<string>(C.CRAWL_CREATE, input),
  list: () => invoke<CrawlJob[]>(C.CRAWL_LIST),
  get: (id: string) =>
    invoke<{ job: CrawlJob | null; pages: CrawlPage[] }>(C.CRAWL_GET, id),
  pause: (id: string) => invoke<void>(C.CRAWL_PAUSE, id),
  resume: (id: string) => invoke<void>(C.CRAWL_RESUME, id),
  cancel: (id: string) => invoke<void>(C.CRAWL_CANCEL, id),
  retry: (id: string) => invoke<void>(C.CRAWL_RETRY, id),
  versions: (id: string) =>
    invoke<{
      versions: WebSourceVersion[];
      content: string;
      title: string;
      contentHash: string;
      summaryStale: boolean;
      pendingVersion?: boolean;
    }>(C.WEB_VERSIONS, id),
  adopt: (input: AdoptWebVersionInput) => invoke<void>(C.WEB_ADOPT, input),
};
