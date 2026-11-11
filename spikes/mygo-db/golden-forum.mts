// 论坛连接器基线：把真实 forum-note / forum-summary / v2ex / discourse / twolibra / forum-post
// （经 esbuild 打包，出站抓取、用量记账换成桩）跑在一组夹具上，录制输出，供 Go 端逐类对拍。
// 用法：run-golden-forum.mts <输出目录>（从 D:\Project\GuiZhi-mygo-spike 目录运行 tsx）。
import { build } from "esbuild";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const stubs = path.join(here, "forum-stubs");
const bundlePath = path.join(here, `.forum-bundle-${process.pid}.mjs`);
await build({
  entryPoints: [path.join(here, "forum-entry.ts")],
  outfile: bundlePath,
  bundle: true,
  format: "esm",
  banner: { js: "import { createRequire as __gzCreateRequire } from 'node:module'; const require = __gzCreateRequire(import.meta.url);" },
  platform: "node",
  target: "node20",
  logLevel: "error",
  packages: "external",
  plugins: [
    {
      name: "forum-stubs",
      setup(b) {
        b.onResolve({ filter: /import\/safe-fetch$|^\.\/safe-fetch$/ }, () => ({ path: path.join(stubs, "safe-fetch.ts") }));
        b.onResolve({ filter: /\/ai-usage$/ }, () => ({ path: path.join(stubs, "ai-usage.ts") }));
      },
    },
  ],
});
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const T = (await import(pathToFileURL(bundlePath).href)) as any;

// ---------- 夹具 ----------
type Outcome = { json?: unknown; error?: string };
type Fixture = Record<string, Outcome | Outcome[]>;

interface Rec {
  kind: string;
  name: string;
  input: unknown;
  output: unknown;
}
const recs: Rec[] = [];
const add = (kind: string, name: string, input: unknown, output: unknown) => recs.push({ kind, name, input, output });

const NOW = Date.now();
/** createdAt 回退到“当前时间”的值不可对拍，统一成 -1 */
function norm(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(norm);
  if (v && typeof v === "object") {
    return Object.fromEntries(
      Object.entries(v as Record<string, unknown>).map(([k, x]) => [
        k,
        k === "createdAt" && typeof x === "number" && Math.abs(x - NOW) < 600_000 ? -1 : norm(x),
      ]),
    );
  }
  return v;
}

function makeFetcher(fx: Fixture) {
  const calls: string[] = [];
  const counters: Record<string, number> = {};
  const fetchJson = async (url: string) => {
    calls.push(url);
    const entry = fx[url];
    if (!entry) throw new Error("HTTP 404");
    const list = Array.isArray(entry) ? entry : [entry];
    const i = Math.min(counters[url] ?? 0, list.length - 1);
    counters[url] = (counters[url] ?? 0) + 1;
    const o = list[i];
    if (o.error !== undefined) throw new Error(o.error);
    return structuredClone(o.json);
  };
  return { fetchJson, calls };
}

// ---------- 1. 纯函数：清洗 / 判题 / 分块 ----------
const sanitizeInputs = [
  "## 讨论总结\n\n开头两句。\n\n# 方案一\n- 要点\n\n**方案二：**\n文字\n\n---\n\n## **共识与分歧**：\n- 一致",
  "```markdown\n总结\n正文\n```",
  "**总结**\n内容\n\n\n\n\n尾",
  "  \n\n内容总结：\n正文",
  "- **标签**：不应变成标题\n**整行加粗**\n###   已是标题  ",
  "",
  "---\n---",
];
for (const s of sanitizeInputs) add("sanitize", "sanitize", s, T.sanitizeForumSummary(s));

const titles = [
  "求推荐", "问个问题，急", "V2EX 帖子 123456", "v2ex主题789", "求推荐一个适合小团队的项目管理工具", "有人用过吗", "Go 并发模式", "", "   ",
  "各位大佬 请教一下", "一二三四五六七八", "一二三四五六七", "大家有没有人在线等！！！", "Rust 与 Go 的取舍：2024 年选型笔记",
];
for (const s of titles) add("needsTitle", "needsTitle", s, T.needsAiTitle(s));

const mk = (n: number, len: number) => Array.from({ length: n }, (_, i) => ({ floor: i + 1, author: `用户${i}`, content: "字".repeat(len) + i }));
for (const [n, len, max] of [[0, 10, 100], [3, 10, 100], [5, 60, 100], [4, 200, 100], [6, 30, 120]] as const) {
  add("chunks", `chunks-${n}-${len}-${max}`, { replies: mk(n, len), maxChars: max }, T.splitReplyChunks(mk(n, len), max));
}

