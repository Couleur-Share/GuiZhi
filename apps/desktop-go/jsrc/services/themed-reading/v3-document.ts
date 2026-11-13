import { Script } from "node:vm";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";
import sanitizeHtml from "sanitize-html";
import libraries from "virtual:reading-libraries";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import type {
  ReadingIssue,
  ReadingScriptModule,
} from "@guizhi/shared/types/reading-page-v3";
import {
  validateReadingLibraries,
  validateReadingScripts,
} from "@guizhi/shared/utils/reading-page-v3";
import { cleanV3Css } from "./v3-css";
import {
  READING_SVG_TAGS,
  READING_SVG_ATTRIBUTES,
  cleanReadingSvgAttributes,
  readingSvgTag,
  validateReadingSvgElements,
  validateReadingSvg,
} from "./reconstruction-svg";
import { escapeThemedText as esc } from "./content";
import { readingComponentCss } from "./visual-components";
import { V3_READER_SCRIPT } from "./v3-reader-script";

export class ReadingPageError extends Error {
  constructor(readonly issue: ReadingIssue) {
    super(issue.message);
    this.name = "ReadingPageError";
  }
}
export const scriptHash = (s: string) =>
  createHash("sha256").update(s).digest("base64");
const scriptText = (s: string) => s.replace(/<\/script/gi, "<\\/script");
export function checkReadingScript(
  module: ReadingScriptModule,
): ReadingIssue | undefined {
  try {
    new Script(`(()=>{\n"use strict";\n${module.code}\n})();`, {
      filename: `reading-${module.id}.js`,
    });
  } catch (error) {
    const line = Number(
      new RegExp(`reading-${module.id}\\.js:(\\d+)`).exec(
        error instanceof Error ? error.stack : "",
      )?.[1],
    );
    return {
      kind: "script",
      unit: module.id,
      ...(line ? { line: Math.max(1, line - 2) } : {}),
      message:
        `交互 ${module.id} 语法错误：${error instanceof Error ? error.message : String(error)}`.slice(
          0,
          4000,
        ),
    };
  }
}

