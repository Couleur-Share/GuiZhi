import { screen } from "electron";
import type { BrowserWindow } from "electron";

import { isE2EEnabled } from "./e2e";

/** 屏幕外落点与最近显示器边缘之间再留一段，避免任务栏预览等边缘情况把窗口带出来 */
const OFFSCREEN_MARGIN_PX = 400;

interface DisplayLike {
  bounds: { x: number; y: number; width: number; height: number };
}

/**
 * 窗口是否走「屏幕外静默」：挪到所有显示器之外并以不激活的方式显示。
 *
 * 自动化截图与 e2e 默认走这条路——它们常在用户正干活时被拉起，正常 show()
 * 会抢走焦点、把用户手上的窗口顶掉。`GUIZHI_WINDOW_MODE` 两个方向都能显式指定：
 * `visible` 让 e2e 恢复可见（人工盯着跑时用），`offscreen` 则可用于 electron:dev。
 */
export function shouldPlaceWindowOffscreen(
  env: NodeJS.ProcessEnv = process.env,
): boolean {
  const mode = env.GUIZHI_WINDOW_MODE;
  if (mode === "offscreen") {
    return true;
  }
  if (mode === "visible") {
    return false;
  }
  return isE2EEnabled(env);
}

/**
 * 落在所有显示器左上角之外的位置。
 * 取全体显示器的最小边界再往外挪，多屏（副屏摆在主屏左侧时 bounds.x 为负）下同样有效；
 * 与 0 取小是兜底：拿不到显示器列表时 Math.min() 会返回 Infinity。
 */
export function resolveOffscreenPosition(
  displays: DisplayLike[],
  windowSize: { width: number; height: number },
): { x: number; y: number } {
  const minX = Math.min(0, ...displays.map((display) => display.bounds.x));
  const minY = Math.min(0, ...displays.map((display) => display.bounds.y));
  return {
    x: Math.round(minX - windowSize.width - OFFSCREEN_MARGIN_PX),
    y: Math.round(minY - windowSize.height - OFFSCREEN_MARGIN_PX),
  };
}

/**
 * 显示窗口但不打扰用户：挪出屏幕、不进任务栏、不抢焦点。
 *
 * 不能改用 hide()——窗口一旦不可见，Chromium 就停止合成，实测
 * page.screenshot() 与 locator.click() 双双超时（20s / 10s 全部失败），
 * 自动化截图会整个失效。窗口必须保持 visible，只是不出现在人眼前。
 * 透明度归零是第二道保险：Windows 在 DPI 变化或显示器热插拔时会把屏幕外的
 * 窗口拽回可见区域，那一下不该被用户看见。
 */
export function showWindowOffscreen(window: BrowserWindow): void {
  const [width, height] = window.getSize();
  const { x, y } = resolveOffscreenPosition(screen.getAllDisplays(), {
    width,
    height,
  });
  window.setSkipTaskbar(true);
  window.setOpacity(0);
  window.setPosition(x, y);
  window.showInactive();
}
