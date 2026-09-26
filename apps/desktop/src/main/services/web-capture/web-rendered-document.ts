/** 在网页主世界运行；保持自包含，供 executeJavaScript 序列化。 */
export function captureRenderedDocument(): {
  html: string;
  restoredEditors: number;
  error?: string;
} {
  const root = document.documentElement.cloneNode(true) as HTMLElement;
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
  return { html: root.outerHTML, restoredEditors };
}
