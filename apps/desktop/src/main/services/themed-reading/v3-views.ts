import { BrowserWindow, WebContentsView, ipcMain, session } from "electron";
import type { WebContents, IpcMainEvent } from "electron";
import { randomUUID } from "node:crypto";
import path from "node:path";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import type {
  ReadingViewBounds,
  ReadingViewCommand,
  ReadingViewEvent,
} from "@guizhi/shared/types/reading-page-v3";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { v3InnerDocument, v3WrapperDocument } from "./v3-document";
import { readThemeAsset } from "./assets";
import { retainAssetFiles } from "../asset-cleanup";

interface ViewEntry {
  id: string;
  owner: WebContents;
  view: WebContentsView;
  page: ThemedReadingVersion;
  preview: boolean;
  enabled: boolean;
  ready: boolean;
  timer?: ReturnType<typeof setInterval>;
  nonce?: string;
  sentAt: number;
  receive: (event: IpcMainEvent, data: any) => void;
  fault?: string;
  onOwnerDestroyed?: () => void;
  onOwnerNavigation?: (
    details: Electron.Event<Electron.WebContentsDidStartNavigationEventParams>,
  ) => void;
  rejectLoad?: (error: Error) => void;
  releaseAssets: () => void;
}
const views = new Map<string, ViewEntry>();
const allowedCommands = new Set([
  "appearance",
  "find",
  "anchor",
  "scroll",
  "interaction",
]);
export function validateReadingViewCommand(command: ReadingViewCommand): void {
  if (!command || !allowedCommands.has(command.type))
    throw new Error("阅读命令无效");
  if (
    command.type === "appearance" &&
    (!["light", "dark"].includes(command.theme) ||
      !Number.isFinite(command.fontSize) ||
      command.fontSize < 12 ||
      command.fontSize > 32 ||
      (command.fontFamily !== undefined &&
        (typeof command.fontFamily !== "string" ||
          command.fontFamily.length > 1000)))
  )
    throw new Error("阅读外观参数无效");
  if (
    command.type === "find" &&
    (typeof command.query !== "string" ||
      command.query.length > 1000 ||
      (command.requestId !== undefined &&
        (typeof command.requestId !== "string" ||
          !/^[\w-]{1,80}$/.test(command.requestId))) ||
      !Number.isSafeInteger(command.index))
  )
    throw new Error("阅读查找参数无效");
  if (
    command.type === "anchor" &&
    (typeof command.id !== "string" ||
      !/^[a-zA-Z][\w-]{0,199}$/.test(command.id))
  )
    throw new Error("阅读章节无效");
  if (
    command.type === "scroll" &&
    (!Number.isFinite(command.top) || command.top < 0 || command.top > 2000000)
  )
    throw new Error("阅读位置无效");
  if (command.type === "interaction" && typeof command.enabled !== "boolean")
    throw new Error("阅读交互开关无效");
}
function getView(owner: WebContents, id: string) {
  const e = views.get(id);
  if (!e || e.owner !== owner)
    throw new Error("阅读视图已失效或不属于当前窗口");
  return e;
}
function emit(e: ViewEntry, type: ReadingViewEvent["type"], value?: unknown) {
  if (!e.owner.isDestroyed())
    e.owner.send(IPC_CHANNELS.THEMED_READING_VIEW_EVENT, {
      viewId: e.id,
      type,
      value,
    });
}
function stopEntry(e: ViewEntry) {
  e.releaseAssets();
  clearInterval(e.timer);
  ipcMain.removeListener(IPC_CHANNELS.READING_VIEW_BRIDGE, e.receive);
  if (e.onOwnerDestroyed)
    e.owner.removeListener("destroyed", e.onOwnerDestroyed);
  if (e.onOwnerNavigation)
    e.owner.removeListener("did-start-navigation", e.onOwnerNavigation);
  const parent = BrowserWindow.fromWebContents(e.owner);
  if (parent && !parent.isDestroyed())
    parent.contentView.removeChildView(e.view);
  if (!e.view.webContents.isDestroyed()) {
    const ses = e.view.webContents.session;
    ses.protocol.unhandle("guizhi-reading");
    ses.webRequest.onBeforeRequest(null);
    e.view.webContents.close();
  }
}
export function destroyReadingView(owner: WebContents, id: string) {
  const e = getView(owner, id);
  views.delete(id);
  stopEntry(e);
}
export function closeReadingViews() {
  for (const e of views.values()) stopEntry(e);
  views.clear();
}
function fault(e: ViewEntry, message: string) {
  if (e.fault) return;
  e.fault = message;
  e.rejectLoad?.(new Error(message));
  clearInterval(e.timer);
  e.view.setVisible(false);
  BrowserWindow.fromWebContents(e.owner)?.contentView.removeChildView(e.view);
  emit(e, "fault", { message });
  if (!e.view.webContents.isDestroyed()) {
    const pid = e.view.webContents.getOSProcessId();
    if (pid && pid !== e.owner.getOSProcessId())
      e.view.webContents.forcefullyCrashRenderer();
  }
}
export async function createReadingView(
  owner: WebContents,
  page: ThemedReadingVersion,
  preview = false,
  enabled = true,
): Promise<string> {
  const parent = BrowserWindow.fromWebContents(owner);
  if (!parent || parent.isDestroyed()) throw new Error("阅读宿主窗口不存在");
  if ([...views.values()].filter((e) => e.owner === owner).length >= 3)
    throw new Error("阅读视图数量超过限制");
  const id = randomUUID(),
    ticket = randomUUID(),
    partition = `reading-view-${id}`;
  const ses = session.fromPartition(partition, { cache: false });
  const view = new WebContentsView({
    webPreferences: {
      session: ses,
      preload: path.join(__dirname, "../reading-view-preload/reading-view.js"),
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      nodeIntegrationInSubFrames: false,
      webSecurity: true,
      backgroundThrottling: false,
    },
  });
  view.setBounds({ x: 0, y: 0, width: 1, height: 1 });
  view.setVisible(false);
  const e: ViewEntry = {
    id,
    owner,
    view,
    page,
    preview,
    enabled: enabled && !preview,
    ready: false,
    sentAt: 0,
    receive: () => undefined,
    releaseAssets: retainAssetFiles(
      page.assets.flatMap((a) => (a.fileName ? [a.fileName] : [])),
    ),
  };
  views.set(id, e);
  const url = `guizhi-reading://${ticket}/index.html`;
  const assetMap = Object.fromEntries(
    page.assets
      .filter((a) => a.status === "ready")
      .map((a) => [
        a.id,
        `guizhi-reading://${ticket}/asset/${encodeURIComponent(a.id)}`,
      ]),
  );
  const document = () =>
    v3WrapperDocument(v3InnerDocument(page, assetMap, e.enabled, preview));
  ses.protocol.handle("guizhi-reading", async (request) => {
    if (!views.has(id)) return new Response("", { status: 410 });
    const u = new URL(request.url);
    if (u.host !== ticket || request.method !== "GET" || u.search)
      return new Response("", { status: 403 });
    if (u.pathname === "/index.html")
      return new Response(document(), {
        headers: {
          "Content-Type": "text/html; charset=utf-8",
          "Cache-Control": "no-store",
        },
      });
    const asset = page.assets.find(
      (a) =>
        u.pathname === `/asset/${encodeURIComponent(a.id)}` &&
        a.status === "ready",
    );
    if (!asset) return new Response("", { status: 404 });
    try {
      const { data, mime } = await readThemeAsset(asset);
      return new Response(new Uint8Array(data), {
        headers: { "Content-Type": mime, "Cache-Control": "no-store" },
      });
    } catch {
      return new Response("", { status: 404 });
    }
  });
  ses.webRequest.onBeforeRequest((details, callback) =>
    callback({
      cancel: !details.url.startsWith(`guizhi-reading://${ticket}/`),
    }),
  );
  ses.setPermissionRequestHandler((_w, _p, cb) => cb(false));
  ses.setPermissionCheckHandler(() => false);
  ses.on("will-download", (event) => event.preventDefault());
  const wc = view.webContents;
  wc.setWindowOpenHandler(() => ({ action: "deny" }));
  wc.on("will-navigate", (event) => event.preventDefault());
  wc.on("will-redirect", (event) => event.preventDefault());
  wc.on("will-frame-navigate", (event) => {
    if (event.url !== "about:srcdoc") event.preventDefault();
  });
  wc.on("render-process-gone", () =>
    fault(e, "阅读交互进程已退出，已保留静态正文"),
  );
  e.receive = (event, data) => {
    if (
      event.sender !== wc ||
      event.senderFrame !== wc.mainFrame ||
      !data ||
      typeof data.type !== "string"
    )
      return;
    if (data.type === "heartbeat") {
      if (data.value === e.nonce) {
        e.nonce = undefined;
        e.sentAt = 0;
      }
      return;
    }
    try {
      if (JSON.stringify(data).length > 64000) return;
    } catch {
      return;
    }
    if (data.type === "ready") {
      e.ready = true;
      emit(e, "ready");
    }
    const v = data.value;
    if (data.type === "key" && ["find", "escape"].includes(v))
      emit(e, "key", v);
    if (
      data.type === "layout" &&
      v &&
      Number.isFinite(v.top) &&
      v.top >= 0 &&
      v.top <= 2000000 &&
      Number.isFinite(v.height) &&
      v.height >= 0 &&
      v.height <= 2000000 &&
      Array.isArray(v.headings) &&
      v.headings.length <= 200 &&
      v.headings.every(
        (h) =>
          h &&
          typeof h.id === "string" &&
          /^[a-zA-Z][\w-]{0,199}$/.test(h.id) &&
          typeof h.text === "string" &&
          h.text.length <= 200,
      )
    )
      emit(e, "layout", v);
    if (
      data.type === "selection" &&
      typeof v?.text === "string" &&
      v.text.length <= 4000 &&
      [v.x, v.y].every((n) => Number.isFinite(n) && Math.abs(n) < 20000)
    )
      emit(e, "selection", v);
    if (
      data.type === "find" &&
      Number.isSafeInteger(v?.count) &&
      v.count >= 0 &&
      v.count <= 10000
    )
      emit(e, "find", v);
    if (
      data.type === "image" &&
      page.assets.some((a) => a.id === v?.id && a.status === "ready")
    )
      emit(e, "image", { id: v.id });
    if (data.type === "fault")
      fault(
        e,
        `${page.design?.scripts?.some((s) => s.id === data.value?.id) ? `交互 ${data.value.id} 运行失败` : "交互运行失败"}：${String(data.value?.message ?? "未知错误").slice(0, 1000)}`,
      );
  };
  ipcMain.on(IPC_CHANNELS.READING_VIEW_BRIDGE, e.receive);
  e.onOwnerDestroyed = () => {
    if (views.has(id)) {
      views.delete(id);
      stopEntry(e);
    }
  };
  owner.once("destroyed", e.onOwnerDestroyed);
  e.onOwnerNavigation = (details) => {
    if (details.isMainFrame && !details.isSameDocument) e.onOwnerDestroyed();
  };
  owner.on("did-start-navigation", e.onOwnerNavigation);
  try {
    // 先加载无脚本的受控空页，确认独立进程后才装载模型产物。
    await wc.loadURL("about:blank");
    if (!wc.getOSProcessId() || wc.getOSProcessId() === owner.getOSProcessId())
      throw new Error("阅读进程隔离检查失败");
    e.nonce = randomUUID();
    e.sentAt = Date.now();
    e.timer = setInterval(() => {
      if (wc.isDestroyed()) return;
      if (e.sentAt && Date.now() - e.sentAt > 5000) {
        fault(e, "阅读交互超过五秒未响应，已停止交互");
        return;
      }
      if (!e.nonce) {
        e.nonce = randomUUID();
        e.sentAt = Date.now();
      }
      wc.send(IPC_CHANNELS.READING_VIEW_COMMAND, {
        type: "heartbeat",
        nonce: e.nonce,
      });
    }, 250);
    // Electron 33 在 srcdoc 同步死循环被终止后可能不 settle loadURL；使用独立故障通道结束等待。
    const failed = new Promise<never>((_resolve, reject) => {
      e.rejectLoad = reject;
    });
    await Promise.race([wc.loadURL(url), failed]);
    e.rejectLoad = undefined;
    return id;
  } catch (error) {
    views.delete(id);
    stopEntry(e);
    throw error;
  }
}
export function updateReadingView(
  owner: WebContents,
  id: string,
  b: ReadingViewBounds,
) {
  const e = getView(owner, id);
  if (
    !b ||
    [b.x, b.y, b.width, b.height].some((n) => !Number.isFinite(n)) ||
    b.width < 0 ||
    b.height < 0 ||
    b.width > 20000 ||
    b.height > 20000 ||
    typeof b.visible !== "boolean"
  )
    throw new Error("阅读视图位置无效");
  const parent = BrowserWindow.fromWebContents(owner),
    [w, h] = parent.getContentSize();
  const x = Math.max(0, Math.min(w, Math.round(b.x))),
    y = Math.max(0, Math.min(h, Math.round(b.y)));
  e.view.setBounds({
    x,
    y,
    width: Math.max(1, Math.min(w - x, Math.round(b.width))),
    height: Math.max(1, Math.min(h - y, Math.round(b.height))),
  });
  const visible = b.visible && b.width > 0 && b.height > 0 && !e.fault;
  if (visible && !parent.contentView.children.includes(e.view))
    parent.contentView.addChildView(e.view);
  if (!visible && parent.contentView.children.includes(e.view))
    parent.contentView.removeChildView(e.view);
  e.view.setVisible(visible);
}
export async function commandReadingView(
  owner: WebContents,
  id: string,
  command: ReadingViewCommand,
) {
  const e = getView(owner, id);
  validateReadingViewCommand(command);
  if (command.type === "interaction") {
    if (e.fault) throw new Error("故障阅读实例已停止，请重新启用交互");
    e.enabled = command.enabled && !e.preview;
    e.ready = false;
    e.view.webContents.reload();
    return;
  }
  if (!e.fault)
    e.view.webContents.send(IPC_CHANNELS.READING_VIEW_COMMAND, command);
}

/** 只在发布前运行；数据库读取和备份检查不调用此函数。 */
export async function probeReadingPage(
  page: ThemedReadingVersion,
): Promise<string | undefined> {
  if (!page.design?.scripts?.some((s) => s.status !== "failed")) return;
  const host = new BrowserWindow({
    show: false,
    webPreferences: {
      sandbox: true,
      nodeIntegration: false,
      contextIsolation: true,
      partition: `reading-probe-${randomUUID()}`,
    },
  });
  let id: string;
  try {
    await host.webContents.loadURL("about:blank");
    id = await createReadingView(host.webContents, page);
    const entry = views.get(id);
    const start = Date.now();
    while (Date.now() - start < 6500) {
      if (entry.fault) return entry.fault;
      if (entry.ready && Date.now() - start > 750) return;
      await new Promise((r) => setTimeout(r, 100));
    }
    return "阅读交互初始化未完成";
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  } finally {
    if (id && views.has(id)) destroyReadingView(host.webContents, id);
    host.destroy();
  }
}
