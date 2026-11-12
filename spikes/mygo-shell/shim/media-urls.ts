// 把页面里的 local-image:// / local-video:// 改写成 WebView2 能加载的 http://<scheme>.localhost/<名称>。
// 原因：WebView2 不认自定义协议，MyGo 的 Protocol.Handle 在 Windows 上实际服务于 http://<scheme>.localhost/。
// 渲染进程代码不改（Electron 版直接用 local-image://），所以在 preload 里统一拦截：
//   1) 元素的 src / poster 属性与 IDL 属性；2) setAttribute；3) innerHTML 等方式带入的节点（MutationObserver）。
// 本文件无依赖，改写规则由 rewriteMediaUrl 单独导出，便于单元测试。
// Rewrites local media URLs to the http://<scheme>.localhost/ form WebView2 serves, so the renderer stays unchanged.

const SCHEMES = ["local-image", "local-video"] as const;

/** 不是本地媒体地址时原样返回。名称按「解码再编码」规整，兼容已编码与未编码两种写法。 */
export function rewriteMediaUrl(value: string): string {
  for (const scheme of SCHEMES) {
    const prefix = scheme + "://";
    if (!value.startsWith(prefix)) continue;
    const rest = value.slice(prefix.length);
    let name = rest;
    try {
      name = decodeURIComponent(rest);
    } catch {
      /* 非法百分号序列：按原文编码 */
    }
    return `http://${scheme}.localhost/` + name.split(/[\\/]+/).map(encodeURIComponent).join("/");
  }
  return value;
}

const ATTRS = ["src", "poster"];
const SELECTOR = ATTRS.map((a) => `[${a}^="local-"]`).join(",");

function fixElement(el: Element) {
  for (const attr of ATTRS) {
    const v = el.getAttribute(attr);
    if (v && v.startsWith("local-")) {
      const next = rewriteMediaUrl(v);
      if (next !== v) nativeSetAttribute.call(el, attr, next);
    }
  }
}

// 安装时才取原生方法：模块顶层不碰 DOM，rewriteMediaUrl 可以在 Node 里单测
let nativeSetAttribute: Element["setAttribute"];

function patchSetAttribute() {
  nativeSetAttribute = Element.prototype.setAttribute;
  Element.prototype.setAttribute = function (name: string, value: string) {
    if (ATTRS.includes(String(name).toLowerCase()) && typeof value === "string") value = rewriteMediaUrl(value);
    return nativeSetAttribute.call(this, name, value);
  };
}

function patchProperty(proto: object, prop: string) {
  const desc = Object.getOwnPropertyDescriptor(proto, prop);
  if (!desc || !desc.set || !desc.get) return;
  Object.defineProperty(proto, prop, {
    ...desc,
    set(v: string) {
      desc.set!.call(this, typeof v === "string" ? rewriteMediaUrl(v) : v);
    },
  });
}

function scan(root: ParentNode) {
  if (root instanceof Element) fixElement(root);
  root.querySelectorAll?.(SELECTOR).forEach(fixElement);
}

export function installMediaUrlRewrite() {
  patchSetAttribute();
  patchProperty(HTMLImageElement.prototype, "src");
  patchProperty(HTMLMediaElement.prototype, "src");
  patchProperty(HTMLSourceElement.prototype, "src");
  patchProperty(HTMLVideoElement.prototype, "poster");
  const observe = () => {
    scan(document);
    new MutationObserver((records) => {
      for (const r of records) {
        if (r.type === "attributes" && r.target instanceof Element) fixElement(r.target);
        for (const n of Array.from(r.addedNodes)) if (n instanceof Element) scan(n);
      }
    }).observe(document, { subtree: true, childList: true, attributes: true, attributeFilter: ATTRS });
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", observe, { once: true });
  else observe();
}