// ---------- 2. 讨论总结（脚本化 chat） ----------
async function runSummary(name: string, input: { title: string; content: string; replies: ReturnType<typeof mk> }, outputs: (string | { error: string })[]) {
  const calls: unknown[] = [];
  let idx = 0;
  const chat = async (_cfg: unknown, messages: unknown[], opts: Record<string, unknown>) => {
    calls.push({ messages, temperature: opts.temperature, maxTokens: opts.maxTokens, timeoutMs: opts.timeoutMs });
    const o = outputs[Math.min(idx++, outputs.length - 1)];
    if (typeof o !== "string") throw new Error(o.error);
    return { content: o, finishReason: "stop", usage: { promptTokens: 1, completionTokens: 2 } };
  };
  let result: unknown, error: string | undefined;
  try {
    result = await T.generateForumSummary(input, { provider: "custom", apiProtocol: "openai", apiKey: "k", apiUrl: "http://x", model: "m" }, { chat });
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  }
  add("summary", name, { input, outputs }, { result: result ?? null, error: error ?? null, calls });
}
await runSummary("single", { title: "Go 并发模式的选择与取舍", content: "主楼内容", replies: mk(3, 20) }, ["## 总结\n\n- 结论\n\n**方案A**\n说明"]);
await runSummary("single-weak-title", { title: "求推荐", content: "", replies: mk(2, 20) }, ["标题：一个足够清楚的新标题\n\n正文总结\n- 要点"]);
await runSummary("single-weak-title-no-protocol", { title: "求推荐", content: "x", replies: mk(2, 20) }, ["没有标题行的总结"]);
await runSummary("empty", { title: "T", content: "", replies: [] }, ["x"]);
await runSummary("blank-output", { title: "有足够长度的标题", content: "", replies: mk(1, 5) }, ["   "]);
await runSummary("only-rules", { title: "有足够长度的标题", content: "", replies: mk(1, 5) }, ["---\n---"]);
await runSummary("error", { title: "有足够长度的标题", content: "", replies: mk(1, 5) }, [{ error: "HTTP 500" }]);
await runSummary("mapreduce", { title: "有足够长度的标题", content: "主楼", replies: mk(40, 700) }, ["片段一", "片段二", "片段三", "归并后的总结\n- 点"]);
await runSummary("mapreduce-weak-title", { title: "急", content: "", replies: mk(40, 700) }, ["n1", "n2", "n3", "标题：归并标题\n\n正文"]);
await runSummary("mapreduce-truncate", { title: "有足够长度的标题", content: "", replies: mk(400, 700) }, ["n"]);

// ---------- 3. 抓取器 ----------
const NO_RETRY = [] as number[];
interface FetchCase {
  name: string;
  platform: "v2ex" | "appinn" | "linuxdo" | "twolibra";
  topicId: string;
  fixture: Fixture;
  auth?: Fixture; // fetchAuthenticatedJson 的夹具（存在即注入）
  retryDelays?: number[];
  pageSize?: number;
  maxPages?: number;
}
const fetchCases: FetchCase[] = [];
const V = "https://www.v2ex.com/api";
const vt = (id: string) => `${V}/topics/show.json?id=${id}`;
const vr = (id: string) => `${V}/replies/show.json?topic_id=${id}`;

