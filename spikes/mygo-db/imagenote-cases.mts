// 图文条目与 OCR 请求差分夹具：真实 image-note-entry.ts / ocr-request.ts（tsx 直接加载）。
// 下载、落盘、OCR、拟题全部是脚本化替身。由 golden-video.mts 动态引入；kind 均以 "imgnote" / "ocr" 开头。
import path from "node:path";

/* eslint-disable @typescript-eslint/no-explicit-any */
type Add = (kind: string, name: string, input: unknown, output: unknown) => void;
const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));

const baseSource = (over: Record<string, unknown> = {}) => ({
  platformLabel: "抖音", title: "标题", authoredTitle: false, description: "", author: "作者",
  imageMirrors: [["https://a/1.jpg", "https://b/1.jpg"]], webpageUrl: "https://v.example/1", ...over,
});
const mirrors = (n: number) => Array.from({ length: n }, (_, i) => [`https://cdn/${i}.jpg`]);

interface Scenario {
  name: string;
  source: ReturnType<typeof baseSource>;
  downloadFail?: Record<number, string>;
  saveFail?: Record<number, string>;
  ocr: null | { texts: (string | { error: string })[] };
  titleCfg?: boolean;
  genTitle?: string | null | { error: string };
}

const scenarios: Scenario[] = [
  { name: "single-no-ocr", source: baseSource(), ocr: null },
  { name: "single-ocr-ok", source: baseSource({ description: "第一行\n\n# 标题样行\n1. 列表样行" }), ocr: { texts: ["图里的字"] }, titleCfg: true, genTitle: "AI 标题" },
  { name: "multi-ocr-partial-fail", source: baseSource({ imageMirrors: mirrors(3), authoredTitle: true, title: "作者标题", description: "文案" }), ocr: { texts: ["甲", { error: "额度用完" }, "丙"] } },
  { name: "multi-ocr-all-fail", source: baseSource({ imageMirrors: mirrors(2) }), ocr: { texts: [{ error: "x".repeat(200) }, { error: "第二个" }] } },
  { name: "download-fail", source: baseSource({ imageMirrors: mirrors(3) }), downloadFail: { 1: "HTTP 403" }, ocr: { texts: ["零", "二"] } },
  { name: "save-fail", source: baseSource({ imageMirrors: mirrors(2) }), saveFail: { 0: "磁盘已满" }, ocr: null },
  { name: "limit-11", source: baseSource({ imageMirrors: mirrors(11), author: "" }), ocr: { texts: Array.from({ length: 9 }, (_, i) => `文字${i}`) } },
  { name: "title-gen-null", source: baseSource({ description: "只有文案" }), ocr: { texts: ["图字"] }, titleCfg: true, genTitle: null },
  { name: "title-gen-error", source: baseSource(), ocr: { texts: ["图字"] }, titleCfg: true, genTitle: { error: "模型挂了" } },
  { name: "title-cfg-no-material", source: baseSource({ title: "  ", description: "" }), ocr: null, titleCfg: true, genTitle: "不该调用" },
  { name: "ocr-empty-string", source: baseSource({ imageMirrors: mirrors(2) }), ocr: { texts: ["", "有字"] } },
  { name: "authored-skips-title", source: baseSource({ authoredTitle: true, title: "原标题" }), ocr: { texts: ["图字"] }, titleCfg: true, genTitle: "不该用" },
];

