// 迁移一致性基准：用归知现有的 TS DAO 生成一份带集合/标签/收藏/归档/回收站/来源平台的隔离库，
// 并把 TS 侧各频道的输出存为 golden.json，供 Go 侧 `go test` 逐项比对。
// 每迁一个频道，就在这里加一组输入，保证 Go 的行为与 TS 逐字段一致。
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";
import {
  initDatabase,
  closeDatabase,
  KnowledgeItemDB,
  CollectionDB,
  TagDB,
  ImportTaskDB,
  AskSessionDB,
  SemanticIndexDB,
} from "../../packages/db/src/index.ts";
import { listInboxItems } from "../../apps/desktop/src/main/services/inbox.ts";
import { buildFtsMatchQuery, segmentTextForFts } from "../../packages/db/src/fts.ts";

const out = process.argv[2];
if (!out) throw new Error("用法: make-golden.mts <输出目录>");
fs.rmSync(out, { recursive: true, force: true });
// 隔离：settings:get 会读 <APPDATA>/GuiZhi/config/ai-models.json 并合并进返回值。
// 必须在加载任何主进程模块之前把 APPDATA 指到临时目录，否则基准里会混进真实用户的 AI 配置与密钥。
const isolatedAppData = path.join(out, "isolated-appdata");
fs.mkdirSync(isolatedAppData, { recursive: true });
process.env.APPDATA = isolatedAppData;
fs.mkdirSync(out, { recursive: true });

const database = initDatabase(path.join(out, "knowledge.db"));
const items = new KnowledgeItemDB(database);
const collections = new CollectionDB(database);
const tags = new TagDB(database);

const tech = collections.create({ name: "技术", icon: "code" });
const reading = collections.create({ name: "阅读" });
collections.create({ name: "空集合" });

const mk = (title: string, content: string, tagNames: string[]) =>
  items.create({ title, content, tagNames } as never);
const a = mk("归知是本地优先的个人知识库", "采集、整理、检索、问答构成核心闭环。", ["知识库", "本地"]);
const b = mk("Electron 与 WebView2 的取舍", "安装包体积与运行时依赖。", ["桌面", "框架"]);
const c = mk("中文分词在 SQLite FTS5 里的做法", "写入前在每个汉字两侧插入空格，做全文检索。", ["检索", "FTS5"]);
const d = mk("MyGo 桌面框架评估", "用 Go 写桌面应用，页面通过 WebView2 渲染。", ["MyGo", "桌面"]);
const e = mk("English only note about backups", "Backups use VACUUM INTO.", ["backup"]);
const f = mk("已归档的旧笔记", "这条笔记已经归档。", ["旧"]);
const g = mk("已进回收站的笔记", "这条笔记在回收站里。", ["知识库"]);
const h = mk("抖音采集的视频笔记", "来自抖音的内容。", ["视频"]);
const i = mk("小红书图文", "来自小红书的内容，讲检索与整理。", ["图文", "检索"]);

const now = Date.now();
database.run("UPDATE knowledge_items SET collection_id = ? WHERE id IN (?, ?, ?)", [tech.id, a.id, c.id, d.id]);
database.run("UPDATE knowledge_items SET collection_id = ? WHERE id = ?", [reading.id, b.id]);
database.run("UPDATE knowledge_items SET is_favorite = 1 WHERE id IN (?, ?)", [a.id, e.id]);
database.run("UPDATE knowledge_items SET status = 'archived' WHERE id = ?", [f.id]);
database.run("UPDATE knowledge_items SET deleted_at = ? WHERE id = ?", [now, g.id]);
for (const [item, platform, n] of [[h, "douyin", 1], [i, "xiaohongshu", 2]] as const) {
  database.run(
    "INSERT INTO source_records (id, item_id, source_type, source_uri, platform, captured_at) VALUES (?, ?, 'url', ?, ?, ?)",
    [`src-${n}`, item.id, `https://example.com/${n}`, platform, now],
  );
}

// ── knowledge:list 用的更丰富数据：Markdown、视频元数据块、超长正文、emoji、批量条目、置顶与时间并列 ──
items.update(a.id, {
  content: "# 归知简介\n\n> 引用一行\n\n- 列表项一\n- 列表项二\n\n```ts\nconst hidden = 1;\n```\n\n**加粗**文字与[链接文字](https://example.com)以及![图片说明](x.png)，全文检索与整理。\n\n\n   末尾段落。",
} as never);
const vid = mk("带元数据块的视频", "x", ["视频"]);
items.update(vid.id, {
  content: "> 平台：抖音 · 作者：甲 · 时长：1:02\n> 简介：这是简介 · 含点\n\n\n视频正文第一行，讲检索。\n第二行。",
} as never);
const longItem = mk("超长正文与搜索摘要", "x", ["检索"]);
items.update(longItem.id, {
  content: "前缀填充".repeat(30) + "中间出现关键词检索在这里，" + "后缀填充内容".repeat(60) + "😀".repeat(40) + "尾部检索结束",
} as never);
const emojiItem = mk("😀 emoji 标题与内容", "x", ["图文"]);
items.update(emojiItem.id, { content: "😀😃😄😁".repeat(12) + "检索" + "😀".repeat(100) + "tail" } as never);
const bulk: string[] = [];
for (let n = 1; n <= 12; n++) {
  const it = mk(`批量条目 ${n}`, `第 ${n} 条。${n % 3 === 0 ? "检索 检索 检索" : n % 2 === 0 ? "检索一次" : "无关内容"}。`, n % 2 ? ["批量"] : ["批量", "检索"]);
  bulk.push(it.id);
}
database.run("UPDATE knowledge_items SET is_pinned = 1 WHERE id IN (?, ?)", [bulk[2], c.id]);
// ── 收件箱 / 导入任务：需复核、导入问题、发现候选、语义索引缺口、手机来源 ──
items.update(bulk[0], { reviewReasons: ["缺少来源", "格式异常"] } as never);
items.update(bulk[1], { reviewStatus: "needs_review" } as never);
items.update(bulk[3], { reviewReasons: ["含 <b>标签</b> & 特殊字符"] } as never);
database.run("UPDATE knowledge_items SET review_reasons = 'not json', review_status = 'needs_review' WHERE id = ?", [bulk[4]]);
for (const it of [a, b, c]) {
  database.run(
    "INSERT INTO knowledge_embeddings (item_id, chunk_index, chunk_text, content_hash, model, dims, vector, updated_at) VALUES (?, 0, 'x', 'h', 'm', 1, x'00', ?)",
    [it.id, now],
  );
}
database.run(
  "INSERT INTO platform_discovery_views (id, name, platform, mode, query, created_at, updated_at) VALUES ('v1', '视图一', 'douyin', 'keyword', 'q', ?, ?)",
  [now, now],
);
const candidateRows: Array<[string, string, string]> = [
  ["e1", JSON.stringify({ title: "  候选标题  " }), "new"],
  ["e2", "{坏 json", "new"],
  ["e3", JSON.stringify({ title: "   " }), "new"],
  ["e4", JSON.stringify({ title: "已忽略" }), "dismissed"],
  ["e5", JSON.stringify({ title: 123 }), "new"],
];
candidateRows.forEach(([ext, json, state], n) => {
  database.run(
    "INSERT INTO platform_discovery_candidates (view_id, platform, external_id, item_json, state, first_seen_at, last_seen_at) VALUES ('v1', 'douyin', ?, ?, ?, ?, ?)",
    [ext, json, state, 1_700_000_100_000 + n * 1000, 1_700_000_100_000 + n * 1000],
  );
});
const taskStatuses = ["pending", "processing", "completed", "failed", "canceled", "duplicate", "completed", "completed"];
const mobileTaskNumbers = new Set([3, 11, 17]);
database.run(
  "INSERT INTO mobile_capture_receipts (relay_key, delivery_id, original_input, task_ids, progress, created_at) VALUES ('r', 'd1', 'x', '[]', '{}', 1700000500000)",
);
for (let n = 1; n <= 30; n++) {
  const status = taskStatuses[n % taskStatuses.length];
  const created = 1_700_000_000_000 + Math.floor(n / 2) * 1000; // 每两条共用时间，逼出 id 次序
  const taskId = `task-${String(n).padStart(2, "0")}`;
  const warning = status === "completed" && n % 3 === 0 ? "转写失败" : null;
  database.run(
    `INSERT INTO import_tasks (id, source_kind, source_input, display_name, status, stage, error, warning, warning_acknowledged_at,
       item_type, result_item_id, duplicate_item_id, tag_names, stage_stats, capture_strategy, comment_limit, force_duplicate, created_at, updated_at)
     VALUES (?, 'url', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
    [
      taskId, `https://example.com/${n}`,
      n % 7 === 0 ? `100% 完成_${n}` : n % 5 === 0 ? `检索任务 ${n}` : `任务 ${n}`,
      status, status === "processing" ? "fetching" : null,
      status === "failed" ? (n % 2 ? "Network TIMEOUT" : "") : null,
      warning, warning && n % 2 === 0 ? created + 1 : null,
      status === "completed" ? "webpage" : null,
      status === "completed" && n % 4 === 0 ? a.id : status === "failed" && n % 3 === 0 ? c.id : null,
      status === "duplicate" ? b.id : null,
      n === 8 ? '["a","b",3]' : n === 9 ? "not json" : null,
      n === 8 ? '[{"stage":"fetch","ms":12,"extra":1},{"stage":1,"ms":2},{"ms":3},"x",null]' : n === 9 ? '{"a":1}' : n === 10 ? "[]" : null,
      n % 4 === 0 ? "authenticated" : "standard", n % 6 === 0 ? 10 : 0,
      created, created,
    ],
  );
  if (mobileTaskNumbers.has(n)) {
    database.run("INSERT INTO mobile_capture_tasks (task_id, relay_key, delivery_id) VALUES (?, 'r', 'd1')", [taskId]);
  }
}

// ── 设置：含合法 JSON、非 JSON 原文、内部状态键（master_password 等）、未知键 ──
for (const [key, value] of [
  ["theme", '"dark"'],
  ["language", "zh"],
  ["master_password", '"secret"'],
  ["backupLastRunAt", "123"],
  ["networkProxy", JSON.stringify({ mode: "manual", protocol: "http", host: "127.0.0.1", port: 7890, username: "", password: "p", bypass: "" })],
  ["ytDlpPath", '""'],
  ["unknownKey", "1"],
  ["backupKeepCount", "not json {"],
]) {
  database.run("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", [key, value]);
}