fetchCases.push({
  name: "v2ex-full", platform: "v2ex", topicId: "1001",
  fixture: {
    [vt("1001")]: { json: [{ title: " 标题\r\n二 ", content: "正文\r\n第二行\r", member: { username: " alice " }, node: { title: " 节点 ", name: "n" }, created: 1700000000, replies: 9, url: " https://www.v2ex.com/t/1001#reply9 " }] },
    [vr("1001")]: { json: [
      { content: "r1\r\n", member: { username: " bob " }, created: 1700000100 },
      { content: "   ", member: { username: "x" }, created: 1700000200 },
      { content: "r3", member: {}, created: 0 },
      { content: "r4", created: 1700000400 },
    ] },
  },
});
fetchCases.push({ name: "v2ex-minimal", platform: "v2ex", topicId: "1002", fixture: { [vt("1002")]: { json: [{ node: { name: "只有 name" }, created: 1700000000 }] }, [vr("1002")]: { json: "not-array" } } });
fetchCases.push({ name: "v2ex-empty", platform: "v2ex", topicId: "1003", fixture: { [vt("1003")]: { json: [] } } });
fetchCases.push({ name: "v2ex-not-array", platform: "v2ex", topicId: "1004", fixture: { [vt("1004")]: { json: { x: 1 } } } });
fetchCases.push({ name: "v2ex-replies-fail", platform: "v2ex", topicId: "1005", retryDelays: NO_RETRY, fixture: { [vt("1005")]: { json: [{ title: "T", created: 1700000000 }] }, [vr("1005")]: { error: "HTTP 500" } } });
for (const [i, e] of ["HTTP 404", "HTTP 403", "HTTP 429", "HTTP 503", "请求超时", "连接被中断", "奇怪的错误", "已取消"].entries()) {
  const id = `11${i}`;
  fetchCases.push({ name: `v2ex-err-${e}`, platform: "v2ex", topicId: id, retryDelays: NO_RETRY, fixture: { [vt(id)]: { error: e } } });
}
fetchCases.push({ name: "v2ex-retry-ok", platform: "v2ex", topicId: "1201", retryDelays: [0, 0], fixture: { [vt("1201")]: [{ error: "HTTP 502" }, { error: "请求超时" }, { json: [{ title: "重试成功", created: 1700000000 }] }], [vr("1201")]: { json: [] } } });
fetchCases.push({ name: "v2ex-retry-exhausted", platform: "v2ex", topicId: "1202", retryDelays: [0, 0], fixture: { [vt("1202")]: { error: "HTTP 502" } } });

