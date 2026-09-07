import sanitizeHtml from "sanitize-html";
import * as css from "css-tree";
import { createHash } from "node:crypto";
import type { WebSnapshot } from "@guizhi/shared/types";

const properties = new Set(
  "color background background-color background-image background-size background-position background-repeat background-origin background-clip border border-width border-style border-color border-radius border-top border-right border-bottom border-left border-top-left-radius border-top-right-radius border-bottom-left-radius border-bottom-right-radius box-shadow box-sizing width height min-width max-width min-height max-height margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left font-family font-size font-weight font-style font-variant line-height letter-spacing word-spacing text-align text-indent text-decoration text-transform text-shadow white-space word-break overflow-wrap vertical-align display flex flex-direction flex-wrap flex-grow flex-shrink flex-basis align-items align-self align-content justify-content gap row-gap column-gap grid-template-columns grid-template-rows grid-column grid-row position top left right bottom transform transform-origin opacity overflow overflow-x overflow-y list-style-type list-style-position border-collapse border-spacing table-layout float clear content object-fit object-position fill stroke stroke-width".split(
    " ",
  ),
);
export type ResourceMapper = (url: string) => string | undefined;

/** CSS AST 白名单，URL 必须由调用方映射为本快照资源。 */
export function cleanCss(
  value: string,
  map: ResourceMapper,
  stylesheet = false,
): string {
  try {
    const ast = css.parse(value, {
      context: stylesheet ? "stylesheet" : "declarationList",
    });
    css.walk(ast, {
      visit: "Declaration",
      enter(node, item, list) {
        let valid = properties.has(node.property.toLowerCase());
        css.walk(node.value, (n) => {
          if (n.type === "Raw") valid = false;
          if (
            n.type === "Function" &&
            [
              "expression",
              "var",
              "env",
              "paint",
              "image-set",
              "-webkit-image-set",
            ].includes(n.name.toLowerCase())
          )
            valid = false;
          if (n.type === "Url") {
            const mapped = map(n.value);
            if (!mapped) valid = false;
            else n.value = mapped;
          }
        });
        if (
          node.property.toLowerCase() === "position" &&
          !["relative", "absolute", "static"].includes(
            css.generate(node.value).toLowerCase(),
          )
        )
          valid = false;
        if (!valid) list.remove(item);
      },
    });
    if (stylesheet)
      css.walk(ast, {
        visit: "Atrule",
        enter(_node, item, list) {
          list.remove(item);
        },
      });
    return css.generate(ast).replace(/</g, "\\3c ");
  } catch {
    return "";
  }
}

const tags = [
  "div",
  "section",
  "article",
  "main",
  "p",
  "span",
  "br",
  "hr",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "b",
  "strong",
  "i",
  "em",
  "u",
  "s",
  "small",
  "sub",
  "sup",
  "blockquote",
  "pre",
  "code",
  "ul",
  "ol",
  "li",
  "table",
  "thead",
  "tbody",
  "tfoot",
  "tr",
  "td",
  "th",
  "caption",
  "colgroup",
  "col",
  "a",
  "img",
  "figure",
  "figcaption",
  "svg",
  "g",
  "path",
  "circle",
  "ellipse",
  "rect",
  "line",
  "polyline",
  "polygon",
  "text",
  "tspan",
];
const svgAttrs = [
  "viewBox",
  "viewbox",
  "xmlns",
  "d",
  "x",
  "y",
  "x1",
  "x2",
  "y1",
  "y2",
  "cx",
  "cy",
  "r",
  "rx",
  "ry",
  "points",
  "fill",
  "stroke",
  "stroke-width",
  "transform",
  "text-anchor",
];
export function cleanHtml(html: string, map: ResourceMapper): string {
  return sanitizeHtml(html, {
    allowedTags: tags,
    allowedAttributes: {
      "*": ["id", "class", "style", "title", "lang", "dir", ...svgAttrs],
      a: ["href", "title", "id", "class", "style"],
      img: ["src", "alt", "width", "height", "style", "class"],
      td: ["colspan", "rowspan", "style"],
      th: ["colspan", "rowspan", "style"],
      ol: ["start", "style"],
      svg: [...svgAttrs, "width", "height", "style"],
    },
    allowedSchemes: ["https", "http", "local-image"],
    allowProtocolRelative: false,
    parseStyleAttributes: false,
    nonTextTags: [
      "script",
      "style",
      "textarea",
      "option",
      "noscript",
      "iframe",
      "object",
      "embed",
      "form",
      "foreignobject",
    ],
    transformTags: {
      "*": (tagName, attrs) => {
        const next = { ...attrs };
        if (next.style) next.style = cleanCss(next.style, map);
        for (const key of ["fill", "stroke"])
          if (
            next[key] &&
            !/^(#[a-f0-9]{3,8}|[a-z]+|rgba?\([\d.,%\s]+\))$/i.test(next[key])
          )
            delete next[key];
        if (tagName === "img") {
          const src = map(next.src ?? "");
          if (src) next.src = src;
          else {
            delete next.src;
            next.alt = next.alt || "图片未保存";
          }
        }
        if (
          tagName === "a" &&
          next.href &&
          !/^(https?:\/\/|#[\w-])/i.test(next.href)
        )
          delete next.href;
        return { tagName, attribs: next };
      },
    },
  });
}

export function snapshotHash(
  snapshot: Omit<WebSnapshot, "hash"> | WebSnapshot,
): string {
  return createHash("sha256")
    .update(
      JSON.stringify({
        html: snapshot.html,
        css: snapshot.css,
        assets: snapshot.assets.map((a) => [a.fileName, a.sha256]).sort(),
        failures: snapshot.failures,
        metadata: [snapshot.account, snapshot.author, snapshot.publishedAt],
      }),
    )
    .digest("hex");
}
/** 数据库或备份不视为可信 HTML；读出时再次清理。 */
export function sanitizeSnapshot(snapshot: WebSnapshot): WebSnapshot {
  if (
    snapshot?.formatVersion !== 1 ||
    typeof snapshot.html !== "string" ||
    typeof snapshot.css !== "string" ||
    snapshot.html.length + snapshot.css.length > 12 * 1024 * 1024 ||
    !Array.isArray(snapshot.assets) ||
    snapshot.assets.length > 200
  )
    throw new Error("原文快照格式无效");
  if (
    !Array.isArray(snapshot.failures) ||
    snapshot.failures.length > 10000 ||
    snapshot.failures.some(
      (f) => !f || typeof f.url !== "string" || typeof f.reason !== "string",
    ) ||
    !Array.isArray(snapshot.warnings) ||
    snapshot.warnings.some((w) => typeof w !== "string") ||
    typeof snapshot.account !== "string" ||
    typeof snapshot.author !== "string"
  )
    throw new Error("原文快照元数据无效");
  const assets = snapshot.assets.filter(
    (a) =>
      a &&
      /^wechat-[a-f0-9]{64}\.(png|jpg|gif|webp)$/.test(a.fileName) &&
      /^[a-f0-9]{64}$/.test(a.sha256),
  );
  if (assets.length !== snapshot.assets.length)
    throw new Error("原文快照资源清单无效");
  const allowed = new Set(assets.map((a) => `local-image://${a.fileName}`));
  const map = (url: string) => (allowed.has(url) ? url : undefined);
  return {
    ...snapshot,
    html: cleanHtml(snapshot.html, map),
    css: cleanCss(snapshot.css, map, true),
    assets,
  };
}
