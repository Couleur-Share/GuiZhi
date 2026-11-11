// 代码生成，勿手改：pnpm dlx tsx spikes/mygo-js/build-render-scripts.mts
package main

// renderScriptInstallProbe 对应 installSettleProbe：安装 DOM 变化观察，返回 true/false。
const renderScriptInstallProbe = `(function installSettleProbe() {
    const key = Symbol.for("guizhi.settle");
    const host = window;
    if (host[key]) return true;
    try {
      const state = { last: performance.now() };
      new MutationObserver(() => {
        state.last = performance.now();
      }).observe(document, {
        childList: true,
        subtree: true,
        characterData: true
      });
      Object.defineProperty(window, key, {
        value: state,
        enumerable: false,
        configurable: true
      });
      return true;
    } catch {
      return false;
    }
  })()`

// renderScriptReadProbe 对应 readSettleProbe：返回 {domQuietMs,textLength,costMs} 或 null。
const renderScriptReadProbe = `(function readSettleProbe() {
    const state = window[Symbol.for("guizhi.settle")];
    if (!state) return null;
    const domQuietMs = performance.now() - state.last;
    const startedAt = performance.now();
    const textLength = document.body ? document.body.innerText.length : 0;
    return { domQuietMs, textLength, costMs: performance.now() - startedAt };
  })()`

// renderScriptSnapshot 对应 captureRenderedDocument：返回 {html,restoredEditors,flattenedShadowRoots?,error?}。
const renderScriptSnapshot = `(function captureRenderedDocument() {
    const liveRoot = document.documentElement;
    const root = liveRoot.cloneNode(true);
    const liveNodes = [liveRoot, ...liveRoot.querySelectorAll("*")];
    const copyNodes = [root, ...root.querySelectorAll("*")];
    const editors = [...document.querySelectorAll(".cm-editor")];
    const copies = [...root.querySelectorAll(".cm-editor")];
    let restoredEditors = 0;
    let codeChars = 0;
    for (const [index, editor] of editors.entries()) {
      try {
        const content = editor.querySelector(".cm-content");
        const view = content?.cmView?.view ?? content?.cmTile?.root?.view;
        const doc = view?.state?.doc;
        if (view?.dom !== editor || !doc || !Number.isSafeInteger(doc.length) || doc.length < 0)
          throw new Error("\\u65E0\\u6CD5\\u786E\\u8BA4\\u4EE3\\u7801\\u7F16\\u8F91\\u5668\\u7684\\u5B8C\\u6574\\u6587\\u6863");
        codeChars += doc.length;
        if (codeChars > 1024 * 1024)
          throw new Error("\\u4EE3\\u7801\\u7F16\\u8F91\\u5668\\u6B63\\u6587\\u8D85\\u8FC7 1 MiB \\u5B57\\u7B26\\u9884\\u7B97");
        const text = doc.toString();
        if (typeof text !== "string" || text.length !== doc.length)
          throw new Error("\\u4EE3\\u7801\\u7F16\\u8F91\\u5668\\u6B63\\u6587\\u957F\\u5EA6\\u6821\\u9A8C\\u5931\\u8D25");
        const pre = document.createElement("pre");
        const code = document.createElement("code");
        code.textContent = text;
        pre.append(code);
        copies[index].replaceWith(pre);
        restoredEditors++;
      } catch {
        return {
          html: "",
          restoredEditors,
          error: "\\u65E0\\u6CD5\\u5B8C\\u6574\\u4FDD\\u5B58\\u7F51\\u9875\\u4EE3\\u7801\\u7F16\\u8F91\\u5668\\uFF0C\\u8BF7\\u91CD\\u8BD5\\u6216\\u6253\\u5F00\\u539F\\u7F51\\u9875"
        };
      }
    }
    let flattenedShadowRoots = 0;
    if (liveNodes.length === copyNodes.length) {
      const sandbox = document.implementation.createHTMLDocument("");
      const flatten = (node, depth) => {
        if (node.nodeType !== 1 || depth > 200)
          return sandbox.importNode(node, true);
        const element = node;
        const copy = sandbox.importNode(element, false);
        const children = element.shadowRoot ? element.shadowRoot.childNodes : element.childNodes;
        for (const child of Array.from(children)) {
          if (child.nodeType === 1 && child.localName === "slot") {
            for (const assigned of child.assignedNodes({
              flatten: true
            }))
              copy.append(flatten(assigned, depth + 1));
          } else copy.append(flatten(child, depth + 1));
        }
        return copy;
      };
      for (const [index, host] of liveNodes.entries()) {
        if (!host.shadowRoot || !root.contains(copyNodes[index])) continue;
        try {
          copyNodes[index].replaceChildren(
            ...Array.from(flatten(host, 0).childNodes)
          );
          flattenedShadowRoots++;
        } catch {
        }
      }
    }
    return {
      html: root.outerHTML,
      restoredEditors,
      ...flattenedShadowRoots ? { flattenedShadowRoots } : {}
    };
  })()`