// Discourse
const dsOrigin = (p: string) => (p === "linuxdo" ? "https://linux.do" : "https://meta.appinn.net");
function discourseCase(name: string, platform: "appinn" | "linuxdo", topicId: string, topic: unknown, extra: Fixture = {}, more: Partial<FetchCase> = {}): FetchCase {
  return { name, platform, topicId, retryDelays: NO_RETRY, fixture: { [`${dsOrigin(platform)}/t/topic/${topicId}.json`]: { json: topic }, ...extra }, ...more };
}
const post = (id: number, n: number, extra: Record<string, unknown> = {}) => ({ id, post_number: n, username: `user${n}`, created_at: "2024-05-01T12:00:00.000Z", raw: `楼 ${n} 的内容`, ...extra });
const topicBase = {
  title: " Discourse 帖子标题 ", posts_count: 5, created_at: "2024-05-01T10:00:00.000Z",
  details: { created_by: { name: " 楼主名 ", username: "op" }, category: { name: " 分类甲 " } },
};
add; // keep lints quiet
fetchCases.push(discourseCase("ds-appinn-full", "appinn", "2001", {
  ...topicBase,
  post_stream: {
    posts: [post(10, 1, { raw: "主楼\r\n正文" }), post(11, 2, { raw: "二楼" })],
    stream: [10, 11, 12, 13, 14],
  },
}, {
  "https://meta.appinn.net/t/2001/posts.json?post_ids[]=12&post_ids[]=13&post_ids[]=14": { json: { post_stream: { posts: [
    post(13, 4, { raw: "", cooked: "<p>四楼 cooked</p>", reply_to_post_number: 2 }),
    post(12, 3, { reply_to_post_number: 99 }),
    post(14, 5, { raw: "", cooked: "   ", reply_to_post_number: 3 }),
  ] } } },
}));
fetchCases.push(discourseCase("ds-linuxdo-category-site", "linuxdo", "2002", {
  title: "", category_id: 7, tags: ["t1", "t2"], posts_count: 1,
  post_stream: { posts: [post(1, 1, { username: "", name: "显示名" })], stream: [1] },
}, { "https://linux.do/site.json": { json: { categories: [{ id: 6, name: "x" }, { id: 7, name: " 站点分类 " }] } } }));
fetchCases.push(discourseCase("ds-tags-fallback", "linuxdo", "2003", {
  title: "标签回退", category_id: 9, tags: ["a", "", "b"],
  post_stream: { posts: [post(1, 1)], stream: [1] },
}, { "https://linux.do/site.json": { error: "HTTP 500" } }));
fetchCases.push(discourseCase("ds-batch-fail-partial", "appinn", "2004", {
  title: "补拉失败", post_stream: { posts: [post(1, 1), post(2, 3)], stream: [1, 5, 2] },
}, { "https://meta.appinn.net/t/2004/posts.json?post_ids[]=5": { error: "HTTP 500" } }));
fetchCases.push(discourseCase("ds-no-stream", "appinn", "2005", {
  title: "无 stream", post_stream: { posts: [post(3, 3), post(1, 1), post(2, 2)] },
}));
fetchCases.push(discourseCase("ds-cooked-html", "appinn", "2006", {
  title: "cooked HTML", post_stream: { posts: [
    post(1, 1, { raw: "", cooked: "<p>第一段 <strong>加粗</strong> 和 <em>斜体</em></p><p>第二段 <a href=\"https://e.com/x\">链接</a></p><script>alert(1)</script><iframe src=\"x\"></iframe>" }),
    post(2, 2, { raw: "", cooked: "<p>引用 <img src=\"https://e.com/a.png\" alt=\"图\"></p>", reply_to_post_number: 1 }),
  ], stream: [1, 2] },
}));
fetchCases.push(discourseCase("ds-cooked-rich", "appinn", "2009", {
  title: "cooked 富文本", post_stream: { posts: [
    post(1, 1, { raw: "", cooked: "<h2>小标题</h2><ul><li>一</li><li>二<ul><li>二点一</li></ul></li></ul><ol><li>甲</li><li>乙</li></ol><blockquote><p>引用</p></blockquote><pre><code class=\"lang-js\">const a = 1;\nconsole.log(a);</code></pre><p>行内 <code>x</code><br>换行</p><hr><table><tr><td>格</td></tr></table>" }),
  ], stream: [1] },
}));
fetchCases.push(discourseCase("ds-errors-field", "appinn", "2007", { errors: ["不存在", "已删除"] }));
fetchCases.push(discourseCase("ds-no-posts", "appinn", "2008", { title: "空", post_stream: { posts: [], stream: [] } }));
for (const [p, e] of [["linuxdo", "HTTP 403"], ["appinn", "HTTP 403"], ["linuxdo", "HTTP 401"], ["appinn", "HTTP 401"], ["appinn", "HTTP 404"], ["appinn", "HTTP 503"], ["appinn", "请求超时"], ["appinn", "连接被中断"], ["appinn", "接口未返回 JSON"], ["appinn", "已取消"]] as const) {
  const id = "21" + Math.abs([...(p + e)].reduce((a, c) => a * 31 + c.charCodeAt(0), 7)) % 1000;
  fetchCases.push({ name: `ds-err-${p}-${e}`, platform: p, topicId: id, retryDelays: NO_RETRY, fixture: { [`${dsOrigin(p)}/t/topic/${id}.json`]: { error: e } } });
}
fetchCases.push({
  name: "ds-auth-fallback-ok", platform: "linuxdo", topicId: "2301", retryDelays: NO_RETRY,
  fixture: { "https://linux.do/t/topic/2301.json": { error: "HTTP 403" } },
  auth: { "https://linux.do/t/topic/2301.json": { json: { title: "登录后可见", post_stream: { posts: [post(1, 1)], stream: [1] } } } },
});
fetchCases.push({
  name: "ds-auth-fallback-fail", platform: "linuxdo", topicId: "2302", retryDelays: NO_RETRY,
  fixture: { "https://linux.do/t/topic/2302.json": { error: "接口未返回 JSON" } },
  auth: { "https://linux.do/t/topic/2302.json": { error: "HTTP 401" } },
});
fetchCases.push({
  name: "ds-retry-ok", platform: "appinn", topicId: "2303", retryDelays: [0, 0],
  fixture: { "https://meta.appinn.net/t/topic/2303.json": [{ error: "HTTP 502" }, { json: { title: "重试成功的帖子", post_stream: { posts: [post(1, 1)], stream: [1] } } }] },
});

