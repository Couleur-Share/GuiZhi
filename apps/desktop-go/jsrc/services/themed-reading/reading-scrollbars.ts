/** iframe 和单文件 HTML 不继承应用样式，须显式提供一致的滚动条。 */
export const READING_SCROLLBARS = `
*::-webkit-scrollbar{width:7px;height:7px}
*::-webkit-scrollbar-track,*::-webkit-scrollbar-corner{background:transparent}
*::-webkit-scrollbar-thumb{background:color-mix(in srgb,var(--theme-text) 24%,transparent);border:2px solid transparent;background-clip:padding-box;border-radius:999px}
*::-webkit-scrollbar-thumb:hover{background-color:color-mix(in srgb,var(--theme-text) 42%,transparent)}
*::-webkit-scrollbar-button{display:none;width:0;height:0}
@supports not selector(::-webkit-scrollbar){*{scrollbar-width:thin;scrollbar-color:color-mix(in srgb,var(--theme-text) 24%,transparent) transparent}}
`;