// 时间：每 3 条共用一个 updated_at，制造并列，逼出 id 次序与游标的并列分支
const all = database.all("SELECT id FROM knowledge_items ORDER BY id") as Array<{ id: string }>;
all.forEach((row, idx) => {
  const t = 1_700_000_000_000 + Math.floor(idx / 3) * 60_000;
  database.run("UPDATE knowledge_items SET created_at = ?, updated_at = ? WHERE id = ?", [t - 5_000, t, row.id]);
});

type Q = Record<string, unknown>;
function runList(query: Q) {
  try {
    return { query, result: items.list(query as never) };
  } catch (error) {
    return { query, error: (error as Error).message };
  }
}
/** 沿 nextCursor 一路翻到底，每一页都进 golden，保证 Go 逐页复现 */
function chain(base: Q): unknown[] {
  const pages: unknown[] = [];
  let query: Q = { ...base };
  for (let guard = 0; guard < 20; guard++) {
    const entry = runList(query) as { result?: { nextCursor?: string | null } };
    pages.push(entry);
    const next = entry.result?.nextCursor;
    if (!next) break;
    query = { ...base, cursor: next };
  }
  return pages;
}
const techId = tech.id;
const tagSearchId = tags.list().find((t) => t.name === "检索")!.id;
const listCases: unknown[] = [
  ...[
    { scope: "all" }, { scope: "all", limit: 4 }, { scope: "all", limit: 3, offset: 2 },
    { scope: "all", includeArchived: true, limit: 50 },
    { scope: "favorites" }, { scope: "archived" }, { scope: "trash" }, { scope: "uncategorized", limit: 5 },
    { scope: "all", collectionId: techId }, { scope: "all", tagId: tagSearchId, limit: 4 },
    { scope: "all", platform: "douyin" }, { scope: "all", platform: "xiaohongshu" },
    { scope: "all", excludedItemIds: [a.id, b.id] },
    { scope: "all", collectionScope: { ids: [techId], includeUncategorized: false } },
    { scope: "all", collectionScope: { ids: [], includeUncategorized: true }, limit: 4 },
    { scope: "all", collectionScope: { ids: [], includeUncategorized: false } },
    { scope: "all", search: "((" },
    { scope: "all", search: "怎么做中文分词的", searchMode: "recall" },
    { scope: "all", search: "归知 检索" },
    { scope: "all", search: "检索", platform: "douyin" },
    { scope: "all", cursor: "garbage" },
  ].map(runList),
  ...chain({ scope: "all", limit: 4 }),
  ...chain({ scope: "all", sortBy: "title", sortOrder: "asc", limit: 5 }),
  ...chain({ scope: "all", sortBy: "createdAt", sortOrder: "asc", limit: 5 }),
  ...chain({ scope: "all", sortBy: "createdAt", sortOrder: "desc", limit: 6 }),
  ...chain({ scope: "all", search: "检索", limit: 3 }),
  ...chain({ scope: "all", search: "检索", searchMode: "recall", limit: 4 }),
];
const ftsInputs: Array<[string, "phrase" | "recall"]> = [
  ["知识库", "phrase"], ["全文检索", "phrase"], ["桌面 框架", "phrase"], ["VACUUM", "phrase"],
  ["归知(测试)", "phrase"], ["((", "phrase"], ["   ", "phrase"], ["a\"b", "phrase"],
  ["归知的语义检索是怎么实现的", "recall"], ["为什么中文分词需要这样做", "recall"],
  ["的了是", "recall"], ["FTS5 中文分词 怎么做", "recall"], ["日本語テスト 한국어", "phrase"],
  ["一二三四五六七八九十 一二三四五六七八九十 abc def ghi jkl", "recall"],
];
const fts = ftsInputs.map(([input, mode]) => ({
  input,
  mode,
  match: buildFtsMatchQuery(input, mode),
  segmented: segmentTextForFts(input),
}));

const countQueries: unknown[] = [
  undefined,
  { scope: "all" }, { scope: "uncategorized" }, { scope: "favorites" },
  { scope: "archived" }, { scope: "trash" }, { scope: "all", includeArchived: true },
  { scope: "all", collectionId: tech.id },
  { scope: "all", tagId: tags.list().find((t) => t.name === "检索")!.id },
  { scope: "all", platform: "douyin" },
  { scope: "all", search: "检索" },
  { scope: "all", search: "怎么做中文分词的", searchMode: "recall" },
  { scope: "all", search: "((" },
];
const counts = countQueries.map((query) => ({ query: query ?? null, result: items.counts(query as never) }));

// ── inbox:list / import:list ──
const importDao = new ImportTaskDB(database);
function runImport(query: unknown) {
  try {
    // 与 import.ipc.ts 一致：传对象走分页，否则只返回第一页的 entries 数组
    const result = query && typeof query === "object"
      ? importDao.listPage(query as never)
      : importDao.listPage().entries;
    return { query: query ?? null, result };
  } catch (error) {
    return { query: query ?? null, error: (error as Error).message };
  }
}
function importChain(base: Q): unknown[] {
  const pages: unknown[] = [];
  let query: Q = { ...base };
  for (let guard = 0; guard < 20; guard++) {
    const entry = runImport(query) as { result?: { nextCursor?: string | null } };
    pages.push(entry);
    const next = entry.result?.nextCursor;
    if (!next) break;
    query = { ...base, cursor: next };
  }
  return pages;
}
const importCases: unknown[] = [
  runImport(undefined), runImport(null), runImport({}),
  ...importChain({ pageSize: 20 }),
  ...importChain({ pageSize: 20, status: "completed" }),
  ...importChain({ pageSize: 20, origin: "desktop" }),
  ...importChain({ pageSize: 1, status: "all" }),
  ...["pending", "processing", "failed", "canceled", "duplicate", "active", "degraded", "bogus"].map((status) => runImport({ status })),
  ...["mobile", "desktop", "all", "bogus"].map((origin) => runImport({ origin })),
  ...["检索", " 检索 ", "100%", "100", "_1", "ERROR", "timeout", "转写", "example.com/1", "任务 1", "%", "\\", ""].map((query) => runImport({ query })),
  runImport({ query: "检索", origin: "mobile" }),
  runImport({ query: "任务", status: "completed", origin: "desktop", pageSize: 20 }),
  runImport({ pageSize: 20.5 }), runImport({ pageSize: 1000 }), runImport({ pageSize: 5 }), runImport({ pageSize: 33.4 }),
  runImport({ cursor: "garbage" }), runImport({ cursor: null }),
  runImport({ cursor: Buffer.from(JSON.stringify({ createdAt: "x", id: "task-01" })).toString("base64url") }),
  runImport({ cursor: Buffer.from(JSON.stringify({ createdAt: 1_700_000_005_000, id: "task-09" })).toString("base64url"), pageSize: 20 }),
];