// 2Libra
const LB = "https://2libra.com/api";
const lbPost = `${LB}/posts/abc`;
const lbPage = (id: string, page: number, limit = 50) => `${LB}/comments/list/flat?post_short_id=${id}&page=${page}&limit=${limit}`;
const lbBase = { short_id: " abc ", title: " 2Libra 标题 ", content: "主楼\r\n内容", author: { username: " 楼主 " }, node: { name: " 节点名 ", slug: " s l " }, created_at: "2024-05-01T12:00:00Z", comment_count: 7 };
fetchCases.push({
  name: "lb-full", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, pageSize: 3,
  fixture: {
    [lbPost]: { json: { c: 0, d: lbBase } },
    [lbPage("abc", 1, 3)]: { json: { c: 0, d: { total_pages: 2, total: 5, items: [
      { id: "1", content: "一楼", author: { username: "甲" }, flat_floor: 1, created_at: "2024-05-01T12:01:00Z" },
      { id: "1", content: "重复" },
      { id: "2", content: "已删除", is_deleted: true },
      { id: "3", content: "  ", author: { username: "空" } },
      { id: " 4 ", content: "匿名", alias_id: "al", floor: 4, reply_comment: { content: "被回复<br/>内容", author: { username: "甲" }, flat_floor: 1 } },
    ] } } },
    [lbPage("abc", 2, 3)]: { json: { c: 0, d: { total_pages: 2, items: [
      { content: "无 id 且无楼层", created_at: "bad" },
      { id: "6", content: "父评论", parent: { content: "p", is_deleted: true } },
      { id: "7", content: "父评论2", parent: { content: "", author: { username: "" } } },
      { id: "8", content: "父评论3", parent: { content: "只有内容", floor: 2 } },
      { id: "9", content: "父评论4", reply_comment: { content: "", author: { username: "乙" }, floor: null } },
    ] } } },
  },
});
fetchCases.push({ name: "lb-page-fail", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, pageSize: 3, fixture: {
  [lbPost]: { json: { c: 0, d: { ...lbBase, comment_count: undefined } } },
  [lbPage("abc", 1, 3)]: { json: { c: 0, d: { total_pages: 3, total: 11, items: [{ id: "1", content: "一" }] } } },
  [lbPage("abc", 2, 3)]: { error: "HTTP 500" },
} });
fetchCases.push({ name: "lb-over-limit", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, pageSize: 2, maxPages: 1, fixture: {
  [lbPost]: { json: { c: 0, d: { ...lbBase, short_id: undefined, comment_count: undefined } } },
  [lbPage("abc", 1, 2)]: { json: { c: 0, d: { total_pages: 5, total: 9, items: [{ id: "1", content: "一" }, { id: "2", content: "二" }] } } },
} });
fetchCases.push({ name: "lb-unavailable-comments", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, fixture: {
  [lbPost]: { json: { c: 0, d: { ...lbBase, node: { slug: "only-slug" }, author: undefined } } },
  [lbPage("abc", 1)]: { json: { c: 1, m: " 评论被关闭 " } },
} });
fetchCases.push({ name: "lb-unwrap-error", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, fixture: { [lbPost]: { json: { c: 1, m: " 帖子被屏蔽\r\n " } } } });
fetchCases.push({ name: "lb-unwrap-error-nomsg", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, fixture: { [lbPost]: { json: { c: 0, d: null } } } });
fetchCases.push({ name: "lb-missing-title", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, fixture: { [lbPost]: { json: { c: 0, d: { ...lbBase, title: "  " } } } } });
fetchCases.push({ name: "lb-missing-slug", platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, fixture: { [lbPost]: { json: { c: 0, d: { ...lbBase, node: { name: "x" } } } } } });
for (const e of ["HTTP 404", "HTTP 403", "HTTP 429", "HTTP 503", "请求超时", "连接被中断", "未知错误", "已取消"]) {
  fetchCases.push({ name: `lb-err-${e}`, platform: "twolibra", topicId: "abc", retryDelays: NO_RETRY, fixture: { [lbPost]: { error: e } } });
}
fetchCases.push({ name: "lb-retry-429", platform: "twolibra", topicId: "abc", retryDelays: [0, 0], fixture: {
  [lbPost]: [{ error: "HTTP 429" }, { json: { c: 0, d: lbBase } }],
  [lbPage("abc", 1)]: { json: { c: 0, d: { items: [] } } },
} });

