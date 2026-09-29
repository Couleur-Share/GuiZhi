// 内容列的最大宽度（vp）：手机上不起作用；折叠屏展开、平板与横屏下限制行长，避免列表被拉成一条难以阅读的长带。
export const CONTENT_MAX_WIDTH: number = 640;

// 内容列底部为悬浮页签栏与提示条预留的空间（vp）。
export const CONTENT_END_OFFSET: number = 112;
// 多选栏比页签栏更高；撤销条叠在页签栏上方。二者出现时列表末尾要多留空间，最后一条才不会被挡住。
export const CONTENT_END_OFFSET_SELECTING: number = 156;
export const CONTENT_END_OFFSET_UNDO: number = 168;