/** 只规范化格式，不执行任何模型代码。外部资源与事件属性明确报错。 */
export function normalizeV3Section(
  html: string,
  unit: string,
  enhanced: boolean,
) {
  if (typeof html !== "string" || html.length > 2000000)
    throw new ReadingPageError({
      kind: "format",
      unit,
      message: "章节 HTML 无效或过大",
    });
  const doc = parseHTML(
    /<html[\s>]/i.test(html)
      ? html
      : `<html><head></head><body>${html}</body></html>`,
  ).document;
  let css = "";
  const scripts: ReadingScriptModule[] = [];
  for (const style of doc.querySelectorAll("style")) {
    css += style.textContent + "\n";
    style.remove();
  }
  for (const s of doc.querySelectorAll("script")) {
    if (
      s.hasAttribute("src") ||
      (s.getAttribute("type") &&
        !["text/javascript", "application/javascript"].includes(
          s.getAttribute("type"),
        ))
    )
      throw new ReadingPageError({
        kind: "security",
        unit,
        message: "阅读页不能加载外部脚本或导入模块，请使用内置库",
      });
    if (!enhanced)
      throw new ReadingPageError({
        kind: "script",
        unit,
        message: "本次已关闭增强交互，请保留静态内容或使用原生折叠",
      });
    scripts.push({
      id: `${unit}-script${scripts.length}`,
      code: s.textContent,
      status: "pending",
    });
    s.remove();
  }
  if (doc.querySelector("iframe,object,embed,base,link,meta[http-equiv]"))
    throw new ReadingPageError({
      kind: "security",
      unit,
      message: "章节包含嵌入页面、外部资源或页面策略标签",
    });
  for (const e of doc.querySelectorAll("*"))
    for (const a of [...e.attributes]) {
      if (/^on/i.test(a.name))
        throw new ReadingPageError({
          kind: "script",
          unit,
          message: `${e.localName}[${a.name}] 事件属性需改为脚本中的 addEventListener`,
        });
      if (
        ["src", "href", "action", "formaction", "poster"].includes(
          a.name.toLowerCase(),
        ) &&
        a.value &&
        !(a.name === "href" && /^#[a-zA-Z][\w-]*$/.test(a.value)) &&
        !(e.localName === "img" && e.hasAttribute("data-theme-asset"))
      )
        throw new ReadingPageError({
          kind: "resource",
          unit,
          message: `${e.localName}[${a.name}] 不能引用外部资源，请使用素材 ID 或页内锚点`,
        });
    }
  cleanV3Css(css);
  validateReadingSvgElements(doc);
  for (const node of [...doc.body.children])
    node.setAttribute("data-reading-unit", unit);
  return { html: doc.body.innerHTML, css, scripts };
}
const tags = [
  ..."div section article main header footer nav aside p span br hr h1 h2 h3 h4 h5 h6 b strong i em u s del small sub sup blockquote pre code ul ol li table thead tbody tfoot tr td th caption colgroup col a img figure figcaption details summary button input label output canvas fieldset legend select option progress meter".split(
    " ",
  ),
  ...READING_SVG_TAGS,
];
export function cleanV3Html(
  html: string,
  assets: Record<string, string> = {},
): string {
  return sanitizeHtml(html, {
    parser: { lowerCaseTags: false, lowerCaseAttributeNames: false },
    allowedTags: tags,
    allowedAttributes: {
      "*": [
        "id",
        "class",
        "style",
        "title",
        "role",
        "aria-*",
        "data-*",
        ...READING_SVG_ATTRIBUTES,
      ],
      a: ["href", "id", "class", "style", "title", "aria-*", "data-*"],
      img: ["src", "alt", "data-theme-asset", "class", "style", "id"],
      input: [
        "id",
        "class",
        "style",
        "type",
        "name",
        "value",
        "min",
        "max",
        "step",
        "checked",
        "placeholder",
        "aria-*",
        "data-*",
      ],
      label: ["for", "id", "class", "style"],
      button: ["id", "class", "style", "type", "aria-*", "data-*", "disabled"],
      details: ["id", "class", "style", "open"],
      canvas: ["id", "class", "style", "width", "height", "aria-*", "data-*"],
      td: ["class", "style", "colspan", "rowspan"],
      th: ["class", "style", "colspan", "rowspan", "scope"],
      option: ["value", "selected"],
      select: ["id", "class", "aria-label"],
      output: ["id", "for", "class", "aria-live"],
    },
    allowedSchemes: ["data", "guizhi-reading"],
    allowProtocolRelative: false,
    parseStyleAttributes: false,
    transformTags: {
      "*": (name, attributes) => {
        const a = Object.fromEntries(
          Object.entries(attributes).map(([k, v]) => [k.toLowerCase(), v]),
        );
        const tag = name.toLowerCase();
        if (a.style) a.style = cleanV3Css(a.style, true);
        if (tag === "img") {
          const src = assets[a["data-theme-asset"]];
          if (src) a.src = src;
          else {
            delete a.src;
            a.alt = `${a.alt || "主题图片"}（图片尚未完成）`;
          }
        }
        if (a.href && !/^#[a-zA-Z][\w-]*$/.test(a.href)) delete a.href;
        if (
          tag === "input" &&
          !["range", "number", "text", "checkbox", "radio"].includes(a.type)
        )
          a.type = "text";
        if (tag === "button") a.type = "button";
        const safe = readingSvgTag(tag);
        cleanReadingSvgAttributes(safe, a);
        return { tagName: safe, attribs: a };
      },
    },
  });
}
export function validateV3Page(
  page: ThemedReadingVersion,
  partial = false,
): void {
  const d = page.design;
  if (!d || !d.html?.trim())
    throw new ReadingPageError({ kind: "content", message: "页面缺少正文" });
  validateReadingScripts(d.scripts ?? []);
  validateReadingLibraries(d.libraries ?? []);
  cleanV3Css(d.css);
  const raw = normalizeV3Section(d.html, "page", false);
  if (raw.css || raw.scripts.length)
    throw new ReadingPageError({
      kind: "format",
      message: "页面内嵌样式和脚本尚未规范化",
    });
  const doc = parseHTML(
    `<html><body>${cleanV3Html(d.html)}</body></html>`,
  ).document;
  const rawDoc = parseHTML(`<html><body>${d.html}</body></html>`).document;
  if (
    rawDoc.body.textContent.replace(/\s/g, "") !==
    doc.body.textContent.replace(/\s/g, "")
  )
    throw new ReadingPageError({
      kind: "content",
      message: "规范化后正文内容缺失",
    });
  if (!partial) validateReadingSvg(doc, []);
  for (const canvas of doc.querySelectorAll("canvas"))
    if (!(
      canvas.textContent.trim() ||
      canvas
        .closest("figure")
        ?.querySelector("figcaption")
        ?.textContent.trim() ||
      canvas.getAttribute("aria-label")
    ))
      throw new ReadingPageError({
        kind: "content",
        message: "Canvas 图解缺少静态说明",
      });
  if (
    !partial &&
    (!doc.querySelector("h1") ||
      !doc.querySelector("h2") ||
      doc.body.textContent.trim().length < 80)
  )
    throw new ReadingPageError({
      kind: "content",
      unit: page.generation?.sections[0]?.id,
      message: "文章结构或正文不完整",
    });
  const ids = new Set<string>();
  for (const el of doc.querySelectorAll("[id]")) {
    if (
      !/^[a-zA-Z][\w-]*$/.test(el.id) ||
      el.id.startsWith("gz-system") ||
      ids.has(el.id)
    )
      throw new ReadingPageError({
        kind: "format",
        unit: el
          .closest("[data-reading-unit]")
          ?.getAttribute("data-reading-unit"),
        message: `重复或无效页面 ID：${el.id}`,
      });
    ids.add(el.id);
  }
  for (const img of doc.querySelectorAll("img"))
    if (!page.assets.some((a) => a.id === img.getAttribute("data-theme-asset")))
      throw new ReadingPageError({
        kind: "resource",
        message: "页面引用了未声明图片",
      });
  if (!partial)
    for (const a of doc.querySelectorAll("a[href]"))
      if (!ids.has(a.getAttribute("href").slice(1)))
        throw new ReadingPageError({
          kind: "format",
          unit: a
            .closest("[data-reading-unit]")
            ?.getAttribute("data-reading-unit"),
          message: `目录链接缺少对应章节：${a.getAttribute("href")}`,
        });
  if (!partial)
    for (const s of d.scripts ?? []) {
      if (s.status !== "failed") {
        if (s.rootId && !ids.has(s.rootId))
          throw new ReadingPageError({
            kind: "script",
            unit: s.id,
            message: "交互缺少对应页面区域",
          });
        const issue = checkReadingScript(s);
        if (issue) throw new ReadingPageError(issue);
      }
    }
  if (
    page.options.enhancedInteraction === false &&
    d.scripts?.some((s) => s.status !== "failed")
  )
    throw new ReadingPageError({
      kind: "security",
      message: "关闭增强交互的页面不能执行模型脚本",
    });
}
const BASE = `*{box-sizing:border-box}html{color-scheme:light;--theme-surface:#fafafa;--theme-text:#20242b}html[data-theme=dark]{color-scheme:dark;--theme-surface:#11151b;--theme-text:#e7eaf0}body{margin:0;background:var(--theme-surface);color:var(--theme-text);font:var(--reader-font-size,17px)/1.8 var(--reader-font-family,system-ui,sans-serif);overflow-wrap:anywhere}main{max-width:980px;margin:auto;padding:24px}img,svg,canvas{max-width:100%}img{height:auto}table{display:block;overflow-x:auto}pre{overflow:auto}button,input,select{font:inherit;color:inherit}button,summary{cursor:pointer;min-height:36px}button,input,select{background:var(--theme-surface);border:1px solid #8495a8;border-radius:6px;padding:4px 10px}a{color:inherit;text-underline-offset:4px}a:focus-visible,button:focus-visible,input:focus-visible,summary:focus-visible{outline:2px solid #368fbd;outline-offset:3px}[hidden]{display:none!important}@media(max-width:600px){main{padding:16px}}@media(prefers-reduced-motion:reduce){*,*:before,*:after{animation:none!important;transition:none!important}}`;

/** 返回内页；宿主和导出都必须把交互内页放进无同源权限的 iframe。 */
export function v3InnerDocument(
  page: ThemedReadingVersion,
  assets: Record<string, string> = {},
  interactive = true,
  partial = false,
  host = true,
): string {
  validateV3Page(page, partial);
  const d = page.design;
  const doc = parseHTML(
    `<html><body>${cleanV3Html(d.html, assets)}</body></html>`,
  ).document;
  const staticLayout =
    !interactive ||
    partial ||
    page.options.enhancedInteraction === false ||
    d.scripts?.some((s) => s.status === "failed");
  if (staticLayout) {
    for (const e of doc.querySelectorAll("[hidden]"))
      e.removeAttribute("hidden");
    for (const e of doc.querySelectorAll("details")) e.setAttribute("open", "");
    for (const e of doc.querySelectorAll("[style]"))
      e.setAttribute(
        "style",
        cleanV3Css(e.getAttribute("style"), true, [], true),
      );
  }
  const code: string[] = host
    ? [V3_READER_SCRIPT]
    : [
        "document.documentElement.dataset.theme=matchMedia('(prefers-color-scheme:dark)').matches?'dark':'light';",
      ];
  if (interactive && !partial && page.options.enhancedInteraction !== false) {
    for (const lib of d.libraries ?? []) {
      const library = libraries.runtime?.[lib];
      if (!library)
        throw new ReadingPageError({
          kind: "resource",
          message: `内置库 ${lib} 不可用`,
        });
      code.push(library);
    }
    for (const s of d.scripts ?? [])
      if (s.status !== "failed")
        code.push(
          `(()=>{try{\n${s.code}\n}catch(e){window.dispatchEvent(new CustomEvent('reading-script-fault',{detail:{id:${JSON.stringify(s.id)},message:String(e.message||e)}}))}})();`,
        );
  }
  const scripts = code.map(scriptText);
  const csp = `default-src 'none';script-src ${scripts.map((s) => `'sha256-${scriptHash(s)}'`).join(" ")};style-src 'unsafe-inline';img-src data: guizhi-reading:;connect-src 'none';font-src 'none';frame-src 'none';object-src 'none';base-uri 'none';form-action 'none';worker-src 'none'`;
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${esc(csp)}"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(page.reconstruction?.outline?.title ?? page.source.title)}</title><style>${BASE}${readingComponentCss(d.html)}${cleanV3Css(d.css, false, [], staticLayout)}</style></head><body class="gz-reading-components">${doc.body.innerHTML}${scripts.map((s) => `<script>${s}</script>`).join("")}</body></html>`;
}
/** 外壳只有固定代码，无应用能力；离线交互也不把模型代码放在顶层文件来源中。 */
export function v3WrapperDocument(inner: string): string {
  const data = JSON.stringify(inner).replace(/</g, "\\u003c");
  const script = `const f=document.getElementById('reading');f.srcdoc=${data};window.addEventListener('message',e=>{if(e.source!==f.contentWindow||e.origin!=='null')return;});`;
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none';script-src 'unsafe-inline';style-src 'unsafe-inline';img-src data: guizhi-reading:;frame-src 'self' about:;connect-src 'none';base-uri 'none';form-action 'none'"><title>AI 阅读页</title><style>html,body{margin:0;width:100%;height:100%}iframe{border:0;width:100%;height:100%;display:block}</style></head><body><iframe id="reading" title="AI 阅读页" sandbox="allow-scripts" referrerpolicy="no-referrer"></iframe><script>${script}</script></body></html>`;
}