// ── AI 用量：相对「今天」的几天，含超过保留期的旧记录、空模型名、多场景同日 ──
const localDayOf = (ms: number) => { const d = new Date(ms); return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`; };
const usageRows: Array<[number, string, string, number, number, number, number]> = [
  [0, "ask", "gpt-x", 5, 1, 1000, 500], [0, "wiki", "gpt-x", 2, 0, 400, 100], [0, "ask", "claude-y", 3, 0, 300, 200],
  [1, "ask", "gpt-x", 4, 2, 800, 300], [1, "ask", "", 1, 0, 10, 5], [2, "wiki", "claude-y", 9, 3, 5000, 2500],
  [5, "inbox", "gpt-x", 2, 0, 50, 20], [29, "ask", "gpt-x", 7, 0, 700, 70], [30, "wiki", "gpt-x", 7, 0, 700, 70], [89, "ask", "old-model", 1, 0, 1, 1],
  [90, "ask", "edge90", 1, 0, 1, 1], [91, "ask", "too-old", 1, 0, 1, 1], [200, "wiki", "ancient", 1, 0, 1, 1],
];
const dayMs = 24 * 60 * 60 * 1000;
for (const [ago, scenario, model, calls, failed, prompt, completion] of usageRows) {
  database.run(
    "INSERT INTO ai_usage_daily(day, scenario, model, calls, failed_calls, prompt_tokens, completion_tokens, updated_at) VALUES(?,?,?,?,?,?,?,?)",
    [localDayOf(Date.now() - ago * dayMs), scenario, model, calls, failed, prompt, completion, 1_700_000_000_000],
  );
}
// ── 问答会话：知识库范围与文章范围、置顶、自定义标题、来源清除标记 ──
const asks = new AskSessionDB(database);
const askMsgs = (pairs: Array<[string, string]>) => JSON.stringify(pairs.map(([question, answer]) => ({ question, answer, role: "turn" })));
const askSeed: Array<{ id: string; title: string; msgs: Array<[string, string]>; at: number }> = [
  { id: "s-01", title: "检索与分词", msgs: [["如何做中文全文检索？", "写入前逐字分隔，用 FTS5。"]], at: 1_700_000_100_000 },
  { id: "s-02", title: "Backup strategy", msgs: [["How to back up?", "Use VACUUM INTO."], ["And restore?", "Copy back."]], at: 1_700_000_200_000 },
  { id: "s-03", title: "桌面框架", msgs: [["Electron 和 MyGo 怎么选？", "看体积与运行时依赖。"]], at: 1_700_000_200_000 },
  { id: "s-04", title: "空消息", msgs: [], at: 1_700_000_300_000 },
  { id: "s-05", title: "同时间 A", msgs: [["检索 检索", "检索"]], at: 1_700_000_400_000 },
  { id: "s-06", title: "同时间 B", msgs: [["检索一次", "答"]], at: 1_700_000_400_000 },
  { id: "s-07", title: "同时间 C", msgs: [["x", "y"]], at: 1_700_000_400_000 },
  { id: "s-08", title: "😀 emoji 标题", msgs: [["😀 问题", "😀 回答"]], at: 1_700_000_500_000 },
  { id: "s-09", title: "100% 与 _ 特殊", msgs: [["100% 完成_1", "ok"]], at: 1_700_000_600_000 },
  { id: "s-10", title: "长对话", msgs: Array.from({ length: 6 }, (_, n) => [`第 ${n} 问 检索`, `第 ${n} 答`] as [string, string]), at: 1_700_000_700_000 },
];
for (const s of askSeed) asks.save({ id: s.id, title: s.title, messagesJson: askMsgs(s.msgs) } as never);
asks.save({ id: "a-01", title: "本文问答一", messagesJson: askMsgs([["本文讲什么？", "讲归知。"]]), scope: "article", itemId: a.id, articleTitle: a.title, webEnabled: true, target: { itemId: a.id, view: "reader", selection: "选中文字 https://example.com/p?token=1" } } as never);
asks.save({ id: "a-02", title: "本文问答二", messagesJson: askMsgs([["再问一次", "再答"]]), scope: "article", itemId: a.id, articleTitle: a.title, webEnabled: false, target: { itemId: a.id, view: "reader" } } as never);
asks.save({ id: "a-03", title: "另一篇", messagesJson: askMsgs([["另一篇讲什么", "讲 Electron"]]), scope: "article", itemId: b.id, articleTitle: b.title, target: { itemId: b.id, view: "source" } } as never);
const askTimes: Record<string, number> = { "a-01": 1_700_000_800_000, "a-02": 1_700_000_900_000, "a-03": 1_700_001_000_000 };
for (const s of askSeed) database.run("UPDATE ask_sessions SET created_at = ?, updated_at = ? WHERE id = ?", [s.at - 50, s.at, s.id]);
for (const [id, at] of Object.entries(askTimes)) database.run("UPDATE ask_sessions SET created_at = ?, updated_at = ? WHERE id = ?", [at - 50, at, id]);
asks.updateMeta("s-03", { pinned: true });
asks.updateMeta("s-01", { title: "  自定义标题 检索  " });
asks.updateMeta("a-02", { pinned: true, title: "置顶的本文问答" });
database.run("INSERT INTO ask_evidence_clearances(item_id, cleared_at) VALUES(?, ?)", [f.id, 1_700_000_000_000]);
const golden: Record<string, unknown> = {
  fts,
  collections: collections.list(),
  tags: tags.list(),
  counts,
  list: listCases,
  inbox: listInboxItems(database as never),
  imports: importCases,
};
const tagSnapshot = tags.list();
closeDatabase();

// electron 桩：必须在加载任何 ipc 模块之前安装（写入场景与 settings 场景共用）
const ipcHandlers = new Map<string, (...args: unknown[]) => unknown>();
const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy(
  { ipcMain: { handle: (ch: string, fn: (...a: unknown[]) => unknown) => ipcHandlers.set(ch, fn), on() {}, removeHandler() {} } } as Record<string | symbol, unknown>,
  { get: (t, p) => (p in t ? t[p] : noop) },
);
const moduleLoader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = moduleLoader._load;
moduleLoader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  return request === "electron" ? electronStub : originalLoad.call(this, request, ...rest);
};
// inbox 写频道：用真实的 inbox.ipc.ts 处理函数（electron 已换成桩），每个场景库单独注册
const inboxIpc = await import("../../apps/desktop/src/main/ipc/inbox.ipc.ts");
function inboxHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  inboxIpc.registerInboxIPC(db as never);
  const wrap = (channel: string) => (...args: unknown[]) => (ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args);
  return {
    organize: wrap("inbox:organize"),
    markReviewed: wrap("inbox:markReviewed"),
    acknowledgeImportWarning: wrap("inbox:acknowledgeImportWarning"),
    aiClassificationSources: wrap("inbox:aiClassificationSources"),
    applyAiClassification: wrap("inbox:applyAiClassification"),
  };
}// ask 会话频道：同样用真实的 ask.ipc.ts 处理函数
const askIpc = await import("../../apps/desktop/src/main/ipc/ask.ipc.ts");
function askHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  askIpc.registerAskIPC(db as never);
  const wrap = (channel: string) => (...args: unknown[]) => (ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args);
  return {
    listSessions: wrap("ask:listSessions"), getSession: wrap("ask:getSession"), saveSession: wrap("ask:saveSession"),
    deleteSession: wrap("ask:deleteSession"), query: wrap("askSession:query"), meta: wrap("askSession:meta"),
  };
}// ai 用量频道：真实的 ai.ipc.ts（含网络请求代理的其余部分，这里只用 usage 三个频道）
const aiIpc = await import("../../apps/desktop/src/main/ipc/ai.ipc.ts");
function aiUsageHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  aiIpc.registerAIIPC(db as never);
  const wrap = (channel: string) => (...args: unknown[]) => (ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args);
  return { usageRecord: wrap("ai:usageRecord"), usageSummary: wrap("ai:usageSummary"), usageClear: wrap("ai:usageClear") };
}// ── 写入频道：每个场景在基准库的独立副本上依次执行一串操作，记录返回值与写后的原始表内容 ──
const baseDb = path.join(out, "knowledge.db");
const A = a.id, B = b.id, C = c.id, F = f.id, G = g.id;
const DUMP_SQL: Record<string, string> = {
  items: "SELECT id, title, content, summary, transcript, item_type, status, collection_id, is_favorite, is_pinned, review_status, review_reasons, deleted_at, created_at, updated_at FROM knowledge_items",
  tags: "SELECT id, name, color_key, created_at, updated_at FROM tags",
  itemTags: "SELECT item_id, tag_id FROM knowledge_item_tags",
  fts: "SELECT item_id, title, content, tags FROM knowledge_fts",
  collections: "SELECT id, name, icon, sort_order, created_at, updated_at FROM collections",
  askSessions: "SELECT id, title, messages_json, created_at, updated_at, scope, item_id, article_title, options_json FROM ask_sessions",
  askMeta: "SELECT id, custom_title, pinned FROM ask_session_meta",
  askFts: "SELECT session_id, title, questions, answers FROM ask_session_fts",
  askClear: "SELECT item_id FROM ask_evidence_clearances",
  semantic: "SELECT item_id, chunk_index, chunk_text, content_hash, model, dims, hex(vector) AS vec, updated_at FROM knowledge_embeddings",
  aiUsage: "SELECT day, scenario, model, calls, failed_calls, prompt_tokens, completion_tokens, updated_at FROM ai_usage_daily",
  importTasks: "SELECT id, status, warning, warning_acknowledged_at, updated_at FROM import_tasks",
  ftsMap: "SELECT m.item_id AS map_item, f.item_id AS fts_item FROM knowledge_fts_map m LEFT JOIN knowledge_fts f ON f.rowid = m.fts_rowid",
};
type Op = { fn: string; args: unknown[] };
const tagIdByName = new Map(tagSnapshot.map((t) => [t.name, t.id]));
const tagIdOf = (name: string) => tagIdByName.get(name) ?? "missing-tag";
// 超过单批 400 的 id 数，覆盖分块路径
const manyIds = [A, B, C, F, G, ...bulk, ...Array.from({ length: 440 }, (_, n) => `missing-${n}`)];
// 每个 apply 的异常/边界输入单独成场景：同一场景里前面的操作会改变条目是否「仍符合归类条件」，会遮住后面的校验
const eligibleIds = [bulk[2], ...bulk.slice(5, 12), e.id, vid.id, longItem.id];
const one = (collectionName: unknown, itemId: unknown = eligibleIds[0]) => ({ assignments: [{ itemId, collectionName }] });
const applyInvalidCases: unknown[] = [
  one(""), one("   "), one("一二三四五六七八九十一二三四五六七八九十一"), one("一二三四五六七八九十一二三四五六七八九十"),
  one("😀😀😀😀😀😀😀😀😀😀😀"), one("😀😀😀😀😀😀😀😀😀😀"), one("a\tb"), one("a\nb"), one("x", ""), one("x", "  "),
  { assignments: [{ itemId: eligibleIds[0], collectionName: "x" }, { itemId: ` ${eligibleIds[0]} `, collectionName: "y" }] },
  one("回收站"), one("全部"), one("未归知识库"), one(" 收藏 "), one("ＡＬＬ"),
  { assignments: [{ itemId: eligibleIds[0], collectionName: "ＡＬＬ" }, { itemId: eligibleIds[1], collectionName: "未分类" }] },
  { assignments: eligibleIds.slice(0, 9).map((itemId, n) => ({ itemId, collectionName: `新合集${n}` })) },
  { assignments: eligibleIds.slice(0, 8).map((itemId, n) => ({ itemId, collectionName: `新合集${n}` })) },
  { assignments: Array.from({ length: 101 }, (_, n) => ({ itemId: `m-${n}`, collectionName: "x" })) },
  { assignments: "not-array" }, { assignments: [null, 5, { itemId: 7, collectionName: 8 }] },
  { assignments: [null] }, { assignments: [{ itemId: eligibleIds[0] }] }, { assignments: [{ collectionName: "x" }] },
  null, {}, { assignments: [] },
  // 复用已有集合（含大小写、全角、首尾空白）而不新建
  one("技术"), one(" ＴＥＣＨ "), one("阅读  "), one("空集合"),
];// ── 语义索引：真实的 semantic.ipc.ts；先在基准库里播种几组向量 ──
const semanticIpc = await import("../../apps/desktop/src/main/ipc/semantic.ipc.ts");
const semanticSvc = await import("../../apps/desktop/src/main/services/semantic.ts");
const { computeContentHash } = await import("../../apps/desktop/src/main/services/import/content-hash.ts");
function semanticHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  semanticIpc.registerSemanticIPC(db as never);
  semanticSvc.resetSemanticSearchTelemetryForTests();
  const wrap = (channel: string) => (...args: unknown[]) => (ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args);
  return { status: wrap("semantic:status"), listPending: wrap("semantic:listPending"), applyEmbeddings: wrap("semantic:applyEmbeddings"), search: wrap("semantic:search"), clear: wrap("semantic:clear") };
}
const semHash: Record<string, string> = {};
// 语义场景用独立的基准库：播种会改条目的审核字段，不能影响其他场景与列表/收件箱基线
const semBaseDb = path.join(out, "semantic-base.db");
{
  fs.copyFileSync(baseDb, semBaseDb);
  const sdb = initDatabase(semBaseDb);
  const sem = new SemanticIndexDB(sdb);
  const hashFor = (id: string) => {
    const r = sdb.get("SELECT title, content, transcript FROM knowledge_items WHERE id = ?", id) as { title: string; content: string; transcript: string | null };
    return computeContentHash(semanticSvc.buildSemanticSourceText(r.title, r.content, r.transcript));
  };
  for (const id of [A, B, C, d.id, e.id, F, G, h.id, i.id, vid.id, emojiItem.id, longItem.id, ...bulk.slice(0, 12)]) semHash[id] = hashFor(id);
  const put = (id: string, model: string, chunks: Array<[string, number[]]>, hash = semHash[id]) =>
    sem.replaceItemChunks({ itemId: id, contentHash: hash, model, dims: chunks[0][1].length, chunks: chunks.map(([text, v]) => ({ text, vector: new Float32Array(v) })) });
  put(A, "m1", [["第一块  文本", [1, 0, 0, 0]], ["第二块\n\n文本", [0.6, 0.8, 0, 0]]]);
  put(B, "m1", [["  很长\n\n文本 " + "字".repeat(200), [0, 1, 0, 0]]]);
  put(C, "m1", [["并列分数", [1, 0, 0, 0]]], "stale-hash");
  put(d.id, "m2", [["另一个模型", [0, 0, 1, 0]]]);
  put(h.id, "m1", [["a\u00a0\u2003b\ufeffc 末尾\u2028", [0.5, 0.5, 0.5, 0.5]]]);
  put(G, "m1", [["回收站里的", [1, 0, 0, 0]]]);
  put(e.id, "m1", [["维度三", [1, 1, 1]]]);
  put(i.id, "m1", [["重复一", [0, 0, 0, 1]], ["重复二", [0, 0, 0, 1]]]);
  put(vid.id, "m3", [["坏的审核原因", [1, 0, 0, 0]]]);
  put(emojiItem.id, "m4", [["😀".repeat(100), [1, 0, 0, 0]]]);
  put(longItem.id, "m5", [["a" + "😀".repeat(100), [1, 0, 0, 0]]]);
  sdb.run("UPDATE knowledge_embeddings SET updated_at = 1700000000000 WHERE item_id IN (?, ?)", [C, h.id]);
  sdb.run("UPDATE knowledge_items SET review_status = 'needs_review', review_reasons = ? WHERE id = ?", [JSON.stringify(["过短", "重复"]), B]);
  sdb.run("UPDATE knowledge_items SET review_reasons = 'not json' WHERE id = ?", [vid.id]);
  closeDatabase();
}
const vecQuery = (model: unknown, vector: unknown, extra: Record<string, unknown> = {}) => ({ model, vector, ...extra });
const goodChunks = (text = "新块") => [{ text, vector: [1, 0, 0, 0] }, { text: text + "2", vector: [0, 0, 1, 0] }];
const applyArgs = (id: string, over: Record<string, unknown> = {}) => ({ itemId: id, contentHash: semHash[id], model: "m1", dims: 4, chunks: goodChunks(), ...over });
const scenarios: Record<string, Op[]> = {
  createMinimal: [{ fn: "create", args: [{ title: "新建", content: "正文 abc 检索" }] }],
  createFull: [{ fn: "create", args: [{
    title: "完整输入", content: "全文内容", transcript: "转写", itemType: "webpage", status: "active", collectionId: tech.id,
    tagNames: [" 检索 ", "检索", "NewTag", "newtag", "桌面", "", "   ", "Beta", "alpha", "ábc", "Zeta"],
    reviewReasons: ["a", " a ", "", "b"],
  }] }],
  createNulls: [{ fn: "create", args: [{ title: null, content: null, itemType: null, status: null, reviewStatus: null, transcript: null, collectionId: null }] }],
  // 数据库 CHECK 约束报错：两边都必须失败，且失败后库保持原样（事务回滚）
  createBadItemType: [{ fn: "create", args: [{ title: "坏类型", itemType: "article", tagNames: ["不该留下"] }] }],
  updateBadItemType: [{ fn: "update", args: [A, { itemType: "article", tagNames: ["也不该留下"] }] }],
  createEmptyInput: [{ fn: "create", args: [{}] }],
  createExplicitReview: [{ fn: "create", args: [{ title: "显式状态", reviewStatus: "clear", reviewReasons: ["仍有原因"] }] }],
  createReasonsOnly: [{ fn: "create", args: [{ title: "只有原因", reviewReasons: ["缺少来源", "格式异常"] }] }],
  updateTextKeepsTags: [{ fn: "update", args: [A, { title: "新标题", content: "新的正文 含 检索" }] }],
  updateTagsReplace: [{ fn: "update", args: [A, { tagNames: ["桌面", "新标签Z", "Zeta", "alpha", "Beta", "ábc", "Éclair", "eclair"] }] }],
  updateTagsClear: [{ fn: "update", args: [A, { tagNames: [] }] }],
  updateOptionalFields: [
    { fn: "update", args: [A, { summary: "摘要", transcript: "转写", isFavorite: false, isPinned: true, reviewReasons: ["原因一"] }] },
    { fn: "update", args: [A, { summary: null, transcript: null, collectionId: null, reviewStatus: "clear", itemType: "webpage", status: "archived" }] },
  ],
  updateNullsFallBack: [{ fn: "update", args: [A, { title: null, content: null, itemType: null, status: null, reviewStatus: null }] }],
  updateNoop: [{ fn: "update", args: [A, {}] }],
  updateMissing: [{ fn: "update", args: ["no-such-id", { title: "x" }] }],
  bulkPlain: [{ fn: "bulkUpdate", args: [[A, B, B, "missing"], { isFavorite: true, collectionId: null, reviewReasons: [] }] }],
  bulkPlainStatus: [{ fn: "bulkUpdate", args: [[A, B, C], { status: "archived", isPinned: true, reviewStatus: "needs_review", reviewReasons: ["x", "y"] }] }],
  bulkTags: [{ fn: "bulkUpdate", args: [[A, B, C], { addTagNames: ["共享新标签", "批量", " 共享新标签 "], removeTagNames: ["桌面", " 本地 "] }] }],
  bulkTagsOnly: [{ fn: "bulkUpdate", args: [[G, "missing"], { removeTagNames: ["知识库"] }] }],
  bulkEmpty: [{ fn: "bulkUpdate", args: [[], { isFavorite: true }] }],
  bulkLarge: [{ fn: "bulkUpdate", args: [manyIds, { status: "active", isFavorite: true }] }],
  setStatus: [{ fn: "setStatus", args: [[A, G, F], "archived"] }, { fn: "setStatus", args: [[], "active"] }],
  trashRestore: [
    { fn: "moveToTrash", args: [[A, G, "missing"]] },
    { fn: "restore", args: [[G, A, F]] },
  ],
  deleteForever: [{ fn: "deleteForever", args: [[C, G, "missing"]] }],
  emptyTrash: [{ fn: "moveToTrash", args: [[A, B]] }, { fn: "emptyTrash", args: [] }],
  // ── 集合 / 标签写入（fn 带 collection. / tag. 前缀）──
  "collection.create": [
    { fn: "collection.create", args: [{ name: "  新集合  ", icon: "star" }] },
    { fn: "collection.create", args: [{ name: "无图标" }] },
    { fn: "collection.create", args: [{ name: "空图标", icon: null }] },
    { fn: "collection.create", args: [{ name: "😀 emoji 集合" }] },
  ],
  "collection.createBad": [
    { fn: "collection.create", args: [{ name: "   " }] },
    { fn: "collection.create", args: [{}] },
    { fn: "collection.create", args: [{ name: null }] },
    { fn: "collection.create", args: [{ name: 5 }] },
    { fn: "collection.create", args: [null] },
  ],
  "collection.update": [
    { fn: "collection.update", args: [tech.id, { name: " 技术二 " }] },
    { fn: "collection.update", args: [reading.id, { icon: null, sortOrder: 9 }] },
    { fn: "collection.update", args: [reading.id, { icon: "book", sortOrder: 1.5 }] },
    { fn: "collection.update", args: [tech.id, {}] },
    { fn: "collection.update", args: ["missing", { name: "x" }] },
  ],
  "collection.updateBad": [
    { fn: "collection.update", args: [tech.id, { name: "  " }] },
    { fn: "collection.update", args: [tech.id, { name: null }] },
    { fn: "collection.update", args: [tech.id, { sortOrder: null }] },
    { fn: "collection.update", args: [tech.id, { name: 7 }] },
  ],
  "collection.delete": [
    { fn: "collection.delete", args: [tech.id] },
    { fn: "collection.delete", args: [tech.id] },
    { fn: "collection.delete", args: ["missing"] },
  ],
  "collection.sequence": [
    { fn: "collection.create", args: [{ name: "甲" }] },
    { fn: "collection.delete", args: [reading.id] },
    { fn: "collection.create", args: [{ name: "乙" }] },
  ],
  "tag.create": [
    { fn: "tag.create", args: [{ name: "  新标签  ", colorKey: "red" }] },
    { fn: "tag.create", args: [{ name: "知识库", colorKey: "blue" }] },
    { fn: "tag.create", args: [{ name: "BACKUP" }] },
    { fn: "tag.create", args: [{ name: "非法颜色", colorKey: "chartreuse" }] },
    { fn: "tag.create", args: [{ name: "颜色空", colorKey: null }] },
    { fn: "tag.create", args: [{ name: "ÁBC" }] },
    { fn: "tag.create", args: [{ name: "ábc" }] },
  ],
  "tag.createBad": [
    { fn: "tag.create", args: [{ name: " " }] },
    { fn: "tag.create", args: [{}] },
    { fn: "tag.create", args: [null] },
    { fn: "tag.create", args: [{ name: 3 }] },
  ],
  "tag.update": [
    { fn: "tag.update", args: [tagIdOf("检索"), { name: " 检索新名 ", colorKey: "purple" }] },
    { fn: "tag.update", args: [tagIdOf("检索新名x"), { colorKey: "green" }] },
    { fn: "tag.update", args: [tagIdOf("桌面"), { colorKey: "nope" }] },
    { fn: "tag.update", args: [tagIdOf("本地"), {}] },
    { fn: "tag.update", args: ["missing", { name: "x" }] },
  ],
  "tag.updateConflict": [
    { fn: "tag.update", args: [tagIdOf("桌面"), { name: "MYGO" }] },
    { fn: "tag.update", args: [tagIdOf("桌面"), { name: "桌面" }] },
    { fn: "tag.update", args: [tagIdOf("桌面"), { name: "  " }] },
    { fn: "tag.update", args: [tagIdOf("桌面"), { name: null }] },
  ],
  "tag.delete": [
    { fn: "tag.delete", args: [tagIdOf("知识库")] },
    { fn: "tag.delete", args: [tagIdOf("知识库")] },
    { fn: "tag.delete", args: ["missing"] },
  ],  // ── 收件箱写频道（fn 带 inbox. 前缀，参数形状与渲染进程一致）──
  "inbox.organize": [
    { fn: "inbox.organize", args: [{ itemIds: [bulk[2], bulk[5], bulk[2]], collectionId: tech.id, addTagNames: ["整理", " 批量 "] }] },
    { fn: "inbox.organize", args: [{ itemIds: [bulk[6]], collectionId: null }] },
    { fn: "inbox.organize", args: [{ itemIds: [bulk[7]], addTagNames: ["仅标签"] }] },
    // 多余字段（status、isFavorite）必须被忽略
    { fn: "inbox.organize", args: [{ itemIds: [bulk[8]], status: "archived", isFavorite: true, collectionId: reading.id }] },
    { fn: "inbox.organize", args: [{ itemIds: [] , collectionId: tech.id }] },
    { fn: "inbox.organize", args: [{ itemIds: "not-array", collectionId: tech.id }] },
    { fn: "inbox.organize", args: [{ itemIds: [1, null, "", bulk[9]], collectionId: tech.id }] },
    { fn: "inbox.organize", args: [null] },
  ],
  "inbox.markReviewed": [
    { fn: "inbox.markReviewed", args: [[bulk[0], bulk[1], bulk[3], bulk[4], "missing", bulk[0]]] },
    { fn: "inbox.markReviewed", args: ["x"] },
    { fn: "inbox.markReviewed", args: [[]] },
  ],
  "inbox.acknowledge": [
    ...Array.from({ length: 30 }, (_, n) => ({ fn: "inbox.acknowledgeImportWarning", args: [`task-${String(n + 1).padStart(2, "0")}`] })),
    { fn: "inbox.acknowledgeImportWarning", args: ["  task-03  "] },
    { fn: "inbox.acknowledgeImportWarning", args: [5] },
    { fn: "inbox.acknowledgeImportWarning", args: [null] },
    { fn: "inbox.acknowledgeImportWarning", args: [""] },
  ],
  "inbox.sources": [
    { fn: "inbox.aiClassificationSources", args: [[bulk[2], bulk[0], A, e.id, longItem.id, emojiItem.id, vid.id, "missing", G, F, bulk[2]]] },
    { fn: "inbox.aiClassificationSources", args: [[]] },
    { fn: "inbox.aiClassificationSources", args: ["x"] },
    { fn: "inbox.aiClassificationSources", args: [Array.from({ length: 101 }, (_, n) => `m-${n}`)] },
    { fn: "inbox.aiClassificationSources", args: [Array.from({ length: 100 }, (_, n) => `m-${n}`)] },
    { fn: "inbox.aiClassificationSources", args: [[" " + bulk[2] + " ", bulk[2]]] },
  ],
  "inbox.apply": [
    { fn: "inbox.applyAiClassification", args: [{ assignments: [
      { itemId: bulk[2], collectionName: "技术" },
      { itemId: bulk[5], collectionName: "  新  集合  " },
      { itemId: bulk[6], collectionName: "新 集合" },
      { itemId: bulk[7], collectionName: "ＡＢＣ　全角" },
      { itemId: bulk[8], collectionName: "abc 全角" },
      { itemId: A, collectionName: "不会生效" },
      { itemId: bulk[9], collectionName: "阅读" },
    ] }] },
  ],
  ...Object.fromEntries(applyInvalidCases.map((input, n) => [`inbox.applyInvalid${String(n).padStart(2, "0")}`, [{ fn: "inbox.applyAiClassification", args: [input] }]])),
  "inbox.applyEdge": [
    // 全部不符合条件：不新建集合
    { fn: "inbox.applyAiClassification", args: [{ assignments: [{ itemId: A, collectionName: "全新" }, { itemId: bulk[0], collectionName: "也全新" }] }] },
    // 大小写、变音、特殊 casing 的归并键
    { fn: "inbox.applyAiClassification", args: [{ assignments: [
      { itemId: bulk[2], collectionName: "İstanbul" },
      { itemId: bulk[5], collectionName: "i̇stanbul" },
      { itemId: bulk[6], collectionName: "ΑΣ" },
      { itemId: bulk[7], collectionName: "ας" },
      { itemId: bulk[8], collectionName: "ΑΣ Β" },
      { itemId: bulk[9], collectionName: "ΣΑ" },
      { itemId: bulk[10], collectionName: "ß" },
      { itemId: bulk[11], collectionName: "SS" },
    ] }] },
  ],  // ── 问答会话（fn 带 ask. 前缀；{__cursorOf: n} 取第 n 步结果的 nextCursor）──
  "ask.queryBasic": [
    { fn: "ask.query", args: [{}] }, { fn: "ask.query", args: [null] }, { fn: "ask.query", args: [undefined] },
    { fn: "ask.query", args: [{ scope: "article" }] }, { fn: "ask.query", args: [{ scope: "knowledge" }] },
    { fn: "ask.query", args: [{ itemId: A }] }, { fn: "ask.query", args: [{ scope: "article", itemId: B }] },
    { fn: "ask.query", args: [{ from: 1_700_000_300_000, to: 1_700_000_600_000 }] },
    { fn: "ask.query", args: [{ from: null }] }, { fn: "ask.query", args: [{ to: 0 }] }, { fn: "ask.query", args: [{ from: "1700000300000" }] },
    { fn: "ask.query", args: [{ scope: "bogus" }] }, { fn: "ask.query", args: [{ itemId: "" }] }, { fn: "ask.query", args: ["x"] }, { fn: "ask.query", args: [5] },
  ],
  "ask.querySearch": [
    ...["检索", " 检索 ", "backup", "Backup restore", "桌面 框架", "😀", "100%", "_", "\"", "a b c d", "   ", "", "自定义标题", "Electron", "不存在的词汇", "回答", "VACUUM INTO"].map((search) => ({ fn: "ask.query", args: [{ search }] })),
    { fn: "ask.query", args: [{ search: "检索", scope: "knowledge", from: 1_700_000_000_000 }] },
    { fn: "ask.query", args: [{ search: 5 }] }, { fn: "ask.query", args: [{ search: null }] }, { fn: "ask.query", args: [{ search: ["x"] }] },
  ],
  "ask.queryLimit": [
    ...[0, 1, 2, 3, 2.5, 500, 1000, -5, null, 10].map((limit) => ({ fn: "ask.query", args: [{ limit }] })),
    { fn: "ask.query", args: [{ limit: "3" }] }, { fn: "ask.query", args: [{ limit: "x" }] }, { fn: "ask.query", args: [{ limit: true }] },
  ],
  "ask.queryPaging": [
    { fn: "ask.query", args: [{ limit: 4 }] },
    { fn: "ask.query", args: [{ limit: 4, cursor: { __cursorOf: 0 } }] },
    { fn: "ask.query", args: [{ limit: 4, cursor: { __cursorOf: 1 } }] },
    { fn: "ask.query", args: [{ limit: 4, cursor: { __cursorOf: 2 } }] },
    { fn: "ask.query", args: [{ limit: 4, cursor: { __cursorOf: 3 } }] },
    // 筛选条件变化后复用游标 → 报错
    { fn: "ask.query", args: [{ limit: 4, scope: "knowledge", cursor: { __cursorOf: 0 } }] },
    { fn: "ask.query", args: [{ limit: 5, cursor: { __cursorOf: 0 } }] },
  ],
  "ask.queryPagingSearch": [
    { fn: "ask.query", args: [{ search: "检索", limit: 2 }] },
    { fn: "ask.query", args: [{ search: "检索", limit: 2, cursor: { __cursorOf: 0 } }] },
    { fn: "ask.query", args: [{ search: "检索", limit: 2, cursor: { __cursorOf: 1 } }] },
    { fn: "ask.query", args: [{ search: "检索 ", limit: 2, cursor: { __cursorOf: 0 } }] },
    { fn: "ask.query", args: [{ search: "答", limit: 1 }] },
    { fn: "ask.query", args: [{ search: "答", limit: 1, cursor: { __cursorOf: 4 } }] },
  ],
  "ask.queryBadCursor": [
    { fn: "ask.query", args: [{ cursor: "garbage" }] }, { fn: "ask.query", args: [{ cursor: "" }] }, { fn: "ask.query", args: [{ cursor: 5 }] },
    { fn: "ask.query", args: [{ cursor: Buffer.from("null").toString("base64url") }] },
    { fn: "ask.query", args: [{ cursor: Buffer.from("{}").toString("base64url") }] },
    { fn: "ask.query", args: [{ cursor: Buffer.from(JSON.stringify({ fingerprint: '[null,null,"",null,null]', rank: 0, pinned: 0, updatedAt: 1, id: "s-05" })).toString("base64url") }] },
    { fn: "ask.query", args: [{ cursor: Buffer.from(JSON.stringify({ fingerprint: '[null,null,"",null,null]', rank: 0, pinned: 1, updatedAt: 1_700_000_900_000, id: "z" })).toString("base64url") }] },
    { fn: "ask.query", args: [{ cursor: Buffer.from(JSON.stringify({ fingerprint: '[null,null,"",null,null]', rank: 0, pinned: 0, updatedAt: 1_700_000_400_000, id: 5 })).toString("base64url") }] },
    { fn: "ask.query", args: [{ cursor: Buffer.from(JSON.stringify({ fingerprint: '[null,null,"",null,null]', rank: 0, pinned: 0, updatedAt: 1_700_000_400_000, id: "s-06" })).toString("base64") }] },
  ],
  "ask.list": [
    { fn: "ask.listSessions", args: [] }, { fn: "ask.listSessions", args: [undefined] }, { fn: "ask.listSessions", args: [null] },
    { fn: "ask.listSessions", args: [{ scope: "article" }] }, { fn: "ask.listSessions", args: [{ itemId: A }] }, { fn: "ask.listSessions", args: [{ scope: "knowledge", itemId: "none" }] },
    { fn: "ask.listSessions", args: [{ limit: 2 }] }, { fn: "ask.listSessions", args: [{ search: "桌面" }] },
  ],
  "ask.get": [
    ...["s-01", "s-02", "s-03", "a-01", "a-02", "a-03", "missing", ""].map((id) => ({ fn: "ask.getSession", args: [id] })),
    { fn: "ask.getSession", args: [null] }, { fn: "ask.getSession", args: [5] },
  ],
  "ask.saveNew": [
    { fn: "ask.saveSession", args: [{ id: "n-01", title: "新会话", messagesJson: JSON.stringify([{ question: "问 https://example.com/p?token=1&q=2#x", answer: "答 (https://example.com/b?key=1)" }]) }] },
    { fn: "ask.saveSession", args: [{ id: "n-02", title: "显式 knowledge", messagesJson: "[]", scope: "knowledge", webEnabled: true, itemId: "ignored", articleTitle: "ignored" }] },
    { fn: "ask.saveSession", args: [{ id: "n-03", title: "文章", messagesJson: "[]", scope: "article", itemId: A, articleTitle: "T", webEnabled: false, target: { itemId: A, view: "reader", selection: "sel https://e.com/?sig=1&a=2" } }] },
    { fn: "ask.saveSession", args: [{ id: "n-04", title: "文章默认 webEnabled", messagesJson: "[]", scope: "article", itemId: B, target: { itemId: B } }] },
    { fn: "ask.saveSession", args: [{ id: "n-05", title: "清除过的文章", messagesJson: "[]", scope: "article", itemId: F, articleTitle: "F", target: { itemId: F, view: "reader", selection: "已清除 https://e.com/?token=1" } }] },
  ],
  "ask.saveUpdate": [
    { fn: "ask.saveSession", args: [{ id: "s-01", title: "改名但被自定义标题覆盖", messagesJson: JSON.stringify([{ question: "新问题 检索", answer: "新回答" }]) }] },
    { fn: "ask.saveSession", args: [{ id: "s-02", title: "Backup strategy v2", messagesJson: "[]" }] },
    { fn: "ask.saveSession", args: [{ id: "a-01", title: "本文问答一（更新）", messagesJson: "[]", scope: "article", itemId: A, articleTitle: "新标题", webEnabled: false, target: { itemId: A, view: "source" } }] },
    { fn: "ask.saveSession", args: [{ id: "s-03", title: "从 knowledge 变 article", messagesJson: "[]", scope: "article", itemId: A, target: { itemId: A } }] },
  ],
  "ask.saveInvalid": [
    { fn: "ask.saveSession", args: [null] }, { fn: "ask.saveSession", args: [{}] }, { fn: "ask.saveSession", args: [5] },
    { fn: "ask.saveSession", args: [{ id: 1, title: "t", messagesJson: "[]" }] }, { fn: "ask.saveSession", args: [{ id: "x", title: 2, messagesJson: "[]" }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: [] }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "{" }] }, { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "{}" }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "[]", scope: "bogus" }] }, { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "[]", scope: 5 }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "[]", scope: "article" }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "[]", scope: "article", itemId: A }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "[]", scope: "article", itemId: A, target: { itemId: B } }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "[]", scope: "article", itemId: A, target: { itemId: A, selection: 5 } }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: "[]", scope: "knowledge", target: { selection: ["x"] } }] },
    { fn: "ask.saveSession", args: [{ id: "x", title: "t", messagesJson: JSON.stringify([{ sources: [{ sourceItemIds: "bad" }] }]) }] },
  ],
  "ask.saveEvidence": [
    { fn: "ask.saveSession", args: [{ id: "e-01", title: "证据", messagesJson: JSON.stringify([
      { question: "q", answer: "a", sources: [{ ordinal: 1, kind: "item", refId: F, sourceId: F, title: "被清除的来源", text: "正文" }, { ordinal: 2, kind: "item", refId: A, sourceId: A, title: "正常", text: `见 https://example.com/p?token=1` }],
        context: { target: { itemId: F, view: "v" }, sources: [{ title: "继承清除" }] } },
      { question: "q2", context: { target: { itemId: A, selection: "https://x.com/?key=1" }, sources: [{ title: "继承 A" }] }, evidenceSources: [{ kind: "item", refId: F, title: "x", evidence: { text: "t", capturedAt: 9, sourceItemIds: [F] } }] },
    ]) }] },
  ],
  "ask.metaTitle": [
    { fn: "ask.meta", args: ["s-02", { title: "  新标题  " }] }, { fn: "ask.meta", args: ["s-02", { title: "再改一次 检索" }] },
    { fn: "ask.meta", args: ["s-04", { title: "x".repeat(200) }] }, { fn: "ask.meta", args: ["s-05", { title: "😀".repeat(100) }] }, { fn: "ask.meta", args: ["s-05", { title: "😀".repeat(101) }] },
    { fn: "ask.meta", args: ["s-06", { title: "x".repeat(201) }] }, { fn: "ask.meta", args: ["s-06", { title: "" }] }, { fn: "ask.meta", args: ["s-06", { title: "   " }] },
    { fn: "ask.meta", args: ["s-06", { title: null }] }, { fn: "ask.meta", args: ["s-06", { title: 5 }] }, { fn: "ask.meta", args: ["s-06", { title: ["x"] }] },
    { fn: "ask.meta", args: ["s-07", { title: "\u3000全角空白\u3000" }] },
  ],
  "ask.metaPin": [
    { fn: "ask.meta", args: ["s-02", { pinned: true }] }, { fn: "ask.meta", args: ["s-03", { pinned: false }] }, { fn: "ask.meta", args: ["s-04", { pinned: 1 }] },
    { fn: "ask.meta", args: ["s-05", { pinned: "" }] }, { fn: "ask.meta", args: ["s-06", { pinned: null }] }, { fn: "ask.meta", args: ["s-07", { pinned: {} }] },
    { fn: "ask.meta", args: ["s-08", {}] }, { fn: "ask.meta", args: ["s-08", null] }, { fn: "ask.meta", args: ["s-08", undefined] },
    { fn: "ask.meta", args: ["s-09", { pinned: true, title: "同时改" }] },
    { fn: "ask.query", args: [{ limit: 5 }] },
  ],
  "ask.metaMissing": [
    { fn: "ask.meta", args: ["missing", { title: "x" }] }, { fn: "ask.meta", args: ["", {}] }, { fn: "ask.meta", args: [null, { pinned: true }] },
    { fn: "ask.meta", args: ["missing", { title: "" }] },
  ],
  "ask.delete": [
    { fn: "ask.deleteSession", args: ["s-01"] }, { fn: "ask.deleteSession", args: ["s-01"] }, { fn: "ask.deleteSession", args: ["missing"] },
    { fn: "ask.deleteSession", args: ["s-03"] }, { fn: "ask.deleteSession", args: [null] }, { fn: "ask.deleteSession", args: [""] },
  ],
  "ask.sequence": [
    { fn: "ask.saveSession", args: [{ id: "q-01", title: "序列 检索", messagesJson: JSON.stringify([{ question: "序列问 检索", answer: "答" }]) }] },
    { fn: "ask.meta", args: ["q-01", { title: "序列改名 桌面", pinned: true }] },
    { fn: "ask.query", args: [{ search: "桌面" }] },
    { fn: "ask.saveSession", args: [{ id: "q-01", title: "再保存", messagesJson: "[]" }] },
    { fn: "ask.query", args: [{ search: "桌面" }] },
    { fn: "ask.deleteSession", args: ["q-01"] },
    { fn: "ask.query", args: [{ search: "桌面" }] },
  ],  // ── AI 用量（fn 带 ai. 前缀）──
  "ai.summary": [
    ...[undefined, null, 1, 2, 3, 7, 30, 31, 90, 91, 1000, 0, -5, 2.5, 0.5, "7", "x", true, [3], {}, "  5  ", "0x10", ""].map((days) => ({ fn: "ai.usageSummary", args: [days] })),
  ],
  "ai.record": [
    { fn: "ai.usageRecord", args: [{ scenario: "ask", model: "gpt-x", promptTokens: 10, completionTokens: 5 }] },
    { fn: "ai.usageRecord", args: [{ scenario: "ask", model: "gpt-x", promptTokens: 10.9, completionTokens: -3, failed: true }] },
    { fn: "ai.usageRecord", args: [{ scenario: "new-scenario", model: "", promptTokens: "7", completionTokens: null }] },
    { fn: "ai.usageRecord", args: [{ scenario: "ask", model: "gpt-x", promptTokens: "abc", completionTokens: [4], failed: "true" }] },
    { fn: "ai.usageRecord", args: [{ scenario: "ask", model: "gpt-x", failed: true }] },
    { fn: "ai.usageRecord", args: [{ scenario: "ask", model: "gpt-x", promptTokens: true, completionTokens: 1e3 }] },
    { fn: "ai.usageRecord", args: [{ scenario: 1, model: "m" }] }, { fn: "ai.usageRecord", args: [{ scenario: "s", model: null }] },
    { fn: "ai.usageRecord", args: [null] }, { fn: "ai.usageRecord", args: [undefined] }, { fn: "ai.usageRecord", args: [5] }, { fn: "ai.usageRecord", args: [{}] },
    { fn: "ai.usageSummary", args: [30] },
  ],
  "ai.clear": [
    { fn: "ai.usageClear", args: [] }, { fn: "ai.usageSummary", args: [90] },
    { fn: "ai.usageRecord", args: [{ scenario: "ask", model: "m", promptTokens: 1, completionTokens: 1 }] }, { fn: "ai.usageSummary", args: [1] },
  ],  // ── 语义索引（fn 带 semantic. 前缀）──
  "semantic.status": [
    ...["m1", "m2", "", null, 5, "none", "m1"].map((m) => ({ fn: "semantic.status", args: [m] })),
    { fn: "semantic.search", args: [vecQuery("m1", [1, 0, 0, 0])] }, { fn: "semantic.status", args: ["m1"] },
    { fn: "semantic.search", args: [vecQuery("m1", [1, 0, 0])] }, { fn: "semantic.status", args: ["m2"] },
  ],
  "semantic.pending": [
    ...[{ model: "m1" }, { model: "m1", limit: 1 }, { model: "m1", limit: 2 }, { model: "m1", limit: 2.5 }, { model: "m1", limit: 3 }, { model: "m1", limit: 10 },
      { model: "m1", limit: 50 }, { model: "m1", limit: 51 }, { model: "m1", limit: 0 }, { model: "m1", limit: -1 }, { model: "m1", limit: "5" }, { model: "m1", limit: "x" },
      { model: "m2", limit: 4 }, { model: "none", limit: 3 }, { model: 5 }, {}, null, undefined, "m1", [1]].map((p) => ({ fn: "semantic.listPending", args: [p] })),
    { fn: "semantic.status", args: ["m1"] },
  ],
  "semantic.search": [
    ...[
      vecQuery("m1", [1, 0, 0, 0]), vecQuery("m1", [1, 0, 0, 0], { limit: 1 }), vecQuery("m1", [1, 0, 0, 0], { limit: 2 }), vecQuery("m1", [1, 0, 0, 0], { limit: 2.5 }),
      vecQuery("m1", [0, 1, 0, 0]), vecQuery("m1", [0.5, 0.5, 0.5, 0.5], { limit: 20 }), vecQuery("m1", [0, 0, 0, 1], { limit: 100 }), vecQuery("m1", [1, 1, 1, 1], { limit: "3" }),
      vecQuery("m1", [1, 0, 0, 0], { limit: 0 }), vecQuery("m1", [1, 0, 0, 0], { limit: -4 }), vecQuery("m1", [1, 0, 0, 0], { limit: "x" }),
      vecQuery("m1", ["1", null, true, [0]]), vecQuery("m1", [1e30, 0, 0, 0]), vecQuery("m1", [0.1, 0.2, 0.3, 0.4], { limit: 20 }), vecQuery("m1", [-1, 0, 0, 0], { limit: 20 }),
      vecQuery("m1", [1, 1, 1]), vecQuery("m1", [1, 0, 0, 0, 0]), vecQuery("m1", []), vecQuery("m1", "abc"), vecQuery("m1", null), vecQuery("", [1, 0, 0, 0]), vecQuery(5, [1, 0, 0, 0]),
      vecQuery("m2", [0, 0, 1, 0]), vecQuery("m2", [1, 0, 0, 0]), vecQuery("m3", [1, 0, 0, 0]), vecQuery("m4", [1, 0, 0, 0]), vecQuery("m5", [1, 0, 0, 0]), vecQuery("none", [1, 0, 0, 0]),
      null, undefined, {}, "m1", [1, 0, 0, 0],
    ].map((p) => ({ fn: "semantic.search", args: [p] })),
  ],
  "semantic.apply": [
    { fn: "semantic.applyEmbeddings", args: [applyArgs(bulk[0])] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(bulk[1], { model: "m9", chunks: [{ text: "单块", vector: ["1", null, 0.5, "x"] }] })] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(A, { chunks: goodChunks("覆盖") })] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(C)] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(bulk[2], { contentHash: "wrong" })] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(G)] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs("missing-item", { contentHash: "x" })] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(bulk[3], { dims: 3 })] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(bulk[3], { dims: 4.0, chunks: [{ text: "整数浮点", vector: [1e-3, 2e-3, -3, 3e38] }] })] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(bulk[4], { chunks: [{ text: "x", vector: [0.1, 0.2, 0.3, 0.4] }] })] },
    { fn: "semantic.status", args: ["m1"] }, { fn: "semantic.listPending", args: [{ model: "m1", limit: 20 }] },
    { fn: "semantic.search", args: [vecQuery("m1", [1, 0, 0, 0], { limit: 20 })] }, { fn: "semantic.search", args: [vecQuery("m9", [1, 0, 1, 0.5])] },
  ],
  "semantic.applyInvalid": [
    ...[
      null, undefined, 5, "x", [], {}, applyArgs(bulk[5], { itemId: 1 }), applyArgs(bulk[5], { contentHash: null }), applyArgs(bulk[5], { model: 1 }),
      applyArgs(bulk[5], { dims: 0 }), applyArgs(bulk[5], { dims: -4 }), applyArgs(bulk[5], { dims: 4.5 }), applyArgs(bulk[5], { dims: "4" }), applyArgs(bulk[5], { dims: null }),
      applyArgs(bulk[5], { chunks: [] }), applyArgs(bulk[5], { chunks: null }), applyArgs(bulk[5], { chunks: {} }), applyArgs(bulk[5], { chunks: [null] }),
      applyArgs(bulk[5], { chunks: [{ vector: [1, 0, 0, 0] }] }), applyArgs(bulk[5], { chunks: [{ text: "x" }] }), applyArgs(bulk[5], { chunks: [{ text: "x", vector: [1, 0, 0] }] }),
      applyArgs(bulk[5], { chunks: [{ text: "x", vector: "1000" }] }), applyArgs(bulk[5], { chunks: [{ text: 1, vector: [1, 0, 0, 0] }] }),
      applyArgs(bulk[5], { chunks: [{ text: "ok", vector: [1, 0, 0, 0] }, { text: "bad", vector: [1] }] }),
    ].map((p) => ({ fn: "semantic.applyEmbeddings", args: [p] })),
    { fn: "semantic.status", args: ["m1"] },
  ],
  "semantic.staleCache": [
    { fn: "semantic.search", args: [vecQuery("m1", [1, 0, 0, 0], { limit: 20 })] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(A, { model: "m2", chunks: [{ text: "移到 m2", vector: [0, 0, 1, 0] }] })] },
    { fn: "semantic.search", args: [vecQuery("m1", [1, 0, 0, 0], { limit: 20 })] },
    { fn: "semantic.search", args: [vecQuery("m2", [0, 0, 1, 0], { limit: 20 })] },
    { fn: "semantic.status", args: ["m1"] },
    { fn: "semantic.applyEmbeddings", args: [applyArgs(B, { chunks: [{ text: "B 新块", vector: [0, 1, 0, 0] }] })] },
    { fn: "semantic.search", args: [vecQuery("m1", [0, 1, 0, 0], { limit: 20 })] },
  ],
  "semantic.clear": [
    { fn: "semantic.search", args: [vecQuery("m1", [1, 0, 0, 0])] }, { fn: "semantic.clear", args: [] },
    { fn: "semantic.search", args: [vecQuery("m1", [1, 0, 0, 0])] }, { fn: "semantic.status", args: ["m1"] }, { fn: "semantic.listPending", args: [{ model: "m1", limit: 50 }] },
    { fn: "semantic.clear", args: [] },
  ],  sequence: [
    { fn: "update", args: [A, { tagNames: ["序列一", "序列二"] }] },
    { fn: "bulkUpdate", args: [[A, B], { addTagNames: ["序列三"], removeTagNames: ["序列一"] }] },
    { fn: "moveToTrash", args: [[A]] },
    { fn: "deleteForever", args: [[A]] },
  ],
};

