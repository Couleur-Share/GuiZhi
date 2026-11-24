/**
 * 显示设置「背景图」三个对照场景（M3-6c，台账 B-020）的 React 侧装配：把同源合成壁纸放进会话的图片目录，
 * 把背景图设置写进持久化状态并重载，再切到「显示设置」滚到底。
 *
 * 壁纸夹具与 Viva 一侧是同一个文件（viva/parity/testdata/wallpaper-fixture.png，由 scripts/parity/tools/make-wallpaper-fixture.mjs 生成）。
 * 渲染层经 local-image://<文件名> 读它，协议处理器服务的是 <userData>/data/assets/images/——会话的 userData 是一次性临时目录
 * （react-session.mjs），所以直接把文件拷进去最干净：不碰真实数据目录，也不经原生选图框。
 *
 * 背景图设置（backgroundImageFileName / Enabled / Opacity / Blur）只存在渲染层的持久化状态里（localStorage 的 guizhi-settings），
 * 不经主进程：写进去再 reload，渲染层启动时合并并生效（同 lib/react-settings-core.mjs 的 resetNetworkProxy）。
 * 渲染层的 hasBackgroundImage 只看 `typeof fileName === "string"` 与 enabled：场景离开时文件名置空串、enabled 置 false，
 * 持久化合并时空串归一成 undefined，后续场景回到无壁纸的普通模式。
 */
import fs from "node:fs";
import path from "node:path";
import { SETTLE_CSS, settle } from "./react-state.mjs";
import { openSettingsSection, scrollSettingsToEnd } from "./react-settings-nav.mjs";

const SETTINGS_KEY = "guizhi-settings";
const SETTINGS_VERSION = 3; // stores/settings.store.ts 的 persist version（同 react-state.mjs）

/** 夹具文件名（落在图片目录里的名字）与源文件路径。 */
export const WALLPAPER_FILE_NAME = "wallpaper-fixture.png";
const WALLPAPER_SOURCE = path.join(import.meta.dirname, "../../../viva/parity/testdata", WALLPAPER_FILE_NAME);

/** 把壁纸夹具拷进会话的图片目录（幂等：已存在则覆盖成同一份内容）。 */
export function installWallpaperFile(session) {
  const dir = path.join(session.userDataDir, "data", "assets", "images");
  fs.mkdirSync(dir, { recursive: true });
  fs.copyFileSync(WALLPAPER_SOURCE, path.join(dir, WALLPAPER_FILE_NAME));
}

/** 改写持久化里的背景图设置；reload 为真时重载（reload 后渲染层按它启动）。 */
async function writeBackground(session, patch, { reload = true } = {}) {
  await session.page.evaluate(
    ({ key, version, values }) => {
      const stored = JSON.parse(localStorage.getItem(key) || "null") ?? { state: {}, version };
      stored.version = version;
      stored.state = { ...stored.state, ...values };
      localStorage.setItem(key, JSON.stringify(stored));
    },
    { key: SETTINGS_KEY, version: SETTINGS_VERSION, values: patch },
  );
  if (!reload) return;
  await session.reload();
  await session.page.addStyleTag({ content: SETTLE_CSS });
  await settle(session.page);
}

/** 等页面上 want 张 local-image 图片都解码完成（WebView2 把 local-image://x 改写成 http://local-image.localhost/x，所以按子串匹配）（complete 且 naturalWidth > 0）；超时说明协议或文件有问题，抛错而不是对着空白采集。 */
async function waitWallpaperImages(win, want) {
  await win.waitForFunction(
    (n) => {
      const imgs = [...document.querySelectorAll('img[src*="local-image"]')];
      return imgs.length >= n && imgs.every((img) => img.complete && img.naturalWidth > 0);
    },
    want,
    { timeout: 15_000 },
  );
}

/**
 * 进入场景：放文件、写设置（enabled / opacity / blur）、重载，切到「显示设置」，等图片解码，滚到内容底。
 * 预览舞台里总有一张图；整窗壁纸启用时背景里再有一张（App.tsx 的 BackgroundImageBackdrop），共 1 或 2 张。
 */
export async function enterWallpaper(win, session, { enabled, opacity, blur }) {
  installWallpaperFile(session);
  await writeBackground(session, {
    backgroundImageFileName: WALLPAPER_FILE_NAME,
    backgroundImageEnabled: enabled,
    backgroundImageOpacity: opacity,
    backgroundImageBlur: blur,
  });
  await openSettingsSection(win, "appearance");
  await waitWallpaperImages(win, enabled ? 2 : 1);
  await scrollSettingsToEnd(win);
  await settle(win);
}

/**
 * 离开场景：把持久化里的背景图设置复位（文件名空串、enabled 关、可见度与虚化回默认）；会话里的夹具文件随一次性 userData 一起删除。
 * 不在这里 reload：场景标了 changesTheme，采集器紧接着会重新应用变体（写状态 + reload），复位的值在那次重载里生效。
 */
export async function leaveWallpaper(session) {
  await writeBackground(
    session,
    { backgroundImageFileName: "", backgroundImageEnabled: false, backgroundImageOpacity: 1, backgroundImageBlur: 0 },
    { reload: false },
  );
}
