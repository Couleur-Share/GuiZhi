/** 在网页主世界运行；保持自包含，供 executeJavaScript 序列化。 */
export function captureRenderedDocument(): {
  html: string;
  restoredEditors: number;
  /** 被展开的 open shadow root 宿主数；没有时不出现，保持原有返回形状。 */
  flattenedShadowRoots?: number;
  error?: string;
} {
  const liveRoot = document.documentElement;
  const root = liveRoot.cloneNode(true) as HTMLElement;
  // 原节点与副本节点按文档顺序一一对应。替换代码编辑器会改变副本结构，所以先建立对应关系。
  const liveNodes = [liveRoot, ...liveRoot.querySelectorAll("*")];
  const copyNodes = [root, ...root.querySelectorAll("*")];
  const editors = [...document.querySelectorAll<HTMLElement>(".cm-editor")];
  const copies = [...root.querySelectorAll<HTMLElement>(".cm-editor")];
  let restoredEditors = 0;
  let codeChars = 0;
  for (const [index, editor] of editors.entries()) {
    // CodeMirror 的 DOM 只保留视口内行。对应 findFromDOM 的两代实现，
    // 读取完整 state.doc 后仅替换快照副本，不能拿可见行拼装并声称完整。
    type View = {
      dom: HTMLElement;
      state: { doc: { length: number; toString(): string } };
    };
    type Content = HTMLElement & {
      cmView?: { view?: View };
      cmTile?: { root?: { view?: View } };
    };
    try {
      const content = editor.querySelector<Content>(".cm-content");
      const view = content?.cmView?.view ?? content?.cmTile?.root?.view;
      const doc = view?.state?.doc;
      if (
        view?.dom !== editor ||
        !doc ||
        !Number.isSafeInteger(doc.length) ||
        doc.length < 0
      )
        throw new Error("无法确认代码编辑器的完整文档");
      codeChars += doc.length;
      if (codeChars > 1024 * 1024)
        throw new Error("代码编辑器正文超过 1 MiB 字符预算");
      const text = doc.toString();
      if (typeof text !== "string" || text.length !== doc.length)
        throw new Error("代码编辑器正文长度校验失败");
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
        error: "无法完整保存网页代码编辑器，请重试或打开原网页",
      };
    }
  }
  // 自定义元素升级后，内容可能整体移入 shadow DOM：cloneNode 与 outerHTML 都不含 shadow root。
  // 页面加载越快，升级越早发生在快照之前，正文（如代码块）就越容易整段丢失。
  // 按渲染结果展开 open shadow root：以 shadow 树为子节点，<slot> 换成其分配到的节点（含后备内容）。
  // 在无浏览上下文的文档里复制节点，避免触发页面自定义元素的构造函数。
  let flattenedShadowRoots = 0;
  if (liveNodes.length === copyNodes.length) {
    const sandbox = document.implementation.createHTMLDocument("");
    const flatten = (node: Node, depth: number): Node => {
      if (node.nodeType !== 1 || depth > 200)
        return sandbox.importNode(node, true);
      const element = node as Element;
      const copy = sandbox.importNode(element, false);
      const children = element.shadowRoot
        ? element.shadowRoot.childNodes
        : element.childNodes;
      for (const child of Array.from(children)) {
        if (child.nodeType === 1 && (child as Element).localName === "slot") {
          for (const assigned of (child as HTMLSlotElement).assignedNodes({
            flatten: true,
          }))
            copy.append(flatten(assigned, depth + 1));
        } else copy.append(flatten(child, depth + 1));
      }
      return copy;
    };
    for (const [index, host] of liveNodes.entries()) {
      // 被代码编辑器替换掉的副本，或已被外层宿主展开时丢弃的副本，不再处理。
      if (!host.shadowRoot || !root.contains(copyNodes[index])) continue;
      try {
        copyNodes[index].replaceChildren(
          ...Array.from(flatten(host, 0).childNodes),
        );
        flattenedShadowRoots++;
      } catch {
        // 个别组件的 shadow 树无法读取时保留副本原状，不连累整页快照。
      }
    }
  }
  return {
    html: root.outerHTML,
    restoredEditors,
    ...(flattenedShadowRoots ? { flattenedShadowRoots } : {}),
  };
}