function dumpAll(database: ReturnType<typeof initDatabase>) {
  const dump: Record<string, unknown[][]> = {};
  for (const [name, sql] of Object.entries(DUMP_SQL)) {
    dump[name] = (database.all(sql) as Array<Record<string, unknown>>).map((row) => Object.values(row));
  }
  return dump;
}

const writes: Record<string, { ops: Op[]; results: unknown[]; dump: unknown }> = {};
for (const [name, ops] of Object.entries(scenarios)) {
  const work = path.join(out, `scenario-${name}.db`);
  fs.copyFileSync(name.startsWith("semantic.") ? semBaseDb : baseDb, work);
  const db = initDatabase(work);
  const daos: Record<string, Record<string, (...a: unknown[]) => unknown>> = {
    knowledge: new KnowledgeItemDB(db) as never,
    collection: new CollectionDB(db) as never,
    tag: new TagDB(db) as never,
    inbox: inboxHandlers(db),
    ask: askHandlers(db),
    ai: aiUsageHandlers(db),
    semantic: semanticHandlers(db),
  };
  const resolveArgs = (args: unknown[], results: Array<{ value?: unknown }>): unknown[] =>
    JSON.parse(JSON.stringify(args.map((x) => (x === undefined ? null : x))), (_k, v) =>
      v && typeof v === "object" && "__cursorOf" in v ? ((results[v.__cursorOf]?.value as { nextCursor?: string } | undefined)?.nextCursor ?? null) : v);
  const call = (fn: string, args: unknown[]) => {
    const [ns, method] = fn.includes(".") ? fn.split(".") : ["knowledge", fn];
    return daos[ns][method](...args);
  };  const results: unknown[] = [];
  for (const op of ops) {
    try {
      results.push({ value: (await call(op.fn, resolveArgs(op.args, results as never))) ?? null });
    } catch (error) {
      results.push({ error: (error as Error).message });
    }
  }
  writes[name] = { ops, results, dump: dumpAll(db) };
  closeDatabase();
  fs.rmSync(work, { force: true });
}
golden.dumpSql = DUMP_SQL;
golden.writes = writes;

