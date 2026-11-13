/** 低优先级的阅读层次：AI 可以覆盖这些静态设计，内容保护单独负责可见性。 */
export const THEMED_READING_PRESENTATION_CSS = `
:where(body){padding:clamp(12px,2vw,24px)}
:where(article,main){max-width:76rem;margin-inline:auto}
:where([data-source-layout] > header){padding-block:1rem 1.5rem;border-bottom:1px solid color-mix(in srgb,currentColor 18%,transparent);margin-bottom:1.5rem}
:where([data-source-layout] > section){padding-block:1.5rem;border-bottom:1px solid color-mix(in srgb,currentColor 14%,transparent)}
:where([data-source-layout] > section:last-child){border-bottom:0}
:where([data-source-block] :is(h1,h2,h3,h4)){margin-block:.25em .8em;text-wrap:pretty}
:where([data-source-block] h1){font-size:2em}
:where([data-source-block] h2){font-size:1.5em}
:where([data-source-block] h3){font-size:1.25em}
:where([data-source-block] li + li){margin-top:.55em}
:where(figure){margin:1.5rem 0}
:where(figure img){border-radius:8px}
:where([data-source-block] blockquote){margin-inline:0;padding:.5em 1em;border-inline-start:3px solid currentColor}
:where([data-source-block] pre){padding:1em;border:1px solid color-mix(in srgb,currentColor 18%,transparent);border-radius:8px}
`;

/** 目录可点击性由程序保证，不受模型去下划线或同色裸文本样式影响。 */
export const THEMED_READING_NAVIGATION_CSS = `
nav a[href]{cursor:pointer!important;text-decoration:underline!important;text-underline-offset:.22em;text-decoration-thickness:1px}
[data-reader-toc]{display:flex!important;flex-direction:column!important;align-items:stretch!important;align-self:start;gap:0!important;padding:12px!important;border:1px solid color-mix(in srgb,currentColor 20%,transparent)!important;border-radius:12px!important;counter-reset:reader-section}
[data-reader-toc-label]{display:block!important;padding:6px 10px 12px!important;font-size:.85em!important;font-weight:700!important;letter-spacing:.04em}
[data-reader-toc] a[data-reader-toc-link]{display:grid!important;grid-template-columns:1.6em minmax(0,1fr) .8em!important;align-items:baseline!important;gap:.5em!important;margin:2px 0!important;padding:10px!important;min-height:44px!important;width:100%!important;border-radius:7px!important;font-size:.9em!important;line-height:1.5!important;counter-increment:reader-section}
[data-reader-toc-link]::before{content:counter(reader-section,decimal-leading-zero);font-size:.8em;font-weight:600;text-decoration:none;opacity:.65}
[data-reader-toc-link]::after{content:"↓";text-decoration:none}
nav a[href]:hover,nav a[aria-current="location"]{background:color-mix(in srgb,var(--theme-text) 10%,var(--theme-surface))!important}
nav a[href]:focus-visible{outline:2px solid currentColor!important;outline-offset:2px!important}
[data-reader-target],[data-source-layout]:target,[data-source-block]:target{outline:2px solid color-mix(in srgb,var(--theme-text) 40%,transparent);outline-offset:4px}
@media(max-width:959px){[data-reader-toc]{display:grid!important;grid-template-columns:repeat(auto-fit,minmax(min(100%,14rem),1fr))!important;margin-bottom:1.25rem!important}[data-reader-toc-label]{grid-column:1/-1}}
@media(max-width:799px){[data-source-layout],[data-source-block] :is(ul,ol){grid-template-columns:minmax(0,1fr)!important;flex-direction:column!important}[data-source-layout]>[data-source-block]{flex-basis:auto!important}[data-reader-toc]{margin-bottom:1.25rem!important}}
@media(prefers-reduced-motion:reduce){html{scroll-behavior:auto}}
`;
