/**
 * 窗口宽度断点（逻辑像素）。渲染层的响应式样式（Tailwind 的 sm / md / lg / xl / 2xl 与 container）按**窗口**宽度判定，
 * 而岛的视口只有阅读区那么宽，媒体查询会得出不同的结果（实测：阅读区 592 px 宽时 `sm:px-10` 不生效，内边距比渲染层小 16 px）。
 * 所以岛的样式表在构建时把每个 `@media (min-width: Npx)` 改写成以 `<html data-win-ge="…">` 为条件（window-width-media.ts），
 * 页面按宿主给的窗口宽度写这个属性（theme.ts）。这里列的就是样式表里出现过的全部 min-width 断点；
 * 构建时遇到表外的值会报错，避免某个断点悄悄失效。
 */
export const WINDOW_BREAKPOINTS = [640, 768, 1024, 1280, 1400, 1536] as const;

/** 窗口宽度已达到的断点，空格分隔（data-win-ge 的取值，CSS 用 `~=` 匹配其中一项）。 */
export function windowBreakpointTokens(width: number): string {
  return WINDOW_BREAKPOINTS.filter((bp) => width >= bp).join(" ");
}