export async function addImageNoteCases(add: Add): Promise<void> {
  const N: any = await import("../../apps/desktop/src/main/services/import/image-note-entry.ts");
  const R: any = await import("../../packages/shared/utils/ocr-request.ts");

  const texts = [
    "", "单行", "a\r\nb\n\n  c  \n", "# 标题", "#标题", "####### 七井", "- 列表", "-无空格", "+ 加", "* 星", "> 引用", "```js", "~~~", "---", "-----", "===", "___", "1. 项", "12) 项", "1.无空格", "1234567890. 超长序号", "  3. 缩进后", "  \n\n  ", "正文\n- 项\n1. 项\n> 引",
  ];
  for (const [i, t] of texts.entries()) add("imgnotePlain", `plain-${i}`, t, N.plainTextToMarkdown(t));

  for (const [i, s] of [baseSource(), baseSource({ author: "" }), baseSource({ imageMirrors: mirrors(5), platformLabel: "小红书" })].entries()) {
    add("imgnoteMeta", `meta-${i}`, s, N.buildImageNoteMetaLine(s));
  }

  for (const n of ["a.png", "a.JPG", "a.jpeg", "a.WebP", "a.gif", "a.bmp", "a.tiff", "noext", "x.y.z.jpg", "", ".png"]) add("ocrMime", `mime-${n}`, n, R.imageMimeFromFileName(n));

  const urls = ["data:image/png;base64,QUJD", "data:image/jpeg;base64,", "not-a-data-url", "data:image/png;base64,a\nb"];
  for (const protocol of ["openai", "anthropic", "gemini"]) {
    for (const [i, u] of urls.entries()) add("ocrBody", `body-${protocol}-${i}`, { model: "m<&>", url: u, protocol }, R.buildOcrRequestBody("m<&>", u, protocol));
  }
  const responses: [string, string][] = [
    ["openai", JSON.stringify({ choices: [{ message: { content: "  识别结果  " } }] })],
    ["openai", JSON.stringify({ choices: [{ message: { content: [{ type: "text", text: "甲" }, { text: null }, { text: 5 }, "str", { x: 1 }, { text: true }] } }] })],
    ["openai", JSON.stringify({ choices: [] })],
    ["openai", JSON.stringify({ choices: [null] })],
    ["openai", JSON.stringify({ choices: [{ message: {} }] })],
    ["openai", JSON.stringify({ choices: [{ message: { content: null } }] })],
    ["openai", JSON.stringify({ choices: [{ message: { content: 12 } }] })],
    ["openai", JSON.stringify({})],
    ["openai", JSON.stringify([1])],
    ["openai", "null"],
    ["anthropic", JSON.stringify({ content: [{ type: "text", text: " 甲" }, { type: "text", text: "乙 " }] })],
    ["anthropic", JSON.stringify({ content: "纯串" })],
    ["anthropic", JSON.stringify({ choices: [{ message: { content: "x" } }] })],
    ["anthropic", JSON.stringify([])],
    ["gemini", JSON.stringify({ choices: [{ message: { content: "g" } }] })],
  ];
  for (const [i, [protocol, body]] of responses.entries()) {
    let out: unknown;
    try { out = { ok: R.parseOcrResponse(body, protocol) }; } catch (e) { out = { error: errMsg(e) }; }
    add("ocrParse", `parse-${i}`, { protocol, body }, out);
  }

  for (const s of scenarios) {
    const stages: string[] = [], downloadCalls: unknown[] = [], saveCalls: unknown[] = [], recognizeCalls: unknown[] = [], titleCalls: unknown[] = [];
    let dl = 0;
    const deps: any = {
      onStage: (st: string) => stages.push(st),
      downloadImage: async (m: string[]) => {
        const i = dl++;
        downloadCalls.push(m);
        if (s.downloadFail?.[i]) throw new Error(s.downloadFail[i]);
        return { dir: "", filePath: path.join("/tmp", `dl-${i}.png`) };
      },
      saveAsset: async (fp: string) => {
        const i = Number(/dl-(\d+)/.exec(fp)![1]);
        saveCalls.push(i);
        if (s.saveFail?.[i]) throw new Error(s.saveFail[i]);
        return `asset-${i}.png`;
      },
      getOcrConfig: () => (s.ocr ? { apiUrl: "https://ocr", apiKey: "k", model: "vision" } : null),
      recognize: async (fp: string) => {
        const idx = Number(/asset-(\d+)/.exec(fp)![1]);
        recognizeCalls.push(idx);
        // 脚本按「第几次被识别」取值，与 Go 端同样的约定：按图序号取
        const r = s.ocr!.texts[idx];
        if (r === undefined) return "";
        if (typeof r === "object") throw new Error(r.error);
        return r;
      },
      getTitleConfig: () => (s.titleCfg ? ({ apiUrl: "https://t", apiKey: "k", model: "text" } as any) : null),
      generateTitle: async (material: string) => {
        titleCalls.push(material);
        const r = s.genTitle;
        if (r && typeof r === "object") throw new Error(r.error);
        return r ?? null;
      },
    };
    let out: unknown;
    const warn = console.warn;
    console.warn = () => {};
    try {
      const e = await N.buildImageNoteEntry({ ...s.source, downloadImage: async () => ({ dir: "", filePath: "" }) }, deps);
      out = { ok: { title: e.title, content: e.content, itemType: e.itemType, sourceUri: e.sourceUri }, stages, downloadCalls, saveCalls, recognizeCalls, titleCalls };
    } catch (e) { out = { error: errMsg(e), stages }; } finally { console.warn = warn; }
    add("imgnoteEntry", s.name, s, out);
  }
}
