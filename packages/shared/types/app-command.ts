/**
 * 应用级命令：托盘菜单 / 全局快捷键通过主进程发给渲染进程的动作。
 */
export type AppCommand =
  | { type: "item:new" }
  | { type: "settings:open" }
  | { type: "updater:open" };
