/**
 * 渲染进程的运行时安全边界。
 *
 * 归知会把导入的第三方网页转成 Markdown 再渲染，正文是不可信内容。
 * 渲染层挂了 rehype-sanitize，但那是唯一一道防线；这里补上纵深防御：
 *
 * - CSP：即使 sanitize 被绕过，脚本也无法执行
 * - will-navigate：preload 的 window.api 绑在 window 上、不区分来源，
 *   一旦主窗口被导航到远程页面，对方就拿到完整的 IPC 能力
 * - 权限处理器：Electron 默认放行多数权限请求，本地知识库一个都不需要
 */
import { SNAPSHOT_BRIDGE_HASH } from "./services/web-capture/snapshot-bridge";
import { shell } from "electron";
import type { Session, WebContents } from "electron";
import path from "path";
import { fileURLToPath } from "url";
import { handleExternalWindowOpen, isAllowedExternalUrl } from "./external-links";

export interface WindowSecurityOptions {
  /** 开发模式下 Vite dev server 的地址；生产为 null */
  devServerUrl: string | null;
  /** 生产构建的渲染产物目录；file:// 导航只允许停留在这里面 */
  rendererDir: string;
}

/**
 * 目标 URL 是否属于应用自身页面。
 *
 * 只有渲染进程自己的源可以在主窗口里导航，其余（含 http/https 外链）
 * 一律拦下并交给系统浏览器。
 *
 * file:// 必须限制在渲染产物目录内：preload 绑在 window 上、不区分来源，
 * 导航到磁盘上任意 HTML（例如导入进资产目录的文件）同样会交出 window.api。
 */
export function isInternalRendererUrl(
  targetUrl: string,
  devServerUrl: string | null,
  rendererDir: string,
): boolean {
  let parsed: URL;
  try {
    parsed = new URL(targetUrl);
  } catch {
    return false;
  }

  // 生产构建从 file:// 加载打包产物
  if (parsed.protocol === "file:") {
    try {
      const relative = path.relative(rendererDir, fileURLToPath(parsed));
      return !relative.startsWith("..") && !path.isAbsolute(relative);
    } catch {
      return false;
    }
  }

  if (!devServerUrl) {
    return false;
  }
  try {
    return parsed.origin === new URL(devServerUrl).origin;
  } catch {
    return false;
  }
}

/**
 * 内容安全策略。
 *
 * style-src 必须留 'unsafe-inline'：Tailwind 之外还有大量 React 内联 style。
 * img-src 保留 http/https：知识条目正文里的外链图片要能显示。
 * dev 额外放开 inline/eval 与 ws，否则 Vite HMR 与 React Refresh 起不来。
 */
export function buildContentSecurityPolicy(
  devServerUrl: string | null,
): string {
  const isDev = devServerUrl !== null;
  const scriptSrc = isDev
    ? "'self' 'unsafe-inline' 'unsafe-eval'"
    : `'self' 'sha256-${SNAPSHOT_BRIDGE_HASH}'`;
  const connectSrc = isDev ? `'self' ${devServerUrl} ws: wss: data:` : "'self' data:";

  return [
    "default-src 'self'",
    `script-src ${scriptSrc}`,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob: local-image: local-video: https: http:",
    "media-src 'self' data: blob: local-image: local-video:",
    "font-src 'self' data:",
    `connect-src ${connectSrc}`,
    "object-src 'none'",
    "frame-src 'self'",
    "worker-src 'self' blob:",
    "base-uri 'self'",
    "form-action 'none'",
  ].join("; ");
}

/** 给单个 webContents 装上导航与开窗拦截 */
export function applyWebContentsSecurity(
  contents: WebContents,
  options: WindowSecurityOptions,
): void {
  contents.setWindowOpenHandler(handleExternalWindowOpen);

  contents.on("will-navigate", (event, targetUrl) => {
    if (
      isInternalRendererUrl(targetUrl, options.devServerUrl, options.rendererDir)
    ) {
      return;
    }
    event.preventDefault();
    if (isAllowedExternalUrl(targetUrl)) {
      void Promise.resolve(shell.openExternal(targetUrl)).catch(
        (error: unknown) => {
          console.warn(
            "Failed to open external URL:",
            error instanceof Error ? error.message : error,
          );
        },
      );
      return;
    }
    console.warn("Blocked in-window navigation:", targetUrl);
  });

  // 子框架导航同样不允许把窗口带走
  contents.on("will-frame-navigate", (event) => {
    if (!event.isMainFrame && event.url === "about:srcdoc") return;
    if (
      isInternalRendererUrl(event.url, options.devServerUrl, options.rendererDir)
    ) {
      return;
    }
    event.preventDefault();
    console.warn("Blocked frame navigation:", event.url);
  });
}

/** 给 session 装上 CSP 响应头与权限拒绝策略 */
export function applySessionSecurity(
  targetSession: Session,
  options: WindowSecurityOptions,
): void {
  const csp = buildContentSecurityPolicy(options.devServerUrl);

  targetSession.webRequest.onHeadersReceived((details, callback) => {
    callback({
      responseHeaders: {
        ...details.responseHeaders,
        "Content-Security-Policy": [csp],
      },
    });
  });

  // 本地知识库不需要摄像头 / 麦克风 / 定位 / 通知等任何 web 权限
  targetSession.setPermissionRequestHandler((_contents, _permission, callback) => {
    callback(false);
  });
  targetSession.setPermissionCheckHandler(() => false);
}