// ── settings:get / settings:set：用真实的 settings.ipc.ts 处理函数（electron 换成桩）做基准 ──
const settingsIpc = await import("../../apps/desktop/src/main/ipc/settings.ipc.ts");
const { DEFAULT_SETTINGS } = await import("../../packages/shared/types/settings.ts");
type SettingsOp = { fn: "get" | "set"; args: unknown[] };
const flare = (extra: Record<string, unknown>) => ({ enabled: true, connection: "ssh", port: 9000, sshHost: "my-host.01", ...extra });
const settingsScenarios: Record<string, SettingsOp[]> = {
  getSeeded: [{ fn: "get", args: [] }],
  setBasic: [{ fn: "set", args: [{ theme: "light", autoSave: false, backupKeepCount: 5 }] }, { fn: "get", args: [] }],
  setRejected: [
    { fn: "set", args: [{ master_password: "x", theme: "dark", bogus: 1, ytDlpPath: "/bin/evil", ffmpegPath: "  " }] },
    { fn: "get", args: [] },
  ],
  setPathClear: [{ fn: "set", args: [{ ytDlpPath: "" }] }, { fn: "get", args: [] }],
  setFlareValid: [{ fn: "set", args: [{ flareSolverr: flare({}) }] }, { fn: "get", args: [] }],
  setFlareLocal: [{ fn: "set", args: [{ flareSolverr: { enabled: false, connection: "local", port: 8191, sshHost: "" } }] }],
  setFlareBadPort: [{ fn: "set", args: [{ theme: "light", flareSolverr: flare({ port: 70000 }) }] }],
  setFlareFloatPort: [{ fn: "set", args: [{ flareSolverr: flare({ port: 80.5 }) }] }],
  setFlareStringPort: [{ fn: "set", args: [{ flareSolverr: flare({ port: "9000" }) }] }],
  setFlareUntrimmedHost: [{ fn: "set", args: [{ flareSolverr: flare({ sshHost: " my-host " }) }] }],
  setFlareBadHost: [{ fn: "set", args: [{ flareSolverr: flare({ sshHost: "-bad host" }) }] }],
  setFlareMissingHost: [{ fn: "set", args: [{ flareSolverr: { enabled: true, connection: "ssh", port: 9000 } }] }],
  setFlareSshEmptyHost: [{ fn: "set", args: [{ flareSolverr: flare({ sshHost: "" }) }] }],
  setFlareBadConnection: [{ fn: "set", args: [{ flareSolverr: flare({ connection: "x" }) }] }],
  setFlareEnabledString: [{ fn: "set", args: [{ flareSolverr: flare({ enabled: "yes" }) }] }],
  setFlareNotObject: [{ fn: "set", args: [{ flareSolverr: "x" }] }, { fn: "set", args: [{ flareSolverr: null }] }, { fn: "set", args: [{ flareSolverr: [] }] }],
  setNullsAndProxy: [
    { fn: "set", args: [{ backgroundImageFileName: null, networkProxy: { mode: "manual", protocol: "socks5", host: "h", port: 1080, username: "u", password: "p", bypass: "" } }] },
    { fn: "get", args: [] },
  ],
  setUnicode: [{ fn: "set", args: [{ backgroundImageFileName: "背景 😀 <x> & \u2028", transcribeDiarize: true }] }, { fn: "get", args: [] }],
  setNullArg: [{ fn: "set", args: [null] }],
  setNoArg: [{ fn: "set", args: [] }],
  setEmpty: [{ fn: "set", args: [{}] }],
  setArray: [{ fn: "set", args: [[1, 2]] }],
  setString: [{ fn: "set", args: ["theme"] }],
};
const settingsDump = "SELECT key, value FROM settings ORDER BY key";

