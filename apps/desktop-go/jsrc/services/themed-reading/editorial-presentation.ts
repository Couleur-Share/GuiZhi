/** 按内容关系选择的阅读组件；仅作用于明确使用 reading-* 类的新设计。 */
export const THEMED_EDITORIAL_CSS = `
.reading-page{--theme-surface:#f7f6f1;--theme-text:#263b37;--reader-panel-surface:#eaf0eb;--reader-panel-text:#243d34;--reader-warm-surface:#f3eadc;--reader-warm-text:#513e27;--reader-accent:#416854;max-width:72rem;border-radius:16px}
.reading-page.reading-explainer{--theme-surface:#faf6ed;--theme-text:#373b32;--reader-panel-surface:#efeddf;--reader-panel-text:#373c2c;--reader-warm-surface:#f3e4c9;--reader-warm-text:#554021;--reader-accent:#8a632d}
@media(prefers-color-scheme:dark){.reading-page{--theme-surface:#1d2928;--theme-text:#e6eeea;--reader-panel-surface:#293b36;--reader-panel-text:#e2ede5;--reader-warm-surface:#3b3329;--reader-warm-text:#f2e5d2;--reader-accent:#aed0b4}.reading-page.reading-explainer{--theme-surface:#292b25;--theme-text:#eeeade;--reader-panel-surface:#343a2d;--reader-panel-text:#e6eadb;--reader-warm-surface:#453a29;--reader-warm-text:#f8e7c8;--reader-accent:#e4bd80}}
:root[data-theme="light"] .reading-page{--theme-surface:#f7f6f1;--theme-text:#263b37;--reader-panel-surface:#eaf0eb;--reader-panel-text:#243d34;--reader-warm-surface:#f3eadc;--reader-warm-text:#513e27;--reader-accent:#416854}
:root[data-theme="light"] .reading-page.reading-explainer{--theme-surface:#faf6ed;--theme-text:#373b32;--reader-panel-surface:#efeddf;--reader-panel-text:#373c2c;--reader-warm-surface:#f3e4c9;--reader-warm-text:#554021;--reader-accent:#8a632d}
:root[data-theme="dark"] .reading-page{--theme-surface:#1d2928;--theme-text:#e6eeea;--reader-panel-surface:#293b36;--reader-panel-text:#e2ede5;--reader-warm-surface:#3b3329;--reader-warm-text:#f2e5d2;--reader-accent:#aed0b4}
:root[data-theme="dark"] .reading-page.reading-explainer{--theme-surface:#292b25;--theme-text:#eeeade;--reader-panel-surface:#343a2d;--reader-panel-text:#e6eadb;--reader-warm-surface:#453a29;--reader-warm-text:#f8e7c8;--reader-accent:#e4bd80}
.reading-page .reading-masthead{padding:0 0 24px;border-bottom:1px solid color-mix(in srgb,currentColor 18%,transparent);margin:0 0 24px}
.reading-page .reading-kicker{font-size:max(12px,.8em);font-weight:700;letter-spacing:.12em;color:var(--reader-accent)}
.reading-page .reading-title{font-size:clamp(30px,2.6em,52px);font-weight:750;line-height:1.3;margin:12px 0 24px;max-width:26em;text-wrap:balance}
.reading-page [data-source-block].reading-title :is(h1,h2,h3){font-size:clamp(30px,2.6em,52px);margin:12px 0 24px}
.reading-page .reading-context :is(h1,h2,h3,p){font-size:max(12px,.8em)!important;margin:0!important}
.reading-page .reading-hero{display:grid;grid-template-columns:minmax(0,2fr) minmax(0,1fr);gap:24px;align-items:center}
.reading-page .reading-lead p{margin:0;max-width:46em}
.reading-page .reading-art{margin:0;width:100%;max-width:20rem;justify-self:center}
.reading-page .reading-art img{display:block;width:100%;border-radius:10px}
.reading-page .reading-masthead:has(+.reading-contents){margin-bottom:0}
.reading-page .reading-contents{margin:0 0 28px;border-bottom:1px solid color-mix(in srgb,currentColor 18%,transparent)}
.reading-page .reading-contents>summary{padding:10px 4px;min-height:44px;font-size:.88em;font-weight:600;cursor:pointer}
.reading-page .reading-contents>summary:focus-visible{outline:2px solid currentColor;outline-offset:3px}
.reading-page .reading-contents nav[data-reader-toc]{display:grid!important;grid-template-columns:repeat(2,minmax(0,1fr))!important;border:0!important;padding:8px 0 16px!important}
.reading-page .reading-pair{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:28px;align-items:start;margin-bottom:28px}
.reading-page .reading-section{padding:0 0 24px;margin:0 0 28px;border-bottom:1px solid color-mix(in srgb,currentColor 18%,transparent)}
.reading-page .reading-pair>.reading-section{border:0;margin:0;padding:0}
.reading-page .reading-section :is(h2,h3){font-size:1.25em;margin:0 0 16px;line-height:1.5}
.reading-page .reading-comparison :is(ul,ol),.reading-page .reading-checklist :is(ul,ol),.reading-page .reading-synthesis :is(ul,ol){display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px 20px;list-style-type:none;padding-inline-start:0!important;margin:0}
.reading-page :is(.reading-comparison,.reading-checklist,.reading-synthesis) li{padding:16px;margin:0;border-radius:10px;--theme-surface:var(--reader-panel-surface);--theme-text:var(--reader-panel-text)}
.reading-page :is(.reading-comparison,.reading-checklist,.reading-synthesis) li :is(ul,ol){display:block;list-style-type:disc;padding-inline-start:1.6em!important}
.reading-page .reading-comparison li{border-top:2px solid color-mix(in srgb,var(--reader-accent) 40%,transparent);border-radius:0;padding:12px 16px}
.reading-page .reading-checklist{padding:24px;border:0;border-radius:12px;--theme-surface:var(--reader-panel-surface);--theme-text:var(--reader-panel-text)}
.reading-page .reading-checklist li{padding:8px 0;border-radius:0;counter-increment:reading-step}
.reading-page .reading-checklist :is(ul,ol){counter-reset:reading-step}
.reading-page .reading-checklist li::before{content:counter(reading-step,decimal-leading-zero);display:block;font-size:.85em;font-weight:700;margin-bottom:6px;color:var(--reader-accent)}
.reading-page .reading-synthesis li:last-child{--theme-surface:var(--reader-warm-surface);--theme-text:var(--reader-warm-text)}
.reading-page :is(.reading-comparison,.reading-checklist,.reading-synthesis) li li{padding:0;border:0;border-radius:0;counter-increment:none;--theme-surface:inherit;--theme-text:inherit}
.reading-page .reading-checklist li :is(ul,ol){counter-reset:none}
.reading-page .reading-checklist li li::before{content:none}
.reading-page .reading-notes{max-width:100%}
.reading-page .reading-dimensions[data-reader-toc]{display:grid!important;grid-template-columns:repeat(3,minmax(0,1fr))!important;gap:12px!important;padding:0!important;margin:0 0 32px!important;border:0!important;border-radius:0!important}
.reading-page .reading-dimensions [data-reader-toc-label]{grid-column:1/-1;padding:0 0 4px!important}
.reading-page .reading-dimensions a[data-reader-toc-link]{display:block!important;min-height:92px!important;margin:0!important;padding:16px!important;border:1px solid color-mix(in srgb,currentColor 22%,transparent);border-top:3px solid var(--reader-accent);border-radius:8px!important;font-size:1em!important;background:var(--reader-panel-surface)!important;color:var(--reader-panel-text)!important}
.reading-page .reading-dimensions [data-reader-toc-link]::before,.reading-page .reading-dimensions [data-reader-toc-link]::after{content:none}
.reading-page .reading-dimensions a[data-reader-toc-link]:hover{box-shadow:inset 0 0 0 1px var(--reader-accent)}
.reading-page .reading-dimension-body{display:grid;grid-template-columns:minmax(0,2fr) minmax(0,1fr);gap:24px;align-items:center}
.reading-page .reading-dimension-body .reading-art{max-width:15rem}
.reading-page .reading-principle{padding:20px 24px;border-inline-start:3px solid var(--reader-accent);border-bottom:0;--theme-surface:var(--reader-warm-surface);--theme-text:var(--reader-warm-text)}
.reading-page .reading-checklist.reading-three :is(ul,ol){grid-template-columns:repeat(3,minmax(0,1fr))}
@media(max-width:799px){.reading-page .reading-title{font-size:clamp(28px,2em,36px);margin-bottom:16px}.reading-page .reading-hero,.reading-page .reading-pair,.reading-page .reading-dimension-body{grid-template-columns:minmax(0,1fr);gap:18px}.reading-page .reading-hero .reading-art{max-width:13rem}.reading-page .reading-dimension-body .reading-art{max-width:13rem}.reading-page .reading-checklist,.reading-page .reading-principle{padding:18px}.reading-page .reading-contents nav[data-reader-toc]{grid-template-columns:minmax(0,1fr)!important}.reading-page .reading-dimensions[data-reader-toc]{grid-template-columns:minmax(0,1fr)!important;gap:8px!important}.reading-page .reading-dimensions a[data-reader-toc-link]{min-height:44px!important;padding:10px 14px!important}}
`;
