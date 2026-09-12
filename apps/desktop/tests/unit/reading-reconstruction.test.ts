import { readingSvgFixture } from "./reading-svg-fixture";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { runInNewContext } from "node:vm";
import { READING_EXPRESSION_SCRIPT } from "@guizhi/shared/utils/reading-expression-runtime";
import { describe, it, expect, vi } from "vitest";
import { parseHTML } from "linkedom";
import { evaluateReadingExpression } from "@guizhi/shared/utils/reading-expression";
import { readingSourceText, replaceReadingSource } from "@guizhi/shared/utils/reading-source";
import { validateReadingReconstruction } from "@guizhi/shared/utils/reading-reconstruction";
import { validateThemedReadingVersion } from "@guizhi/db/themed-reading-validation";
import { themedReadingDocument } from "../../src/main/services/themed-reading/document";
import { reconstructionChunks, runReconstruction } from "../../src/main/services/themed-reading/reconstruction-pipeline";
const mocks = vi.hoisted(() => ({ call: vi.fn(), search: vi.fn(), capture: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/design", () => ({ callDesignModel: mocks.call, planTheme: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/search-service", () => ({ searchReadingWeb: mocks.search }));
vi.mock("../../src/main/services/web-capture/web-capture", () => ({ captureWebPage: mocks.capture }));

describe("自由重构阅读", () => {
  it("设计请求复用正文并传递流式进度，保存前展开引用", async () => {
    const p = reconstructionFixture(); p.design = null; p.designDirection = "蓝色";
    p.reconstruction.draft[0].markdown = "完整的正文内容，保留全部关键解释。".repeat(10);
    const progress = vi.fn();
    mocks.call.mockReset().mockImplementation(async (...args) => {
      expect(JSON.parse(args[1]).manuscript[0].blocks[0].copyId).toBe("s0b0");
      args[6].onProgress(1234);
      return { html: '<main><h1>文章</h1><h2>说明</h2><div data-reading-copy="s0b0"></div></main>', css: "", interactions: [] };
    });
    await runReconstruction(p, {model:"test"} as any, new AbortController().signal, {checkpoint:vi.fn(),stage:vi.fn(),request:vi.fn(),designProgress:progress});
    expect(mocks.call).toHaveBeenCalledTimes(1);
    expect(p.design.html).toContain(p.reconstruction.draft[0].markdown);
    expect(p.design.html).not.toContain("data-reading-copy");
    expect(progress.mock.calls).toEqual([[1,0],[1,1234]]);
  });

  it("SVG 引用失败会修复重试，关闭图片生成仍可完成图解", async () => {
    const p = readingSvgFixture(); p.options.action = "redesign"; p.designDirection = "阅读方法";
    const design = p.design; p.design = null;
    mocks.call.mockReset().mockResolvedValueOnce({ ...design, html: design.html.replace('id="flow-arrow"', 'id="missing-arrow"'), interactions: [] }).mockResolvedValueOnce({ ...design, interactions: [] });
    await runReconstruction(p, { model: "test" } as any, new AbortController().signal, { checkpoint: vi.fn(), stage: vi.fn(), request: vi.fn() });
    expect(mocks.call).toHaveBeenCalledTimes(2);
    expect(JSON.parse(mocks.call.mock.calls[1][1]).repair).toMatch(/SVG.*不存在/);
    expect(p.options.generateImages).toBe(false); expect(p.assets).toHaveLength(0);
    expect(themedReadingDocument(p)).toContain('markerWidth="7"');
  });

  it("未知格式明确拒绝，离线公式运行时与主进程解析器一致", () => {
    const page = reconstructionFixture(); (page as any).formatVersion = 99;
    expect(() => validateThemedReadingVersion(page)).toThrow();
    for (const expression of ["1+2*3", "min(a,3)+max(2,4)", "-(a+3)/2", "1/0", "process.exit()", "x", "2**3"]) {
      const execute = (fn: () => number) => {try {return fn();} catch {return "error";}};
      expect(execute(() => runInNewContext(`${READING_EXPRESSION_SCRIPT};evaluateReadingExpression(expression,{a:10})`,{expression}))).toBe(execute(() => evaluateReadingExpression(expression,{a:10})));
    }
  });
  it("图片文案编辑保留元数据、画廊与识别段落", () => {
    const item = {itemType:"image" as const,content:"> 平台：图片 · 作者：测试\n\n旧文案\n\n![原图](local-image://a.png)\n\n## 图中文字\n\n识别结果"};
    const result = replaceReadingSource(item,"body","新文案");
    expect(readingSourceText(item,"body")).toBe("旧文案");
    expect(result).toContain("> 平台：图片 · 作者：测试"); expect(result).toContain("![原图](local-image://a.png)"); expect(result).toContain("识别结果");
    expect(readingSourceText({...item,content:result},"body")).toBe("新文案");
  });
  it("允许重写和扩写，不输出原文核对与源码附录，导出不携带桥接", () => {
    const page = reconstructionFixture(); validateThemedReadingVersion(page);
    const html = themedReadingDocument(page), doc = parseHTML(html).document;
    expect(doc.querySelector("h1").textContent).toBe("重新拟定的标题");
    expect(doc.body.textContent).not.toContain("仅原文含有的私有文字");
    expect(html).not.toContain("核对原文"); expect(html).not.toContain("data-instance=");
    expect(doc.querySelectorAll("input")).toHaveLength(2);
    const script = [...doc.querySelectorAll("script")].find(s => !s.hasAttribute("type"));
    // 检测打包函数引用了外部帮助变量的风险。
    expect(script.textContent).not.toMatch(/\beval\s*\(|new Function/);
  });
  it.each(["<script>alert(1)</script>", '<img src=x onerror="alert(1)">', "<iframe></iframe>"])("拒绝模型可执行内容 %s", extra => { const p = reconstructionFixture(); p.design.html += extra; expect(() => themedReadingDocument(p)).toThrow(); });
  it.each(["@import 'https://evil.test/x';", "p{background:url(https://evil.test)}", "body{position:fixed}"])("拒绝加载和覆盖样式 %s", css => { const p = reconstructionFixture(); p.design.css = css; expect(() => themedReadingDocument(p)).toThrow(); });
  it("伪造资料 ID 在读库时被拒绝", () => { const p = reconstructionFixture(); p.reconstruction.draft[0].referenceIds = ["fake"]; expect(() => validateThemedReadingVersion(p)).toThrow(); });
  it("资料仅在导出末尾折叠展示，不带内部正文", () => {
    const p = reconstructionFixture(); p.reconstruction.references = [{ id: "R1", title: "官方资料", url: "https://example.org/ref", capturedAt: 1, text: "不会导出的采集全文", status: "ready" }]; p.reconstruction.draft[0].referenceIds = ["R1"];
    expect(themedReadingDocument(p)).toContain("<summary>参考资料</summary>");
    expect(themedReadingDocument(p)).not.toContain("不会导出的采集全文");
    expect(themedReadingDocument(p, "host")).not.toContain("<summary>参考资料</summary>");
  });
  it("完整分块包括超长输入中间内容", () => { const content = "前".repeat(20000) + "中部唯一事实" + "后".repeat(20000); expect(reconstructionChunks(content).join("")).toBe(content); });
  it("安全公式支持变量与函数，拒绝代码和无效结果", () => {
    expect(evaluateReadingExpression("max(a/2, min(b,3)) + 1", { a: 10, b: 2 })).toBe(6);
    for (const expr of ["globalThis.process", "alert(1)", "a.constructor", "1/0", "x", "1;2"]) expect(() => evaluateReadingExpression(expr, {})).toThrow();
    expect(evaluateReadingExpression("200 / 90", {})).toBeCloseTo(2.2222);
  });
  it("论坛分段编辑保留元数据与其他正文，CRLF 不丢失", () => {
    const item = { itemType: "forum" as const, content: "> 平台：论坛\r\n\r\n## 讨论总结\r\n\r\n旧总结\r\n\r\n## 正文\r\n\r\n主楼\r\n\r\n## 讨论（1 条）\r\n\r\n回复内容" };
    const content = replaceReadingSource(item, "summary", "新总结");
    expect(content).toContain("> 平台：论坛\r\n"); expect(content).toContain("## 正文\r\n\r\n主楼"); expect(content).toContain("回复内容");
    expect(readingSourceText({ ...item, content }, "summary")).toBe("新总结");
    expect(readingSourceText({ ...item, content: replaceReadingSource(item, "body", "新主楼") }, "summary")).toBe("旧总结");
  });
  it("仅调整设计复用编辑稿且不搜索", async () => {
    const p = reconstructionFixture(); p.options.action = "redesign"; p.options.research = true; p.reconstruction.researchComplete = true;
    const d = p.design; p.design = null; p.designDirection = "蓝色";
    mocks.call.mockReset().mockResolvedValue({ ...d, interactions: p.reconstruction.interactions }); mocks.search.mockReset();
    const hooks = { checkpoint: vi.fn(), stage: vi.fn(), request: vi.fn() };
    await runReconstruction(p, { model: "test" } as any, new AbortController().signal, hooks);
    expect(mocks.call).toHaveBeenCalledTimes(1); expect(mocks.search).not.toHaveBeenCalled(); expect(p.design.html).toBe(d.html);
  });
  it("搜索成功但没取得正文时保留检查点，不能记为完成", async () => {
    const p = reconstructionFixture(); p.options.research = true; p.design = null; p.reconstruction.draft = []; p.reconstruction.queries = [{ query: "参考资料", done: false, results: [] }];
    mocks.search.mockReset().mockResolvedValue([{ title: "仅搜索摘要", url: "https://example.org/r" }]); mocks.capture.mockRejectedValue(new Error("网络失败"));
    mocks.call.mockReset().mockResolvedValue({urls:["https://example.org/r"]});
    const hooks = { checkpoint: vi.fn(), stage: vi.fn(), request: vi.fn() };
    await expect(runReconstruction(p, { model: "test" } as any, new AbortController().signal, hooks)).rejects.toThrow("未取得有效正文");
    expect(p.reconstruction.queries[0].done).toBe(true); expect(p.reconstruction.references[0].status).toBe("failed"); expect(p.reconstruction.researchComplete).not.toBe(true);
    await expect(runReconstruction(p, { model: "test" } as any, new AbortController().signal, hooks)).rejects.toThrow(); expect(mocks.search).toHaveBeenCalledTimes(1);
    validateReadingReconstruction(p.reconstruction);
  });
  it("筛选实际资料后允许部分网页失败，成功研究检查点不会重复搜索", async () => {
    const p = reconstructionFixture(), design = p.design, interactions=p.reconstruction.interactions;
    p.design=null; p.reconstruction.draft=[]; p.options.research=true;
    p.reconstruction.queries=[{query:"直接资料",done:false,results:[]}];
    mocks.search.mockReset().mockResolvedValue([{title:"官方资料",url:"https://example.org/official",text:"有效正文".repeat(100)},{title:"失败网页",url:"https://example.org/failed"}]);
    mocks.capture.mockReset().mockRejectedValue(new Error("抓取失败"));
    mocks.call.mockReset().mockResolvedValueOnce({urls:["https://example.org/official","https://example.org/failed"]}).mockResolvedValueOnce({adequate:true}).mockResolvedValueOnce({title:"章节",markdown:"扩写的新专题",referenceIds:["R1"]}).mockResolvedValueOnce({...design,interactions});
    const hooks={checkpoint:vi.fn(),stage:vi.fn(),request:vi.fn()};
    await runReconstruction(p,{model:"test"} as any,new AbortController().signal,hooks);
    expect(p.reconstruction.researchComplete).toBe(true); expect(p.reconstruction.references.map(r=>r.status)).toEqual(["ready","failed"]);
    expect(hooks.request.mock.calls.filter(c=>c[0]==="pagesRead")).toHaveLength(1);
    p.design=null; mocks.call.mockResolvedValue({...design,interactions});
    await runReconstruction(p,{model:"test"} as any,new AbortController().signal,hooks);
    expect(mocks.search).toHaveBeenCalledTimes(1); expect(mocks.capture).toHaveBeenCalledTimes(2);
  });
});
