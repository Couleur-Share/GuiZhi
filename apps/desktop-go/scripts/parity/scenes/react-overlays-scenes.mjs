/**
 * 全局浮层（M3-6a）的 React 侧对照场景：命令面板（默认态、搜索命中态）、更新对话框「发现新版本」、首次使用设置清单、旧版数据迁移确认。
 *
 * 都是整窗对照（遮罩与面板压在 rail 与顶栏之上，背后是设置页的默认分区）。数据层与操作在 lib/react-overlays.mjs；
 * 夹具与 Viva 一侧（viva/parity/scenes/overlays.go）是同一份 JSON。
 * 命令面板的搜索框、设置清单的折叠钮都带着焦点（打开即聚焦或点击后留焦点）：搜索框的焦点是用户看到的状态，采集时保留（keepFocus）；
 * 其余对话框采集前清焦点（Viva 一侧同样 ParkAndBlur）。
 */
import {
  OVERLAYS_FIXTURE as FIX,
  leaveOverlays,
  openMigrationConfirm,
  openPalette,
  openSetupChecklist,
  openUpdateDialog,
} from "../lib/react-overlays.mjs";

function overlayScene(enter, { keepFocus = false } = {}) {
  return {
    module: "app",
    keepFocus,
    async enter({ win, session }) {
      await enter(win, session);
    },
    async leave({ session }) {
      await leaveOverlays(session);
    },
  };
}

export const OVERLAYS_SCENES = {
  // 命令面板，没输入：常用动作 + 设置分区的快捷入口，底部没有任何提示行。
  "app.window.overlays.palette-default": overlayScene((win, session) => openPalette(win, session), { keepFocus: true }),
  // 输入搜索词：动作命中、知识条目、Wiki、采集任务（一条完成、一条失败）分组出现。
  "app.window.overlays.palette-search": overlayScene((win, session) => openPalette(win, session, { query: FIX.palette.query }), { keepFocus: true }),
  // 更新对话框：顶栏的更新按钮、版本号、更新说明、「下载更新」。
  "app.window.overlays.update-available": overlayScene(openUpdateDialog),
  // 首次使用设置清单：文本模型卡片 + 展开的可选项。
  "app.window.overlays.setup-checklist": overlayScene((win, session) => openSetupChecklist(win, session, { optionalOpen: FIX.setup.optionalOpen })),
  // 旧版数据迁移确认：条目数、范围说明、「暂不迁移」「立即迁移」。
  "app.window.overlays.migration-confirm": overlayScene(openMigrationConfirm),
};
