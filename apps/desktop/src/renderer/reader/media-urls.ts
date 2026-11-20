/**
 * 把页面里的 local-image:// / local-video:// 改写成岛自己的 scheme 下的路径（宿主在 /media/<scheme>/<名称> 提供，并做路径与扩展名校验）。
 *
 * 做法与 Go 壳里旧渲染层的 shim/media-urls.ts 一致（它改写到 http://<scheme>.localhost/）：复用的阅读组件不改，
 * 在页面里统一拦截 1) 元素的 src / poster 属性与 IDL 属性、2) setAttribute、3) 经 innerHTML 等带入的节点（MutationObserver）。
 * 规则函数单独导出，便于单测；installMediaRewrite 才碰 DOM。
 *
 * 路径是相对的（/media/…），随页面地址解析成 <scheme>://<host>/media/…，这样本文件不需要知道岛的 scheme 名。
 */

const SCHEMES = ["local-image", "local-video"] as const;

/** 不是本地媒体地址时原样返回。名称按「解码再编码」规整，兼容已编码与未编码两种写法。 */
export function rewriteMediaUrl(value: string): string {
  for (const scheme of SCHEMES) {
    const prefix = `${scheme}://`;
    if (!value.startsWith(prefix)) continue;
    const rest = value.slice(prefix.length);
    let name = rest;
    try {
      name = decodeURIComponent(rest);
    } catch {
      /* 非法百分号序列：按原文编码 */
    }
    const segments = name.split(/[\\/]+/).filter(Boolean);
    return `/media/${scheme}/${segments.map(encodeURIComponent).join("/")}`;
  }
  return value;
}

const ATTRS = ["src", "poster"];
const SELECTOR = ATTRS.map((attr) => `[${attr}^="local-"]`).join(",");

let nativeSetAttribute: Element["setAttribute"] | null = null;

function fixElement(element: Element): void {
  for (const attr of ATTRS) {
    const value = element.getAttribute(attr);
    if (value && value.startsWith("local-")) {
      const next = rewriteMediaUrl(value);
      if (next !== value) (nativeSetAttribute ?? element.setAttribute).call(element, attr, next);
    }
  }
}

function scan(root: ParentNode): void {
  if (root instanceof Element) fixElement(root);
  root.querySelectorAll?.(SELECTOR).forEach(fixElement);
}

function patchProperty(proto: object, prop: string): void {
  const desc = Object.getOwnPropertyDescriptor(proto, prop);
  if (!desc || !desc.set || !desc.get) return;
  const set = desc.set;
  Object.defineProperty(proto, prop, {
    ...desc,
    set(this: unknown, value: unknown) {
      set.call(this, typeof value === "string" ? rewriteMediaUrl(value) : value);
    },
  });
}

let installed = false;

/** 安装一次：之后页面里任何方式写进来的 local-* 地址都会被改写。 */
export function installMediaRewrite(): void {
  if (installed) return;
  installed = true;
  nativeSetAttribute = Element.prototype.setAttribute;
  const native = nativeSetAttribute;
  Element.prototype.setAttribute = function (name: string, value: string) {
    const key = String(name).toLowerCase();
    return native.call(
      this,
      name,
      ATTRS.includes(key) && typeof value === "string" ? rewriteMediaUrl(value) : value,
    );
  };
  patchProperty(HTMLImageElement.prototype, "src");
  patchProperty(HTMLMediaElement.prototype, "src");
  patchProperty(HTMLSourceElement.prototype, "src");
  patchProperty(HTMLVideoElement.prototype, "poster");
  const observe = (): void => {
    scan(document);
    new MutationObserver((records) => {
      for (const record of records) {
        if (record.type === "attributes" && record.target instanceof Element) {
          fixElement(record.target);
        }
        for (const node of Array.from(record.addedNodes)) {
          if (node instanceof Element) scan(node);
        }
      }
    }).observe(document, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ATTRS,
    });
  };
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", observe, { once: true });
  } else {
    observe();
  }
}
