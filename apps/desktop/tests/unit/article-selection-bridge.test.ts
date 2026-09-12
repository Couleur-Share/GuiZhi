// @vitest-environment node
import { afterEach, expect, it } from "vitest";
import { JSDOM } from "jsdom";
import { ARTICLE_SELECTION_BRIDGE } from "../../src/main/services/web-capture/article-selection-bridge";
const opened: JSDOM[] = [];
afterEach(() => opened.splice(0).forEach(d => d.window.close()));
function fixture() {
  const host = new JSDOM(""), page = new JSDOM('<body><details><summary>展开</summary><p>反向代理需要核对流量路径</p></details><input value="不能采集输入框"></body>', { runScripts: "outside-only", beforeParse(win) { Object.defineProperty(win, "parent", { value: host.window }); } });
  opened.push(host, page);
  page.window.eval(`const id='instance';const sent=[];window.sent=sent;const send=(type,value)=>sent.push({type,value});${ARTICLE_SELECTION_BRIDGE}`);
  const send = (source: Window, id = "instance") => page.window.dispatchEvent(new page.window.MessageEvent("message", { source, data: { id, type: "article-locate", value: { text: "反向代理", request: "r" } } }));
  return { host, page, send, messages: (page.window as any).sent as any[] };
}
it("拒绝不匹配实例与非父窗口消息，正确定位会展开段落", () => {
  const { host, page, send, messages } = fixture();
  send(page.window as unknown as Window); send(host.window as unknown as Window, "other");
  expect(messages).toEqual([]);
  send(host.window as unknown as Window);
  expect(messages[0]).toMatchObject({ type: "article-located", value: { found: true, request: "r" } });
  expect(page.window.document.querySelector("details")?.open).toBe(true);
});
it("选择消息只传纯文本及有限坐标，不传 HTML", () => {
  const { page, messages } = fixture(), range = page.window.document.createRange();
  range.selectNodeContents(page.window.document.querySelector("p")!);
  (range as any).getBoundingClientRect = () => ({ left: 20, bottom: 40 });
  const selection = page.window.getSelection()!; selection.addRange(range);
  page.window.document.dispatchEvent(new page.window.MouseEvent("mouseup"));
  expect(messages[0]).toEqual({ type: "article-selection", value: { text: "反向代理需要核对流量路径", x: 20, y: 40 } });
});
