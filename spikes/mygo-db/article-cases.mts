// 本文问答差分夹具：真实 article-context.ts / reading-source.ts / article-ask-context.ts /
// native-search-response.ts / search-result-url.ts / native-search.ts（tsx 直接加载）。
// articleCtx 用例在临时 SQLite 里写入条目后调用真实 articleContext。
// 由 golden-video.mts 动态引入；kind 均以 "art" 开头，Go 端由 article_context_test.go 对拍。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

/* eslint-disable @typescript-eslint/no-explicit-any */
type Add = (kind: string, name: string, input: unknown, output: unknown) => void;
const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));

const para = (n: number, tag: string) => Array.from({ length: n }, (_, i) => `${tag}第${i}段，讲的是量子计算与 Rust 内存模型的话题。${"填充文字".repeat(40)}`).join("\n\n");

export async function addArticleCases(add: Add): Promise<void> {
  const A: any = await import("../../packages/shared/utils/article-context.ts");
  const S: any = await import("../../packages/shared/utils/reading-source.ts");
  const IMG: any = await import("../../packages/shared/utils/image-note.ts");
  const CTX: any = await import("../../apps/desktop/src/main/services/article-ask-context.ts");
  const NR: any = await import("../../apps/desktop/src/main/services/themed-reading/native-search-response.ts");
  const NS: any = await import("../../apps/desktop/src/main/services/themed-reading/native-search.ts");
  const PU: any = await import("../../apps/desktop/src/main/services/themed-reading/search-result-url.ts");

  const norm = [
    "", "  a  b  ", "![图](http://x/y.png)文字", "[链接](http://a)后", "# 标题 > 引用 *粗* _斜_ `码`", "a\u00a0b\u3000c", "多\n\n行\t文本",
    "![](x)[](y)[a](b)", "[嵌套[内]](u)", "\ufeff前缀", "a\u2028b\u2029c", "![a](b(c)d)",
  ];
  norm.forEach((s, i) => add("artNorm", `norm-${i}`, s, A.normalizeArticleText(s)));

  const long = para(40, "甲") + "\n\n" + "English paragraph about rust ownership and borrow checker. ".repeat(30);
  const sel = "第7段，讲的是量子计算";
  const selCases: { name: string; text: string; question: string; selection?: string; budget?: number }[] = [
    { name: "short", text: "短文", question: "什么" },
    { name: "exact-budget", text: "x".repeat(12000), question: "q" },
    { name: "over-by-one", text: "段落\n\n".repeat(3000), question: "段落" },
    { name: "long-q", text: long, question: "量子计算是什么" },
    { name: "long-q-en", text: long, question: "rust ownership" },
    { name: "long-sel", text: long, question: "解释", selection: sel },
    { name: "long-sel-missing", text: long, question: "解释", selection: "完全不存在的选段" },
    { name: "small-budget", text: long, question: "量子", budget: 1500 },
    { name: "tiny-budget", text: long, question: "量子", budget: 50 },
    { name: "crlf", text: long.replace(/\n/g, "\r\n"), question: "Rust 内存", budget: 3000 },
    { name: "huge-single", text: "长".repeat(5000), question: "长", budget: 2200 },
    { name: "emoji-chunks", text: ("😀".repeat(700) + "\n\n").repeat(6) + "尾部", question: "尾部", budget: 3000 },
    { name: "sel-4000", text: long, question: "x", selection: "量".repeat(4000), budget: 6000 },
    { name: "empty-blocks", text: "\n\n \n\n\u3000\n\n" + "正文".repeat(3000), question: "正文", budget: 2000 },
    { name: "terms-dup", text: long, question: "量子 量子 量子 内存 内存", budget: 2500 },
    { name: "single-char-terms", text: long, question: "a b c 1 2", budget: 2500 },
  ];
  for (const c of selCases) {
    const args = [c.text, c.question, c.selection ?? "", c.budget ?? 12000];
    add("artSelect", c.name, { text: c.text, question: c.question, selection: c.selection ?? "", budget: c.budget ?? 12000 },
      c.budget === undefined && c.selection === undefined ? A.selectArticleContext(c.text, c.question) : A.selectArticleContext(...args));
  }

  const imgContents = [
    "文案\n\n![a](http://x/a.png)\n\n## 图中文字\n\n识别内容", "只有文案", "![a](u) ![b](v)\n\n文案", "文案\n\n## 图中文字\n", "## 图中文字\n\n全文", "", "a\n\n\n\nb\n\n  \n\nc",
  ];
  imgContents.forEach((c, i) => add("artImgSections", `img-${i}`, c, IMG.splitImageNoteSections(c)));

  const meta = "> 平台：抖音 · 作者：甲 · 时长：1:02\n\n正文内容";
  const forum = "> 平台：NGA\n\n## 正文\n\n楼主正文\n\n## 讨论总结\n\n总结文本\n\n## 讨论（共 2 楼）\n\n#1 甲\n内容";
  const rsCases: { content: string; itemType: string; kind: string }[] = [
    { content: meta, itemType: "video", kind: "body" }, { content: meta, itemType: "video", kind: "summary" },
    { content: "纯正文", itemType: "note", kind: "body" }, { content: "纯正文", itemType: "note", kind: "summary" },
    { content: forum, itemType: "forum", kind: "body" }, { content: forum, itemType: "forum", kind: "summary" },
    { content: "文案\n\n![a](u)\n\n## 图中文字\n\nocr", itemType: "image", kind: "body" },
    { content: "> 平台：小红书\n\n文案\n\n![a](u)\n\n## 图中文字\n\nocr", itemType: "image", kind: "body" },
    { content: "无标题论坛", itemType: "forum", kind: "body" }, { content: "无标题论坛", itemType: "forum", kind: "summary" },
  ];
  rsCases.forEach((c, i) => {
    let out: any;
    try { out = { text: S.readingSourceText({ content: c.content, itemType: c.itemType }, c.kind) }; } catch (e) { out = { error: errMsg(e) }; }
    add("artReadingSource", `rs-${i}`, c, out);
  });

  const targets: unknown[] = [
    null, undefined, {}, { itemId: "", view: "body" }, { itemId: "a", view: "nope" }, { itemId: "a", view: "body" },
    { itemId: "a".repeat(201), view: "body" }, { itemId: "a".repeat(200), view: "themed", versionId: "v", sourceKind: "summary" },
    { itemId: "a", view: "body", versionId: 5 }, { itemId: "a", view: "body", versionId: "x".repeat(201) },
    { itemId: "a", view: "body", sourceKind: "bad" }, { itemId: "a", view: "body", sourceKind: 1 },
    { itemId: "a", view: "body", selection: "x".repeat(4001) }, { itemId: "a", view: "body", selection: "x".repeat(4000) },
    { itemId: "a", view: "body", selection: 3 }, { itemId: "a", view: "replies", floor: 12 }, { itemId: "a", view: "replies", floor: 0 },
    { itemId: "a", view: "replies", floor: -3 }, { itemId: "a", view: "replies", floor: 1.5 }, { itemId: "a", view: "replies", floor: "3" },
    { itemId: "a", view: "replies", floor: 9007199254740991 }, { itemId: "a", view: "replies", floor: 9007199254740992 },
    { itemId: 7, view: "body" }, { itemId: "a", view: "body", extra: 1 },
  ];
  targets.forEach((t, i) => {
    let out: any;
    try { out = { ok: CTX.parseArticleTarget(t) }; } catch (e) { out = { error: errMsg(e) }; }
    add("artTarget", `target-${i}`, t === undefined ? "@@undef@@" : t, out);
  });

  // ---- articleContext：临时库 + 真实实现 ----
  const { initDatabase, closeDatabase } = await import("../../packages/db/src/index.ts");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-art-"));
  const db = initDatabase(path.join(dir, "art.db"));
  const items = [
    { id: "n1", title: "笔记一", content: para(30, "乙"), transcript: null, itemType: "note", deletedAt: null },
    { id: "n2", title: "短笔记", content: "很短的正文，包含 关键词。", transcript: null, itemType: "note", deletedAt: null },
    { id: "n3", title: "已删除", content: "x", transcript: null, itemType: "note", deletedAt: 1700000000000 },
    { id: "v1", title: "视频", content: meta, transcript: para(20, "丙") + "\n\nrust 内存模型", itemType: "video", deletedAt: null },
    { id: "v2", title: "无文字稿视频", content: meta, transcript: null, itemType: "video", deletedAt: null },
    { id: "f1", title: "论坛帖", content: forum, transcript: null, itemType: "forum", deletedAt: null },
    { id: "i1", title: "图文", content: "文案很长\n\n![a](u)\n\n## 图中文字\n\n识别出来的文字内容", transcript: null, itemType: "image", deletedAt: null },
    { id: "i2", title: "无 OCR 图文", content: "文案", transcript: null, itemType: "image", deletedAt: null },
  ];
  const ins = db.prepare("INSERT INTO knowledge_items (id,title,content,transcript,item_type,deleted_at,created_at,updated_at) VALUES (?,?,?,?,?,?,1,1)");
  for (const it of items) ins.run(it.id, it.title, it.content, it.transcript, it.itemType, it.deletedAt);
  const ctxCases: { name: string; input: unknown }[] = [
    { name: "body", input: { target: { itemId: "n1", view: "body" }, question: "乙第3段讲什么" } },
    { name: "body-selection", input: { target: { itemId: "n1", view: "body", selection: "第5段，讲的是量子计算" }, question: "解释" } },
    { name: "body-selection-spaced", input: { target: { itemId: "n1", view: "body", selection: "第5段，  讲的是 量子计算" }, question: "解释" } },
    { name: "body-selection-missing", input: { target: { itemId: "n1", view: "body", selection: "不存在的文字" }, question: "解释" } },
    { name: "short", input: { target: { itemId: "n2", view: "body" }, question: "关键词" } },
    { name: "deleted", input: { target: { itemId: "n3", view: "body" }, question: "x" } },
    { name: "missing", input: { target: { itemId: "zz", view: "body" }, question: "x" } },
    { name: "bad-target", input: { target: { itemId: "n1", view: "bad" }, question: "x" } },
    { name: "no-target", input: { question: "x" } },
    { name: "question-too-long", input: { target: { itemId: "n1", view: "body" }, question: "x".repeat(8001) } },
    { name: "question-not-string", input: { target: { itemId: "n1", view: "body" }, question: 5 } },
    { name: "video-body", input: { target: { itemId: "v1", view: "body" }, question: "x" } },
    { name: "video-summary", input: { target: { itemId: "v1", view: "summary" }, question: "x" } },
    { name: "video-transcript", input: { target: { itemId: "v1", view: "transcript" }, question: "rust 内存模型" } },
    { name: "video-transcript-empty", input: { target: { itemId: "v2", view: "transcript" }, question: "x" } },
    { name: "forum-body", input: { target: { itemId: "f1", view: "body" }, question: "x" } },
    { name: "forum-summary", input: { target: { itemId: "f1", view: "summary" }, question: "x" } },
    { name: "forum-replies-floor", input: { target: { itemId: "f1", view: "replies", floor: 3 }, question: "x" } },
    { name: "image-recognized", input: { target: { itemId: "i1", view: "recognized" }, question: "x" } },
    { name: "image-recognized-empty", input: { target: { itemId: "i2", view: "recognized" }, question: "x" } },
    { name: "image-body", input: { target: { itemId: "i1", view: "body" }, question: "x" } },
    { name: "snapshot-no-version", input: { target: { itemId: "n1", view: "snapshot" }, question: "x" } },
    { name: "snapshot-missing-version", input: { target: { itemId: "n1", view: "snapshot", versionId: "nope" }, question: "x" } },
    { name: "themed-no-version", input: { target: { itemId: "n1", view: "themed" }, question: "x" } },
  ];
  for (const c of ctxCases) {
    let out: any;
    try { out = { ok: CTX.articleContext(db, c.input) }; } catch (e) { out = { error: errMsg(e) }; }
    add("artContext", c.name, { items, input: c.input }, out);
  }
  closeDatabase();
  fs.rmSync(dir, { recursive: true, force: true });

  // ---- 原生搜索与公开 URL ----
  const urls: unknown[] = [
    "https://example.com/a", "http://example.com", "ftp://example.com", "https://user:pw@example.com/", "https://example.com#frag", "https://localhost/x", "http://127.0.0.1/x",
    "http://10.0.0.5/", "http://192.168.1.1", "http://[::1]/", "http://[fe80::1]/", "https://169.254.169.254/", "https://foo.local/", "https://foo.internal/", "not a url", "",
    5, null, "https://" + "a".repeat(3990) + ".com", "https://example.com:8443/path?q=1", "HTTPS://EXAMPLE.COM/", "https://xn--fiq228c.com/", "http://0.0.0.0/", "http://2130706433/",
  ];
  urls.forEach((u, i) => add("artPublicUrl", `url-${i}`, u === undefined ? "@@undef@@" : u, PU.isPublicSearchUrl(u)));

  const sse = (events: unknown[], sep = "\n\n") => events.map(e => `data: ${typeof e === "string" ? e : JSON.stringify(e)}`).join(sep) + sep;
  const completed = (extra: Record<string, unknown> = {}) => ({
    status: "completed",
    output: [
      { type: "web_search_call", status: "completed", action: { type: "search", sources: [{ type: "url", url: "https://a.example/1#x", title: " 来源一 " }, { type: "url", url: "http://127.0.0.1/x", title: "内网" }, { type: "other", url: "https://z.example" }] } },
      { type: "message", content: [{ type: "output_text", annotations: [{ type: "url_citation", url: "https://b.example/2", title: "引用二" }, { type: "url_citation", url: "https://a.example/1", title: "重复" }] }] },
    ],
    ...extra,
  });
  const bodies: Record<string, string> = {
    json: JSON.stringify(completed()),
    jsonLeading: "  \n" + JSON.stringify(completed()),
    jsonBad: "{not json",
    sseOk: sse([{ type: "response.created" }, "[DONE]", { type: "response.completed", response: completed() }]),
    sseCrlf: sse([{ type: "response.completed", response: completed() }]).replace(/\n/g, "\r\n"),
    sseError: sse([{ type: "error" }, { type: "response.completed", response: completed() }]),
    sseFailed: sse([{ type: "response.failed" }]),
    sseIncomplete: sse([{ type: "response.incomplete" }]),
    sseNoCompleted: sse([{ type: "response.created" }]),
    sseEmpty: "",
    sseMultiData: "event: x\ndata: {\"type\":\"response.completed\",\ndata: \"response\":{\"status\":\"completed\",\"output\":[]}}\n\n",
    sseNoSpace: "data:{\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n",
    sseBadFrame: sse(["{bad"]),
    notObject: "[1,2]",
    jsonNull: "null",
  };
  const respObj = (o: unknown) => JSON.stringify(o);
  for (const [name, body] of Object.entries(bodies)) {
    let out: any;
    try { out = { response: NR.decodeNativeSearchResponse(body) }; } catch (e) { out = { error: errMsg(e) }; }
    add("artNativeDecode", name, body, out);
  }
  const resultCases: Record<string, unknown> = {
    ok: completed(),
    notCompleted: { ...completed(), status: "in_progress" },
    hasError: { ...completed(), error: { message: "x" } },
    noOutput: { status: "completed" },
    outputNotArray: { status: "completed", output: {} },
    nullResponse: null,
    noSearchCall: { status: "completed", output: [{ type: "message", content: [] }] },
    searchNotCompleted: { status: "completed", output: [{ type: "web_search_call", status: "in_progress", action: { type: "search" } }] },
    mixedSearch: { status: "completed", output: [{ type: "web_search_call", status: "completed", action: { type: "search", sources: [{ type: "url", url: "https://m.example/1" }] } }, { type: "web_search_call", status: "failed", action: { type: "search" } }] },
    openPageOnly: { status: "completed", output: [{ type: "web_search_call", status: "completed", action: { type: "open_page", url: "https://o.example/p" } }] },
    openPageAndSearch: { status: "completed", output: [{ type: "web_search_call", status: "completed", action: { type: "search", sources: [] } }, { type: "web_search_call", status: "completed", action: { type: "open_page", url: "https://o.example/p#h" } }] },
    noPublic: { status: "completed", output: [{ type: "web_search_call", status: "completed", action: { type: "search", sources: [{ type: "url", url: "http://10.0.0.1/" }] } }] },
    manySources: { status: "completed", output: [{ type: "web_search_call", status: "completed", action: { type: "search", sources: Array.from({ length: 9 }, (_, i) => ({ type: "url", url: `https://many.example/${i}`, title: i % 2 ? `标题${i}` : "  " })) } }] },
    longTitle: { status: "completed", output: [{ type: "web_search_call", status: "completed", action: { type: "search", sources: [{ type: "url", url: "https://t.example/", title: "题".repeat(600) }, { type: "url", url: "https://u.example/", title: 5 }] } }] },
    annotationsNotArray: { status: "completed", output: [{ type: "web_search_call", status: "completed", action: { type: "search", sources: [{ type: "url", url: "https://s.example/" }] } }, { type: "message", content: [{ type: "output_text", annotations: "x" }, null, { type: "output_text", annotations: [null, { type: "url_citation", url: "https://c.example/#x", title: "cite" }] }] }] },
    nullItems: { status: "completed", output: [null, 5, { type: "web_search_call", status: "completed", action: { type: "search", sources: [null, { type: "url", url: "https://n.example/" }] } }] },
  };
  for (const [name, r] of Object.entries(resultCases)) {
    let out: any;
    try { out = { results: NR.nativeSearchResults(r) }; } catch (e) { out = { error: errMsg(e) }; }
    add("artNativeResults", name, respObj(r), out);
  }

  const cfgs: Record<string, unknown>[] = [
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://api.openai.com/v1", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://api.openai.com/v1/", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://api.openai.com", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://x.example/v2", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://x.example/v1/responses", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://x.example/v1/chat/completions", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://x.example/api/paas/v4", apiKey: "k", model: "m" },
    { provider: "anthropic", apiProtocol: "anthropic", apiUrl: "https://api.anthropic.com", apiKey: "k", model: "m" },
    { provider: "gemini", apiProtocol: "gemini", apiUrl: "https://generativelanguage.googleapis.com", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "ftp://x.example/v1", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://u:p@x.example/v1", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://x.example/v1?x=1", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "not a url", apiKey: "k", model: "m" },
    { provider: "openai", apiProtocol: "openai", apiUrl: "https://x.example:8443/openai/v1", apiKey: "k", model: "m" },
  ];
  cfgs.forEach((c, i) => {
    let out: any;
    try { out = { endpoint: NS.nativeSearchEndpoint(c).href }; } catch (e) { out = { error: errMsg(e) }; }
    add("artNativeEndpoint", `ep-${i}`, c, out);
  });
}