for (const c of fetchCases) {
  const { fetchJson, calls } = makeFetcher(c.fixture);
  const auth = c.auth ? makeFetcher(c.auth) : undefined;
  const deps: Record<string, unknown> = { fetchJson, retryDelaysMs: c.retryDelays ?? NO_RETRY };
  if (auth) deps.fetchAuthenticatedJson = auth.fetchJson;
  if (c.pageSize) deps.commentPageSize = c.pageSize;
  if (c.maxPages) deps.maxCommentPages = c.maxPages;
  let thread: unknown, error: string | undefined;
  try {
    const fn = { v2ex: T.fetchV2exThread, appinn: T.fetchAppinnThread, linuxdo: T.fetchLinuxdoThread, twolibra: T.fetchTwolibraThread }[c.platform];
    thread = await fn(c.topicId, deps);
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  }
  add("thread", c.name, { platform: c.platform, topicId: c.topicId, fixture: c.fixture, auth: c.auth ?? null, retryDelays: c.retryDelays ?? [], pageSize: c.pageSize ?? 0, maxPages: c.maxPages ?? 0 },
    { thread: norm(thread ?? null), error: error ?? null, calls, authCalls: auth?.calls ?? [] });
}

// ---------- 4. 小节读写：用组装出来的条目做输入 ----------
const noteThread = {
  platform: "v2ex", topicId: "9", title: "笔记帖子", author: "楼主", node: "技术", createdAt: Date.UTC(2024, 4, 1, 4, 0, 0), replyCount: 3,
  content: "主楼第一行\n\n第二段",
  replies: [
    { floor: 1, author: "甲", content: "一楼内容\n多行", createdAt: 1 },
    { floor: 2, author: "乙", content: "二楼回复甲", createdAt: 2, replyTo: { author: "甲", floor: 1, snippet: "一楼内容 多行" } },
    { floor: 3, author: "", content: "匿名三楼", createdAt: 3, replyTo: { author: "丙", snippet: "无楼层引用" } },
  ],
  webpageUrl: "https://www.v2ex.com/t/9",
};
const post0 = await T.extractForumPost({ platform: "v2ex", topicId: "9" }, { fetchThread: async () => noteThread, getSummaryConfig: () => null });
const post1 = await T.extractForumPost({ platform: "v2ex", topicId: "9" }, { fetchThread: async () => noteThread, getSummaryConfig: () => ({ model: "m" }), summarize: async () => ({ summary: "旧总结\n\n### 方案\n- 要点", title: null }) });
const noteContents: string[] = [post0.content, post1.content, "没有任何小节的纯文本", "## 正文\n\n只有正文\n\n## 讨论（1 条）\n\n**1 楼 · 旧作者**\n旧格式回复\n\n**2 楼 · 乙**\n> 回复 @旧作者（1 楼）：摘录\n回复正文", ""];
for (const [i, content] of noteContents.entries()) {
  const replies = T.parseForumReplies(content);
  add("noteOps", `note-${i}`, { content }, {
    sections: T.splitForumNoteSections(content),
    replies,
    sectionReplies: T.parseForumReplySection(T.splitForumNoteSections(content).replies),
    filtered: ["", "乙", "1 楼", "回复", "不存在的关键字", "多行"].map((q) => ({ q, floors: T.filterForumReplies(replies, q).map((r: { floor: number }) => r.floor) })),
    resolve: [{ author: "甲", floor: 1 }, { author: "乙" }, { author: "" }, { author: "不存在", floor: 9 }, { author: "旧作者" }].map((rt) => T.resolveReplyTargetFloor(replies, rt)),
    upsert: T.upsertForumSummarySection(content, "新的总结\n\n- 点一\n- 点二"),
    replace: T.replaceForumRepliesSection(content, [{ floor: 1, author: "新甲", content: "新内容" }, { floor: 2, author: "新乙", content: "回复", replyTo: { author: "新甲", floor: 1, snippet: "新内容" } }]),
    replaceEmpty: T.replaceForumRepliesSection(content, []),
  });
}
for (const s of ["a<br/>b", "a<BR />b  <p>c</p>", "  多 \n 空白\t折叠  ", "x".repeat(300), "😀".repeat(150)]) add("snippet", "snippet", s, T.normalizeForumSnippet(s));
add("replyBlock", "replyBlock", null, [
  T.formatForumReplyBlock({ floor: 1, author: "甲", content: "内容" }),
  T.formatForumReplyBlock({ floor: 2, author: "", content: "匿名", replyTo: { author: " 乙 ", floor: 1, snippet: "摘<br>要" } }),
  T.formatForumReplyBlock({ floor: 3, author: "丙", content: "无作者引用", replyTo: { author: "", snippet: "" } }),
  T.formatForumReplyBlock({ floor: 4, author: "丁", content: "仅摘要", replyTo: { author: "戊", snippet: "x" } }),
]);