// ── AI 配置文件（config/ai-models.json）场景：用假密钥，文件落在隔离的 APPDATA 下 ──
const aiFileText = (body: Record<string, unknown>) =>
  JSON.stringify({ kind: "guizhi-ai-config", version: 1, updatedAt: "2026-01-01T00:00:00.000Z", providers: [], models: [], modelRouteDefaults: {}, ...body }, null, 2) + "\n";
const richAi = aiFileText({
  providers: [
    { id: "p1", name: "  供应商一  ", provider: "Claude Official", apiKey: "  test-key-1  ", apiUrl: " https://x.test/v1 ", enabled: false },
    { id: "p2", provider: "gemini-pro", apiProtocol: "bogus", apiKey: 42, apiUrl: "https://y.test", lastVerifiedAt: "2026-01-01T00:00:00Z" },
    { id: "p3", name: "   ", provider: "Local", apiProtocol: "openai", apiUrl: "https://generativelanguage.googleapis.com/v1", lastVerifiedAt: null },
  ],
  models: [
    { id: "m1", providerId: " p1 ", provider: "Claude Official", apiUrl: "https://x.test/v1", model: "claude-x", capabilities: { vision: true, custom: 1, chat: false }, isDefault: true },
    { id: "m2", provider: "Local", apiUrl: "https://api.anthropic.com", model: "chat-a", apiKey: "test-key-2", isDefault: false },
    { id: "m3", name: " 名 ", provider: "Local", apiProtocol: "gemini", apiUrl: "u", model: "m3", isDefault: true, capabilities: { chat: true, embedding: true }, enabled: false },
  ],
  modelRouteDefaults: { mainText: " m2 ", fastText: "  ", bogusRoute: "x", imageGen: "m1", embedding: 5 },
});
const aiGoodProvider = { id: "np1", name: " 新供应商 ", provider: "OpenAI", apiKey: " test-key-new ", apiUrl: "https://new.test/v1" };
const aiGoodModel = { id: "nm1", provider: "OpenAI", apiUrl: "https://new.test/v1", model: "gpt-x", isDefault: true, capabilities: { vision: true } };
const legacyPayload = { aiProvider: " OpenAI ", aiApiProtocol: "openai", aiApiKey: " test-key-legacy ", aiApiUrl: " https://z.test ", aiModel: " gpt-legacy " };
const aiScenarios: Record<string, { ai: string | null; ops: SettingsOp[] }> = {
  aiAbsentGet: { ai: null, ops: [{ fn: "get", args: [] }] },
  aiRichGet: { ai: richAi, ops: [{ fn: "get", args: [] }] },
  aiBadKind: { ai: JSON.stringify({ kind: "x", version: 1 }), ops: [{ fn: "get", args: [] }, { fn: "set", args: [{ aiModels: [] }] }] },
  aiBadVersion: { ai: JSON.stringify({ kind: "guizhi-ai-config", version: 2 }), ops: [{ fn: "get", args: [] }, { fn: "set", args: [{ aiModels: [] }] }] },
  aiBadJson: { ai: "{bad", ops: [{ fn: "get", args: [] }, { fn: "set", args: [{ aiModels: [] }] }] },
  aiNullRoot: { ai: "null", ops: [{ fn: "set", args: [{ aiModels: [] }] }] },
  aiNumberRoot: { ai: "5", ops: [{ fn: "set", args: [{ aiModels: [] }] }] },
  aiBadProviderInFile: {
    ai: aiFileText({ providers: [{ id: "p", provider: "", apiUrl: "u" }] }),
    ops: [{ fn: "get", args: [] }, { fn: "set", args: [{ aiModels: [] }] }],
  },
  aiNoChatModels: {
    ai: aiFileText({ models: [{ id: "e1", provider: "P", apiUrl: "u", model: "emb", capabilities: { chat: false } }] }),
    ops: [{ fn: "get", args: [] }],
  },
  aiRoutesOnly: { ai: aiFileText({ modelRouteDefaults: { mainText: "m" } }), ops: [{ fn: "get", args: [] }] },
  aiSetFull: {
    ai: null,
    ops: [{ fn: "set", args: [{ aiProviders: [aiGoodProvider], aiModels: [aiGoodModel], modelRouteDefaults: { mainText: " nm1 ", fastText: "nm1" } }] }, { fn: "get", args: [] }],
  },
  aiSetProvidersOnly: { ai: richAi, ops: [{ fn: "set", args: [{ aiProviders: [aiGoodProvider] }] }, { fn: "get", args: [] }] },
  aiSetNullLists: { ai: richAi, ops: [{ fn: "set", args: [{ aiProviders: null, aiModels: null }] }, { fn: "get", args: [] }] },
  aiSetLegacy: { ai: null, ops: [{ fn: "set", args: [legacyPayload] }, { fn: "get", args: [] }] },
  aiSetLegacyIncomplete: { ai: null, ops: [{ fn: "set", args: [{ ...legacyPayload, aiApiKey: "  " }] }] },
  aiSetLegacyIgnoredWhenModels: { ai: richAi, ops: [{ fn: "set", args: [legacyPayload] }, { fn: "get", args: [] }] },
  aiSetLegacyBadTypes: { ai: null, ops: [{ fn: "set", args: [{ ...legacyPayload, aiProvider: 5 }] }] },
  aiSetLegacyFalsyProtocol: { ai: null, ops: [{ fn: "set", args: [{ ...legacyPayload, aiApiProtocol: "" }] }] },
  aiSetRoutesArray: { ai: richAi, ops: [{ fn: "set", args: [{ modelRouteDefaults: [1] }] }, { fn: "get", args: [] }] },
  aiSetRoutesNull: { ai: richAi, ops: [{ fn: "set", args: [{ modelRouteDefaults: null }] }, { fn: "get", args: [] }] },
  aiSetInvalidProvider: { ai: richAi, ops: [{ fn: "set", args: [{ aiProviders: [{ id: "x" }] }] }] },
  aiSetInvalidProviderUrl: { ai: richAi, ops: [{ fn: "set", args: [{ aiProviders: [{ id: "x", provider: "P" }] }] }] },
  aiSetInvalidProviderId: { ai: richAi, ops: [{ fn: "set", args: [{ aiProviders: [{ provider: "P", apiUrl: "u" }] }] }] },
  aiSetInvalidModelUrl: { ai: richAi, ops: [{ fn: "set", args: [{ aiModels: [{ id: "a", model: "m", provider: "p" }] }] }] },
  aiSetInvalidModelProvider: { ai: richAi, ops: [{ fn: "set", args: [{ aiModels: [{ id: "a", model: "m", apiUrl: "u" }] }] }] },
  aiSetInvalidModelId: { ai: richAi, ops: [{ fn: "set", args: [{ aiModels: [{ model: "m", provider: "p", apiUrl: "u" }] }] }] },
  aiSetNullElement: { ai: richAi, ops: [{ fn: "set", args: [{ aiProviders: [null] }] }] },
  aiSetStringElement: { ai: richAi, ops: [{ fn: "set", args: [{ aiProviders: ["x"] }] }] },
  aiSetNonStringModel: { ai: richAi, ops: [{ fn: "set", args: [{ aiModels: [{ id: "a", model: 5, provider: "p", apiUrl: "u" }] }] }] },
  aiSetProtocolInference: {
    ai: null,
    ops: [{ fn: "set", args: [{ aiModels: [
      { id: "i1", provider: "Anthropic", apiUrl: "u", model: "a" },
      { id: "i2", provider: "x", apiUrl: "https://API.ANTHROPIC.COM/v1", model: "b" },
      { id: "i3", provider: "Google AI", apiUrl: "u", model: "c" },
      { id: "i4", provider: "x", apiUrl: "u", model: "d", apiProtocol: "openai", name: "  ", providerId: "  ", capabilities: null },
      { id: "i5", provider: "x", apiUrl: "u", model: "e", apiProtocol: "gemini", capabilities: { chat: "no" }, enabled: false, isDefault: "true" },
    ] }] }, { fn: "get", args: [] }],
  },
  aiSetWithOtherKeys: {
    ai: null,
    ops: [{ fn: "set", args: [{ theme: "light", aiModels: [aiGoodModel], backupKeepCount: 3, networkProxy: { mode: "direct", protocol: "http", host: "", port: 1, username: "", password: "", bypass: "" } }] }, { fn: "get", args: [] }],
  },
  aiSetWithBadFlare: { ai: null, ops: [{ fn: "set", args: [{ theme: "light", aiModels: [aiGoodModel], flareSolverr: "x" }] }, { fn: "get", args: [] }] },
  aiSetUnicode: { ai: null, ops: [{ fn: "set", args: [{ aiProviders: [{ id: "u1", name: "名 😀 <b> & \u2028", provider: "P", apiUrl: "https://u.test" }] }] }] },
};
const allSettingsScenarios: Record<string, { ai: string | null; ops: SettingsOp[] }> = {
  ...Object.fromEntries(Object.entries(settingsScenarios).map(([k, ops]) => [k, { ai: null, ops }])),
  ...aiScenarios,
};
const aiConfigDir = path.join(isolatedAppData, "GuiZhi", "config");
const aiConfigFile = path.join(aiConfigDir, "ai-models.json");
const settingsRuns: Record<string, { ai: string | null; ops: SettingsOp[]; results: unknown[]; dump: unknown[][]; aiAfter: string | null }> = {};
for (const [name, { ai, ops }] of Object.entries(allSettingsScenarios)) {
  const work = path.join(out, `settings-${name}.db`);
  fs.copyFileSync(baseDb, work);
  fs.rmSync(aiConfigDir, { recursive: true, force: true });
  if (ai !== null) {
    fs.mkdirSync(aiConfigDir, { recursive: true });
    fs.writeFileSync(aiConfigFile, ai, "utf8");
  }
  const db = initDatabase(work);
  ipcHandlers.clear();
  settingsIpc.registerSettingsIPC(db as never);
  const results: unknown[] = [];
  for (const op of ops) {
    try {
      const value = await ipcHandlers.get(`settings:${op.fn}`)!({}, ...op.args);
      results.push({ value: value ?? null });
    } catch (error) {
      results.push({ error: (error as Error).message });
    }
  }
  settingsRuns[name] = {
    ai, ops, results,
    dump: (db.all(settingsDump) as Array<Record<string, unknown>>).map((row) => Object.values(row)),
    aiAfter: fs.existsSync(aiConfigFile) ? fs.readFileSync(aiConfigFile, "utf8") : null,
  };
  closeDatabase();
  fs.rmSync(work, { force: true });
}
fs.rmSync(aiConfigDir, { recursive: true, force: true });
const seededGet = settingsRuns.getSeeded.results[0] as { value?: Record<string, unknown> };
if (!seededGet.value || "aiProviders" in seededGet.value || "aiModels" in seededGet.value) {
  throw new Error("基准里出现了 AI 配置：APPDATA 隔离失效，已中止以免泄露真实密钥");
}
if (!aiConfigDir.startsWith(out)) throw new Error("AI 配置目录不在隔离区内");
golden.settings = { defaults: DEFAULT_SETTINGS, dumpSql: settingsDump, runs: settingsRuns };

fs.writeFileSync(path.join(out, "golden.json"), JSON.stringify(golden, null, 2));
console.log("golden ok:", out, `fts=${fts.length} counts=${counts.length} list=${listCases.length} writes=${Object.keys(writes).length}`);
