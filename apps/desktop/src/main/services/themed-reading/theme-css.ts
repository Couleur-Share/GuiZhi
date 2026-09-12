export { cleanThemedCss, themedColorContrast } from "./css-safety";

/** 程序填入的正文始终保有正常布局、最小字号及用户主题对比度。 */
export const THEMED_READING_BASE_CSS = `
:root{color-scheme:light dark;--reader-font-size:16px;--reader-font-family:system-ui,sans-serif;--reader-surface:#faf8f3;--reader-text:#242321;--theme-surface:var(--reader-surface);--theme-text:var(--reader-text)}
@media(prefers-color-scheme:dark){:root{--reader-surface:#191b20;--reader-text:#eceef3}}
:root[data-theme="light"]{color-scheme:light;--reader-surface:#faf8f3;--reader-text:#242321}
:root[data-theme="dark"]{color-scheme:dark;--reader-surface:#191b20;--reader-text:#eceef3}
*{box-sizing:border-box;min-width:0;max-width:100%}html,body{margin:0;padding:0;overflow-wrap:anywhere}body{padding:24px;font:var(--reader-font-size)/1.8 var(--reader-font-family);background:var(--theme-surface);color:var(--theme-text)}
img,svg{max-width:100%;height:auto}svg{max-height:30rem}img{cursor:zoom-in}main{max-width:100%;margin:auto}pre{overflow-x:auto;white-space:pre-wrap}table{display:block;overflow-x:auto;border-collapse:collapse}td,th{padding:.5em;border:1px solid currentColor}a{color:inherit;text-decoration:underline}details>summary{cursor:pointer}p,ul,ol,blockquote,pre,table{margin-block:.7em}h1,h2,h3,h4,h5,h6{line-height:1.45}summary{min-height:24px}nav a{display:inline-block;margin:.25em .75em .25em 0}
@media(max-width:640px){body{padding:14px}.theme-layout,main,article,section{grid-template-columns:minmax(0,1fr)!important;flex-wrap:wrap!important}}
`;

// 页面留白由可信样式兜底，只作用于最外层内容，避免嵌套章节累计缩窄正文。
// AI 的动态 spacing 可能被安全清理移除；已保存页面与离线导出同样需要保留阅读边距。
export const THEMED_READING_CONTENT_GUARD_CSS = `
html,body{display:block!important;width:100%!important;min-width:0!important;max-width:100%!important;margin:0!important;box-sizing:border-box!important}html{padding:0!important;border:0!important;font-size:16px!important}body{font-size:var(--reader-font-size)!important;font-family:var(--reader-font-family)!important}
body>:is([data-source-layout],[data-source-block]){padding-inline:clamp(20px,4vw,48px)!important;padding-block:clamp(16px,3vw,36px)!important}
body>[data-source-layout]{margin-inline:auto!important}
[data-source-layout]{width:100%!important;min-width:min(100%,16rem)!important;flex-wrap:wrap!important;background:var(--theme-surface)!important;color:var(--theme-text)!important}
[data-source-layout],[data-source-block]{box-sizing:border-box!important;margin-inline:0!important;position:static!important;float:none!important;opacity:1!important;visibility:visible!important;transform:none!important;filter:none!important;clip:auto!important;clip-path:none!important;overflow:visible!important;height:auto!important;min-height:0!important;max-height:none!important;font-size:var(--reader-font-size)!important;letter-spacing:normal!important;word-spacing:normal!important}
nav,summary{color:var(--theme-text)!important;background:var(--theme-surface)!important}nav a{color:inherit!important}svg,[data-source-block] svg{max-height:30rem!important;max-width:100%!important;overflow:hidden!important}
[data-source-block]{display:block!important;width:100%!important;min-width:0!important;max-width:100%!important;min-height:auto!important;height:auto!important;max-height:none!important;overflow:visible!important;flex:1 1 16rem!important;font-size:var(--reader-font-size)!important;line-height:1.8!important;color:var(--theme-text)!important;background:var(--theme-surface)!important}
[data-source-block] *{position:static!important;float:none!important;opacity:1!important;visibility:visible!important;transform:none!important;filter:none!important;clip:auto!important;clip-path:none!important;max-height:none!important;height:auto!important;line-height:inherit!important;color:inherit!important;background:transparent!important;letter-spacing:normal!important;word-spacing:normal!important}
[data-source-block] :is(p,li,span,a,strong,b,em,i,td,th,code,blockquote,div){font-size:inherit!important}
[data-source-block] :is(p,blockquote,div,section,pre,table){display:block!important;width:100%!important;min-width:0!important;max-width:100%!important}
[data-source-block] :is(ul,ol){width:100%!important;min-width:0!important;max-width:100%!important;flex-wrap:wrap!important}
[data-source-block] :is(li,blockquote,pre,th,td){color:var(--theme-text)!important;background:var(--theme-surface)!important}
[data-source-block] :is(span,a,strong,b,em,i,code){display:inline!important}
[data-source-block] table{overflow-x:auto!important;table-layout:auto!important}[data-source-block] :is(td,th){min-width:8em!important;white-space:normal!important;overflow-wrap:anywhere!important}
[data-source-block] img{width:auto!important;min-width:0!important;max-width:100%!important;height:auto!important}
[data-source-block] li{display:list-item!important}[data-source-block] :is(ul,ol){padding-inline-start:1.6em!important}[data-source-block] tr{display:table-row!important}[data-source-block] :is(td,th){display:table-cell!important}[data-source-block] pre{overflow-x:auto!important;white-space:pre-wrap!important}[data-source-block] :is(h1,h2,h3,h4,h5,h6){display:block!important;line-height:1.45!important;font-weight:700!important}
`;
