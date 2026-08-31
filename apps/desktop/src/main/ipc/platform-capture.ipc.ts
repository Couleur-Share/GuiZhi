import { BrowserWindow, ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import {
  isDiscoveryInterval,
  isCommentLimit,
  isPlatformCapturePlatform,
  type CaptureCommentsInput,
  type DiscoverCreatorInput,
  type PlatformCapturePlatform,
  type PlatformDiscoveryPage,
  type SearchPlatformInput,
  type SaveDiscoveryViewInput,
} from "@guizhi/shared/types";
import {
  detectPlatformCapturePlatform,
  isCreatorProfileUrl,
} from "@guizhi/shared/utils/platform-capture";
import { SourceCommentDB } from "@guizhi/db";
import type Database from "../database/sqlite";
import { getBrowserCaptureService } from "../services/platform-capture/browser-capture";
import { captureSourceComments } from "../services/platform-capture/source-comments";
import { normalizeUrl } from "../services/import/url-normalize";
import { readNetworkProxySetting } from "../services/import/import-service";
import { BrowserDiscoveryCollector } from "../services/discovery/browser-discovery-collector";
import {
  DiscoveryService,
  type DiscoveryServiceOptions,
} from "../services/discovery/discovery-service";
import type { BackgroundJobRuntime } from "../services/background-jobs";
import { readBooleanSetting } from "../settings/settings-readers";

function requirePlatform(value: unknown): PlatformCapturePlatform {
  if (!isPlatformCapturePlatform(value)) throw new Error("不支持的平台");
  return value;
}

function normalizedCursor(value: unknown): string | null {
  if (value == null || value === "") return null;
  const parsed = Number.parseInt(String(value), 10);
  if (!Number.isInteger(parsed) || parsed < 0 || parsed > 100)
    throw new Error("无效的分页游标");
  return String(parsed);
}

function withImportedItems(
  db: Database.Database,
  page: PlatformDiscoveryPage,
): PlatformDiscoveryPage {
  return {
    ...page,
    items: page.items.map((item) => {
      const normalized = normalizeUrl(item.url);
      const row = db.get(
        `SELECT s.item_id AS item_id FROM source_records s
         JOIN knowledge_items i ON i.id = s.item_id
         WHERE s.normalized_uri = ? AND i.deleted_at IS NULL
         ORDER BY s.captured_at DESC LIMIT 1`,
        normalized,
      ) as { item_id: string } | undefined;
      return row ? { ...item, importedItemId: row.item_id } : item;
    }),
  };
}

export interface PlatformCaptureIpcOptions {
  backgroundJobs: BackgroundJobRuntime;
  discoveryOptions?: DiscoveryServiceOptions;
}

function requireDiscoveryInput(
  db: Database.Database,
  raw: unknown,
): SaveDiscoveryViewInput {
  if (!raw || typeof raw !== "object") throw new Error("发现视图参数不合法");
  const input = raw as SaveDiscoveryViewInput;
  if (!isPlatformCapturePlatform(input.platform)) throw new Error("不支持的平台");
  if (input.mode !== "creator" && input.mode !== "keyword") throw new Error("发现模式不合法");
  const name = typeof input.name === "string" ? input.name.trim().slice(0, 80) : "";
  const query = typeof input.query === "string" ? input.query.trim().slice(0, 500) : "";
  if (!name || !query) throw new Error("名称和查询内容不能为空");
  if (input.mode === "creator" && !isCreatorProfileUrl(input.platform, query)) {
    throw new Error("作者主页地址不合法");
  }
  if (input.intervalMinutes !== undefined && !isDiscoveryInterval(input.intervalMinutes)) {
    throw new Error("发现周期不合法");
  }
  if (input.enabled && readBooleanSetting(db, "backgroundTasksEnabled") !== true) {
    throw new Error("启用定时发现前，请先授权归知在系统后台运行");
  }
  return { ...input, name, query };
}

export function registerPlatformCaptureIPC(
  db: Database.Database,
  options: PlatformCaptureIpcOptions,
): void {
  const service = getBrowserCaptureService({
    getNetworkProxy: () => readNetworkProxySetting(db),
  });
  const comments = new SourceCommentDB(db);
  const discovery = new DiscoveryService(
    db,
    new BrowserDiscoveryCollector(service),
    options.backgroundJobs,
    options.discoveryOptions,
  );
  discovery.registerBackgroundHandler();

  ipcMain.handle(IPC_CHANNELS.PLATFORM_CAPTURE_STATUS, () =>
    service.getStatuses(),
  );
  ipcMain.handle(IPC_CHANNELS.PLATFORM_CAPTURE_LOGIN, (event, raw) => {
    const record =
      raw && typeof raw === "object"
        ? (raw as { platform?: unknown; forceRelogin?: unknown })
        : null;
    const platform = requirePlatform(record ? record.platform : raw);
    return service.login(
      platform,
      record?.forceRelogin === true,
      BrowserWindow.fromWebContents(event.sender),
    );
  });
  ipcMain.handle(
    IPC_CHANNELS.PLATFORM_CAPTURE_CANCEL_LOGIN,
    (_event, rawPlatform) =>
      service.cancelForPlatform("login", requirePlatform(rawPlatform)),
  );
  ipcMain.handle(IPC_CHANNELS.PLATFORM_CAPTURE_LOGOUT, (_event, rawPlatform) =>
    service.logout(requirePlatform(rawPlatform)),
  );
  ipcMain.handle(IPC_CHANNELS.PLATFORM_CAPTURE_CLEAR_ALL, () =>
    service.clearAllData(),
  );
  ipcMain.handle(
    IPC_CHANNELS.PLATFORM_CAPTURE_DISCOVER_CREATOR,
    async (_event, raw: DiscoverCreatorInput) => {
      const platform = requirePlatform(raw?.platform);
      if (
        typeof raw?.url !== "string" ||
        !isCreatorProfileUrl(platform, raw.url)
      ) {
        throw new Error("作者主页地址不合法");
      }
      const page = await service.discoverCreator({
        platform,
        url: raw.url,
        cursor: normalizedCursor(raw.cursor),
        limit: 20,
      });
      return withImportedItems(db, page);
    },
  );
  ipcMain.handle(
    IPC_CHANNELS.PLATFORM_CAPTURE_SEARCH,
    async (_event, raw: SearchPlatformInput) => {
      const platform = requirePlatform(raw?.platform);
      const keyword =
        typeof raw?.keyword === "string"
          ? raw.keyword.trim().slice(0, 100)
          : "";
      if (!keyword) throw new Error("搜索关键词不能为空");
      const page = await service.search({
        platform,
        keyword,
        cursor: normalizedCursor(raw.cursor),
        limit: 20,
      });
      return withImportedItems(db, page);
    },
  );
  ipcMain.handle(IPC_CHANNELS.PLATFORM_CAPTURE_CANCEL_DISCOVERY, () =>
    service.cancel("discovery"),
  );
  ipcMain.handle(
    IPC_CHANNELS.PLATFORM_CAPTURE_LIST_COMMENTS,
    (_event, itemId: string) => comments.list(String(itemId ?? "")),
  );
  ipcMain.handle(
    IPC_CHANNELS.PLATFORM_CAPTURE_REFRESH_COMMENTS,
    async (_event, raw: CaptureCommentsInput) => {
      if (!isCommentLimit(raw?.limit) || Number(raw?.limit) === 0) {
        throw new Error("无效的评论数量");
      }
      const itemId = typeof raw?.itemId === "string" ? raw.itemId : "";
      const source = db.get(
        `SELECT source_uri FROM source_records
         WHERE item_id = ? AND platform IN ('xiaohongshu','douyin')
         ORDER BY captured_at DESC LIMIT 1`,
        itemId,
      ) as { source_uri: string | null } | undefined;
      const platform = source?.source_uri
        ? detectPlatformCapturePlatform(source.source_uri)
        : null;
      if (!source?.source_uri || !platform)
        throw new Error("该条目没有可补采评论的平台来源");
      const captured = await captureSourceComments(
        service,
        platform,
        source.source_uri,
        raw.limit,
      );
      comments.upsertMany(
        captured.map((comment) => ({
          ...comment,
          itemId,
          platform,
        })),
      );
      return comments.list(itemId);
    },
  );

  ipcMain.handle(IPC_CHANNELS.DISCOVERY_VIEW_LIST, () => discovery.listViews());
  ipcMain.handle(IPC_CHANNELS.DISCOVERY_VIEW_GET, (_event, id: string) =>
    discovery.getDetail(String(id ?? "")),
  );
  ipcMain.handle(IPC_CHANNELS.DISCOVERY_VIEW_SAVE, (_event, raw: unknown) =>
    discovery.save(requireDiscoveryInput(db, raw)),
  );
  ipcMain.handle(IPC_CHANNELS.DISCOVERY_VIEW_DELETE, (_event, id: string) =>
    discovery.delete(String(id ?? "")),
  );
  ipcMain.handle(IPC_CHANNELS.DISCOVERY_VIEW_RUN, (_event, id: string) =>
    discovery.run(String(id ?? "")),
  );
  ipcMain.handle(IPC_CHANNELS.DISCOVERY_VIEW_RESUME_LOGIN, (_event, id: string) =>
    discovery.resumeAfterLogin(String(id ?? "")),
  );
  ipcMain.handle(
    IPC_CHANNELS.DISCOVERY_CANDIDATE_SET_STATE,
    (_event, raw: { platform?: unknown; externalId?: unknown; state?: unknown }) => {
      const platform = requirePlatform(raw?.platform);
      const externalId = typeof raw?.externalId === "string" ? raw.externalId : "";
      if (!externalId || !["new", "dismissed", "imported"].includes(String(raw?.state))) {
        throw new Error("候选状态参数不合法");
      }
      return discovery.setCandidateState(
        platform,
        externalId,
        raw.state as "new" | "dismissed" | "imported",
      );
    },
  );
}
