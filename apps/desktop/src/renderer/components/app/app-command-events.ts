/**
 * 应用级命令事件：托盘 / 全局快捷键触发的跨组件动作。
 */
export const APP_NEW_ITEM_EVENT = "shortcut:newItem";

export function dispatchNewItemCommand(): void {
  window.dispatchEvent(new CustomEvent(APP_NEW_ITEM_EVENT));
}
