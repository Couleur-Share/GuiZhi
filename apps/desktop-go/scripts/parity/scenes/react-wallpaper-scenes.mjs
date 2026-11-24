/**
 * 显示设置「背景图」三个整窗对照场景（M3-6c，台账 B-020）：设置页「显示设置」滚到底，背景图分区完整可见。
 *
 * 三个场景共用同一张合成壁纸夹具（lib/react-wallpaper.mjs），只在「是否启用 / 可见度 / 虚化」上不同：
 *   - wallpaper-disabled：已选图、未启用（可见度 0.7、虚化 0）——整窗是普通不透明模式，只有 16:9 预览舞台展示玻璃模式的壁纸与缩微外壳；
 *   - wallpaper-enabled：已启用（可见度 0.35、虚化 0）——整窗壁纸模式（左栏、设置侧栏、顶栏、内容区与卡片都是玻璃色）加预览舞台；
 *   - wallpaper-blurred：已启用并虚化（可见度 0.55、虚化 14px）——检验 blur(14px) + scale(1.03) 的高斯模糊。
 *
 * 取整窗（不设 region）：壁纸与玻璃面板要在整个窗口里一起比。场景改了外观（壁纸模式的令牌）与持久化设置，
 * 所以标 changesTheme：采集后把变体的外观恢复回来；leave 再把背景图设置复位，保证后面的场景不受污染。
 */
import { enterWallpaper, leaveWallpaper } from "../lib/react-wallpaper.mjs";

function wallpaperScene(settings) {
  return {
    module: "settings",
    changesTheme: true,
    async enter({ win, session }) {
      await enterWallpaper(win, session, settings);
    },
    async leave({ session }) {
      await leaveWallpaper(session);
    },
  };
}

export const WALLPAPER_SCENES = {
  "settings.appearance.wallpaper-disabled": wallpaperScene({ enabled: false, opacity: 0.7, blur: 0 }),
  "settings.appearance.wallpaper-enabled": wallpaperScene({ enabled: true, opacity: 0.35, blur: 0 }),
  "settings.appearance.wallpaper-blurred": wallpaperScene({ enabled: true, opacity: 0.55, blur: 14 }),
};
