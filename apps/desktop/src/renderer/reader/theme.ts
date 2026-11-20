import type { ReaderLang, ReaderTheme } from "./protocol";

/**
 * 把外观输入写到 <html> 上，做法与渲染层完全一致（settings-general-actions 的 setDocumentThemeColor / setDocumentFontSize /
 * 深色类，App.tsx 的 data-motion）：样式表（globals.css）据此算出全部语义令牌，岛里不另存一份颜色。
 */
export function applyTheme(
  root: HTMLElement,
  theme: ReaderTheme,
  lang: ReaderLang,
): void {
  root.classList.toggle("dark", theme.dark);
  root.style.setProperty("--theme-hue", String(theme.hue));
  root.style.setProperty("--theme-saturation", String(theme.saturation));
  root.style.setProperty("--base-font-size", `${theme.basePx}px`);
  root.dataset.motion = theme.motion;
  root.lang = lang === "en" ? "en" : "zh-CN";
}