// ---------- 5. 条目组装 ----------
interface PostCase {
  name: string;
  target: { platform: string; topicId: string };
  thread?: unknown; // 抓取成功
  fetchError?: string;
  cfg: boolean; // 是否配置了摘要模型
  summarize?: { summary: string; title: string | null } | null | { error: string };
}
const baseThread = (over: Record<string, unknown> = {}) => ({ ...noteThread, ...over });
const postCases: PostCase[] = [
  { name: "post-no-config", target: { platform: "v2ex", topicId: "9" }, thread: baseThread(), cfg: false },
  { name: "post-summary-ok", target: { platform: "v2ex", topicId: "9" }, thread: baseThread(), cfg: true, summarize: { summary: "总结正文\n\n### 小标题\n- 点", title: null } },
  { name: "post-summary-new-title", target: { platform: "v2ex", topicId: "9" }, thread: baseThread({ title: "求推荐" }), cfg: true, summarize: { summary: "总结", title: "AI 拟的新标题" } },
  { name: "post-summary-same-title", target: { platform: "v2ex", topicId: "9" }, thread: baseThread({ title: "同名标题" }), cfg: true, summarize: { summary: "总结", title: "同名标题" } },
  { name: "post-summary-null", target: { platform: "v2ex", topicId: "9" }, thread: baseThread(), cfg: true, summarize: null },
  { name: "post-summary-error", target: { platform: "v2ex", topicId: "9" }, thread: baseThread(), cfg: true, summarize: { error: "模型 500：" + "错".repeat(200) } },
  { name: "post-no-replies", target: { platform: "appinn", topicId: "9" }, thread: baseThread({ platform: "appinn", replies: [], replyCount: 0, node: "", author: "" }), cfg: true, summarize: { summary: "不会被调用", title: null } },
  { name: "post-empty-body", target: { platform: "linuxdo", topicId: "9" }, thread: baseThread({ platform: "linuxdo", content: "" }), cfg: false },
  { name: "post-op-only", target: { platform: "nga", topicId: "9" }, thread: baseThread({ platform: "nga", replyRetention: "op-only", replyCount: 120, summaryReplies: [{ floor: 5, author: "路人", content: "采样楼层", createdAt: 1 }] }), cfg: true, summarize: { summary: "采样总结", title: null } },
  { name: "post-op-only-no-config", target: { platform: "nga", topicId: "9" }, thread: baseThread({ platform: "nga", replyRetention: "op-only", replyCount: 120 }), cfg: false },
  { name: "post-warning", target: { platform: "twolibra", topicId: "9" }, thread: baseThread({ platform: "twolibra", warningReason: "评论第 2 页抓取失败（HTTP 500），讨论可能不完整" }), cfg: false },
  { name: "post-fetch-error", target: { platform: "v2ex", topicId: "9" }, fetchError: "HTTP 500", cfg: false },
  { name: "post-fetch-cancelled", target: { platform: "v2ex", topicId: "9" }, fetchError: "已取消", cfg: false },
  { name: "post-nodeseek-unready", target: { platform: "nodeseek", topicId: "9" }, cfg: false },
];
for (const c of postCases) {
  const stages: string[] = [];
  const summarizeCalls: unknown[] = [];
  const deps: Record<string, unknown> = {
    onStage: (s: string) => stages.push(s),
    getSummaryConfig: () => (c.cfg ? { model: "m" } : null),
    summarize: async (input: unknown) => {
      summarizeCalls.push(input);
      const r = c.summarize;
      if (r && "error" in r) throw new Error(r.error);
      return r ?? null;
    },
  };
  if (c.thread) deps.fetchThread = async () => structuredClone(c.thread);
  else if (c.fetchError) deps.fetchThread = async () => { throw new Error(c.fetchError); };
  let result: unknown, error: string | undefined;
  try {
    result = await T.extractForumPost(c.target, deps);
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  }
  add("post", c.name, { target: c.target, thread: c.thread ?? null, fetchError: c.fetchError ?? null, cfg: c.cfg, summarize: c.summarize === undefined ? "undefined" : c.summarize },
    { result: result ?? null, error: error ?? null, stages, summarizeCalls });
}

// ---------- 6. NGA ----------
const { addNgaCases } = await import(pathToFileURL(path.join(here, "nga-cases.mts")).href);
await addNgaCases(T, add, norm);

fs.rmSync(bundlePath, { force: true });
export const forumGolden = { recs };
