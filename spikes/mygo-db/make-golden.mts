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
import { saveKnowledgeDraft } from "../../packages/db/src/knowledge-draft.ts";
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
const ipcOn = new Map<string, (...args: unknown[]) => unknown>();
const dialogQueue: Array<{ canceled: boolean; filePaths: string[] }> = [];
const saveQueue: Array<{ canceled: boolean; filePath?: string }> = [];
const dialogCalls: Array<{ kind: "open" | "save"; options: Record<string, unknown> }> = [];
const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy(
  {
    ipcMain: {
      handle: (ch: string, fn: (...a: unknown[]) => unknown) => ipcHandlers.set(ch, fn),
      on: (ch: string, fn: (...a: unknown[]) => unknown) => ipcOn.set(ch, fn),
      removeAllListeners: (ch: string) => ipcOn.delete(ch),
      removeHandler() {},
    },
    app: { getPath: (_name: string) => path.join(isolatedAppData, "GuiZhi"), getVersion: () => "0.28.0" },
    dialog: {
      showOpenDialog: async (...a: unknown[]) => { dialogCalls.push({ kind: "open", options: a[a.length - 1] as Record<string, unknown> }); return dialogQueue.shift(); },
      showSaveDialog: async (...a: unknown[]) => { dialogCalls.push({ kind: "save", options: a[a.length - 1] as Record<string, unknown> }); return saveQueue.shift(); },
    },
  } as Record<string | symbol, unknown>,
  { get: (t, p) => (p in t ? t[p] : noop) },
);
const moduleLoader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = moduleLoader._load;
moduleLoader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "virtual:reading-libraries") return { __esModule: true, default: { compiler: "", animation: "", components: {}, runtime: {} } }; // vite 虚拟模块，导入服务的依赖链会带上它
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
}// 后台任务：真实的 BackgroundJobRuntime + background-job.ipc.ts；时钟、租约持有者、推送都由场景控制
const jobRuntimeMod = await import("../../apps/desktop/src/main/services/background-jobs.ts");
const jobIpc = await import("../../apps/desktop/src/main/ipc/background-job.ipc.ts");
function jobHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  let clock = 1_700_000_000_000;
  const sent: unknown[] = [];
  const handled: unknown[] = [];
  const runtime = new jobRuntimeMod.BackgroundJobRuntime(db as never, {
    now: () => clock, ownerId: "OWNER", sendRendererJob: (job: unknown) => sent.push(job),
  });
  jobIpc.registerBackgroundJobIPC(runtime);
  const wrap = (channel: string) => (...args: unknown[]) => (ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args);
  const drain = () => ({ sent: sent.splice(0), handled: handled.splice(0) });
  return {
    clock: (ms: unknown) => { clock = ms as number; return null; },
    register: (kind: unknown, behavior: unknown) => {
      runtime.registerHandler(kind as never, async (job: unknown) => {
        handled.push(job);
        const [mode, arg] = String(behavior).split(":");
        if (mode === "throw") throw new Error(arg ?? "boom");
        if (mode === "pause") return { pause: true, message: arg || undefined };
        if (mode === "next") return { nextRunAt: Number(arg) };
        if (mode === "nextUndefined") return { nextRunAt: undefined };
        return undefined;
      });
      return null;
    },
    schedule: (id: unknown, input: unknown) => runtime.schedule(id as string, input as never),
    unschedule: (kind: unknown, scope: unknown) => runtime.unschedule(kind as never, scope as string),
    tick: async () => { await runtime.tick(); return null; },
    settle: async () => { for (let i = 0; i < 5; i++) await new Promise((r) => setImmediate(r)); return drain(); },
    drain: () => drain(),
    list: wrap("backgroundJob:list"), sync: wrap("backgroundJob:syncRenderer"), renew: wrap("backgroundJob:renew"),
    complete: wrap("backgroundJob:complete"), fail: wrap("backgroundJob:fail"), pause: wrap("backgroundJob:pause"),
    resume: wrap("backgroundJob:resume"),
  };
}

// 导入队列：真实的 import.ipc.ts + ImportQueue + 持久化；只把「抽取」换成按输入文本决定行为的固定替身
const importIpc = await import("../../apps/desktop/src/main/ipc/import.ipc.ts");
function scrubTasks(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(scrubTasks);
  if (v && typeof v === "object") {
    const o: Record<string, unknown> = {};
    for (const [k, x] of Object.entries(v)) {
      o[k] = k === "stageStats" && Array.isArray(x) ? x.map((s) => (s as { stage?: unknown } | null)?.stage ?? null) : scrubTasks(x);
    }
    return o;
  }
  return v;
}
function importHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  const events: Array<Record<string, unknown>> = [];
  const holds = new Map<string, () => void>();
  const extract = async (task: { sourceKind: string; sourceInput: string }, signal: AbortSignal, onStage: (s: never) => void) => {
    const input = task.sourceInput;
    const rest = (prefix: string) => input.slice(prefix.length);
    if (input.startsWith("hold:")) {
      await new Promise<void>((resolve, reject) => {
        holds.set(input, resolve);
        signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
      });
    }
    if (input.startsWith("stage:")) { onStage("web-preparing" as never); onStage("web-verifying" as never); }
    if (input.startsWith("fail:")) throw new Error(rest("fail:"));
    if (input.includes("/captcha")) throw new Error("网页要求完成验证码");
    if (input.includes("/boom")) throw new Error("抓取失败 boom");
    const base = {
      title: input.startsWith("same:") ? "共同标题" : `标题 ${input}`,
      content: input.startsWith("same:") ? "共同内容正文" : input.startsWith("short:") ? "短" : `${input} 的正文内容，足够长以通过复核。`.repeat(3),
      itemType: input.startsWith("img:") ? "image" : "note",
      sourceUri: task.sourceKind === "url" ? input : null, // 真实连接器对非 URL 返回 null，undefined 会让 SQLite 绑定报错
      transcript: input.startsWith("tr:") ? `转写 ${input}` : undefined,
    };
    if (input.startsWith("degrade:")) return { ...base, degradedReason: rest("degrade:") };
    if (input.startsWith("warn:")) return { ...base, warningReason: rest("warn:") };
    return base;
  };
  importIpc.registerImportIPC(db as never, (task: unknown) => events.push(JSON.parse(JSON.stringify(task))), { extract } as never);
  const wrap = (channel: string) => (...args: unknown[]) => scrubTasks((ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args));
  const settle = async () => { for (let i = 0; i < 40; i++) await new Promise((r) => setImmediate(r)); return null; };
  return {
    enqueue: wrap("import:enqueue"), list: wrap("import:list"), state: wrap("import:queueState"), pause: wrap("import:pause"),
    resume: wrap("import:resume"), cancel: wrap("import:cancel"), retry: wrap("import:retry"), remove: wrap("import:remove"),
    clearFinished: wrap("import:clearFinished"), clearTerminalPreview: wrap("import:clearTerminalPreview"), clearTerminal: wrap("import:clearTerminal"),
    seed: (rows: unknown) => {
      for (const r of rows as Array<Record<string, unknown>>) {
        (db as { run: (sql: string, params: unknown[]) => void }).run(
          "INSERT INTO import_tasks (id, source_kind, source_input, display_name, status, stage, capture_strategy, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
          [r.id, r.kind ?? "text", r.input, r.input, r.status, r.stage ?? null, r.strategy ?? "standard", r.at ?? 1_700_000_000_000, r.at ?? 1_700_000_000_000]);
      }
      return null;
    },
    recover: () => { importIpc.getRegisteredImportService().queue.recover(); return null; },
    release: (name: unknown) => { holds.get(name as string)?.(); holds.delete(name as string); return null; },
    settle,
    // 事件按任务（以 sourceInput 区分）分组：任务之间的交错顺序不是契约，同一任务的状态序列才是
    events: () => {
      const byTask: Record<string, unknown[]> = {};
      for (const e of events.splice(0)) (byTask[e.sourceInput as string] ??= []).push(scrubTasks(e));
      return byTask;
    },
  };
}

// 批量选择/彻底删除：真实的 knowledge.ipc.ts（含资产清理）；selectionId 是随机 uuid，统一抹成 SEL
const knowledgeIpc = await import("../../apps/desktop/src/main/ipc/knowledge.ipc.ts");
function scrubSel(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(scrubSel);
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, k === "selectionId" && typeof x === "string" ? "SEL" : scrubSel(x)]));
  return v;
}
function selHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  knowledgeIpc.registerKnowledgeIPC(db as never);
  const wrap = (channel: string) => async (...args: unknown[]) => scrubSel(await (ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args));
  return { selection: wrap("knowledge:selection"), deleteForever: wrap("knowledge:deleteForever"), emptyTrash: wrap("knowledge:emptyTrash") };
}
// sel.* 场景的资产文件：每个场景开始前重建，结束后列出剩余文件作为比对基准
const selAssets = { images: ["sel-d.png", "sel-shared.png", "sel-crawl.png", "sel-keep.png", "sel-g.png", "sel-rev.png", "sel-web.png", "sel-snap.png", "sel-orphan.png"], videos: ["sel-g.mp4"] };
const selAssetRoot = path.join(isolatedAppData, "GuiZhi", "data", "assets");
function resetSelAssets() {
  fs.rmSync(selAssetRoot, { recursive: true, force: true });
  for (const [dir, names] of Object.entries(selAssets)) {
    fs.mkdirSync(path.join(selAssetRoot, dir), { recursive: true });
    for (const n of names) fs.writeFileSync(path.join(selAssetRoot, dir, n), "x");
  }
}
function listSelAssets(): string[] {
  const out: string[] = [];
  for (const dir of Object.keys(selAssets)) {
    const full = path.join(selAssetRoot, dir);
    if (fs.existsSync(full)) for (const n of fs.readdirSync(full)) out.push(`${dir}/${n}`);
  }
  return out.sort();
}

// ── 写入频道：每个场景在基准库的独立副本上依次执行一串操作，记录返回值与写后的原始表内容 ──
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
  wikiPages: "SELECT id, title, normalized_title, kind, summary, body, aliases_json, provider, model, prompt_version, generated_at, manual_edited_at, created_at, updated_at FROM wiki_pages",
  wikiLinks: "SELECT from_page_id, to_page_id, created_at FROM wiki_page_links",
  wikiSources: "SELECT page_id, item_id, created_at FROM wiki_page_sources",
  wikiRevisions: "SELECT id, page_id, title, kind, summary, body, aliases_json, model, prompt_version, created_at FROM wiki_page_revisions",
  wikiSuggestions: "SELECT page_id, draft_json, reason, created_at FROM wiki_page_suggestions",
  wikiFts: "SELECT page_id, title, summary, body FROM wiki_fts",
  wikiInvalid: "SELECT page_title FROM wiki_invalidated_pages",
  backgroundJobs: "SELECT id, kind, scope_id, state, payload_json, interval_minutes, next_run_at, attempt, lease_owner, lease_until, last_error, last_success_at, created_at, updated_at FROM background_jobs",
  aiUsage: "SELECT day, scenario, model, calls, failed_calls, prompt_tokens, completion_tokens, updated_at FROM ai_usage_daily",
  importTasks: "SELECT id, status, warning, warning_acknowledged_at, updated_at FROM import_tasks",
  // 导入队列：任务全列（stage_stats 只取阶段名，耗时是观测值）与入库产物
  importFull: "SELECT id, source_kind, source_input, display_name, status, stage, error, warning, warning_acknowledged_at, item_type, result_item_id, duplicate_item_id, collection_id, refresh_of_item_id, tag_names, (SELECT group_concat(CASE WHEN type = 'object' THEN json_extract(value, '$.stage') END, ',') FROM json_each(CASE WHEN json_valid(stage_stats) THEN stage_stats ELSE '[]' END)) AS stage_names, capture_strategy, comment_limit, force_duplicate, created_at, updated_at FROM import_tasks",
  sourceRecords: "SELECT item_id, source_type, source_uri, access_uri, normalized_uri, content_hash, platform, captured_at FROM source_records",
  sourceRevisions: "SELECT item_id, title, content, transcript, review_json, captured_at FROM source_capture_revisions",
  ftsMap: "SELECT m.item_id AS map_item, f.item_id AS fts_item FROM knowledge_fts_map m LEFT JOIN knowledge_fts f ON f.rowid = m.fts_rowid",
  // 批量选择：冻结集合（展开成逐条 id，集合 id 是随机的不比）、批量回执、各类版本/抓取页
  selectionSets: "SELECT s.created_at, j.key, j.value FROM knowledge_selection_sets s, json_each(s.ids_json) j",
  selectionSetCount: "SELECT COUNT(*) FROM knowledge_selection_sets",
  batchResults: "SELECT run_id, item_id, command_json, result_json, updated_at FROM knowledge_batch_results",
  webVersions: "SELECT id, item_id FROM web_source_versions WHERE id LIKE 'wv-%'",
  revisionIds: "SELECT id, item_id FROM source_capture_revisions WHERE id LIKE 'rv-%' OR id LIKE 'rev-%'",
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
// ── Wiki：真实的 wiki.ipc.ts；基准库另播种页面、链接、来源、历史版本与编译指纹 ──
const wikiIpc = await import("../../apps/desktop/src/main/ipc/wiki.ipc.ts");
function wikiHandlers(db: unknown): Record<string, (...a: unknown[]) => unknown> {
  wikiIpc.registerWikiIPC(db as never);
  const wrap = (channel: string) => (...args: unknown[]) => (ipcHandlers.get(channel) as (...a: unknown[]) => unknown)({}, ...args);
  return {
    catalog: wrap("wiki:catalog"), backlinkCounts: wrap("wiki:backlinkCounts"), search: wrap("wiki:search"), getPage: wrap("wiki:getPage"),
    updatePage: wrap("wiki:updatePage"), deletePage: wrap("wiki:deletePage"), status: wrap("wiki:status"), graph: wrap("wiki:graph"),
    listRevisions: wrap("wiki:listRevisions"), restoreRevision: wrap("wiki:restoreRevision"),
  };
}
const wikiBaseDb = path.join(out, "wiki-base.db");
const wikiFlushBaseDb = path.join(out, "wiki-flush-base.db");
{
  fs.copyFileSync(baseDb, wikiBaseDb);
  const wdb = initDatabase(wikiBaseDb);
  const page = (id: string, title: string, kind: string, summary: string, body: string, aliases: string | null, prompt: string, manual: number | null, at: number) =>
    wdb.run(
      "INSERT INTO wiki_pages(id,title,normalized_title,kind,summary,body,aliases_json,provider,model,prompt_version,generated_at,manual_edited_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
      [id, title, title.toLowerCase(), kind, summary, body, aliases, "guizhi", "model-a", prompt, at - 10, manual, at - 20, at],
    );
  page("p-01", "归知", "topic", "本地优先的个人知识库", "归知把 [[检索]] 与 [[Electron]] 放在一起。\n\n第二段。", '["GuiZhi","归知应用"]', "fulltext-v2", null, 1_700_000_100_000);
  page("p-02", "检索", "concept", "全文检索与语义检索", "检索分为全文与语义。见 [[归知]]。", null, "fulltext-v2", null, 1_700_000_200_000);
  page("p-03", "Electron", "entity", "桌面框架", "Electron 体积大。", "[]", "fulltext-v2", 1_700_000_250_000, 1_700_000_300_000);
  page("p-04", "MyGo", "entity", "Go 桌面框架", "MyGo 用 WebView2。", null, "old-v1", null, 1_700_000_300_000);
  page("p-05", "孤立页", "topic", "没有任何链接", "孤立。", null, "fulltext-v2", null, 1_700_000_050_000);
  for (const [from, to] of [["p-01", "p-02"], ["p-01", "p-03"], ["p-02", "p-01"], ["p-03", "p-02"], ["p-04", "p-02"], ["p-04", "p-03"]])
    wdb.run("INSERT INTO wiki_page_links VALUES(?,?,?)", [from, to, 1_700_000_000_000]);
  for (const [pg, item, at] of [["p-01", A, 3], ["p-01", G, 2], ["p-01", B, 1], ["p-02", C, 1], ["p-03", d.id, 1], ["p-04", e.id, 1], ["p-05", vid.id, 1]] as const)
    wdb.run("INSERT INTO wiki_page_sources VALUES(?,?,?)", [pg, item, 1_700_000_000_000 + at]);
  for (let n = 1; n <= 12; n++)
    wdb.run("INSERT INTO wiki_page_revisions VALUES(?,?,?,?,?,?,?,?,?,?)", [`r-${String(n).padStart(2, "0")}`, "p-01", `归知 v${n}`, "topic", `摘要 ${n}`, `旧正文 ${n} [[检索]]`, null, "model-a", "fulltext-v2", 1_699_999_000_000 + n]);
  wdb.run("INSERT INTO wiki_page_revisions VALUES(?,?,?,?,?,?,?,?,?,?)", ["r-20", "p-02", "检索旧", "concept", "旧摘要", "旧检索正文", '["Search"]', "model-a", "fulltext-v2", 1_699_999_500_000]);
  for (const [item, hash, prompt] of [[A, "h-a", "fulltext-v2"], [B, "h-b", ""], [C, "h-c", "fulltext-v2"], [G, "h-g", "fulltext-v2"], [d.id, "h-d", "fulltext-v2"]])
    wdb.run("INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,failure_count,next_attempt_at,updated_at) VALUES(?,?,?,?,?,?,?)", [item, hash, "model-a", prompt, prompt ? 0 : 2, null, 1_700_000_000_000]);
  wdb.run("UPDATE knowledge_items SET review_status = 'needs_review', review_reasons = ? WHERE id = ?", [JSON.stringify(["过短"]), B]);
  wdb.run("UPDATE knowledge_items SET review_reasons = 'not json' WHERE id = ?", [vid.id]);
  wdb.run("UPDATE knowledge_items SET title = '' WHERE id = ?", [d.id]);
  for (const { id, title, summary, body } of wdb.all("SELECT id, title, summary, body FROM wiki_pages") as Array<{ id: string; title: string; summary: string; body: string }>)
    wdb.run("INSERT INTO wiki_fts(page_id,title,summary,body) VALUES(?,?,?,?)", [id, segmentTextForFts(title), segmentTextForFts(summary), segmentTextForFts(body)]);
  closeDatabase();

  // 失效重建用的库：贡献与待重建标题。触发器会在更新条目时清空贡献，所以条目字段不再改动
  fs.copyFileSync(wikiBaseDb, wikiFlushBaseDb);
  const fdb = initDatabase(wikiFlushBaseDb);
  const draft = (title: string, kind: string, summary: string, body: string, linkTargets: string[], aliases: string | null = null) =>
    JSON.stringify({ title, normalizedTitle: title.toLowerCase(), kind, summary, aliasesJson: aliases, body, linkTargets });
  const contrib = (item: string, block: string, pageTitle: string, json: string) => fdb.run("INSERT INTO wiki_contributions VALUES(?,?,?,?)", [item, block, pageTitle, json]);
  contrib(C, "b1", "检索", draft("检索", "concept", "新摘要", "新检索正文 [[归知]] [[不存在]]", ["归知", "不存在", "检索"], '["Retrieval"]'));
  contrib(A, "b1", "检索", draft("检索", "concept", "另一摘要", "来自归知条目的检索正文", ["electron"]));
  contrib(A, "b2", "检索", draft("检索", "concept", "第三摘要", "第三块", ["归知"]));
  contrib(d.id, "b1", "electron", draft("Electron", "entity", "新框架摘要", "新 Electron 正文", ["mygo"]));
  contrib(i.id, "b1", "新页面", draft("新页面", "topic", "新页摘要", "新页正文 [[检索]] [[mygo]]", ["检索", "mygo", "新页面二"]));
  contrib(e.id, "b1", "新页面二", draft("新页面二", "entity", "第二个新页", "第二个新页正文 [[新页面]]", ["新页面"]));
  contrib(e.id, "b2", "mygo", draft("MyGo", "entity", "MyGo 新摘要", "MyGo 新正文", ["检索"]));
  contrib(G, "b1", "归知", draft("归知", "topic", "回收站来源", "不应出现", []));
  for (const t of ["检索", "electron", "mygo", "新页面", "新页面二", "孤立页", "幽灵", "归知"]) fdb.run("INSERT INTO wiki_invalidated_pages VALUES(?)", [t]);
  closeDatabase();
}
// 导入队列场景的基准库：去掉预置任务（它们会在启动恢复时被真实执行），其余数据保留
const importBaseDb = path.join(out, "import-base.db");
{
  fs.copyFileSync(baseDb, importBaseDb);
  const idb = initDatabase(importBaseDb);
  idb.run("DELETE FROM mobile_capture_tasks");
  idb.run("DELETE FROM import_tasks");
  closeDatabase();
}
// 批量选择/彻底删除场景的基准库：D、G 在回收站；各处引用资产（正文、采集修订、网页版本、抓取页），
// E 与 D 共用 sel-shared.png / sel-keep.png（E 未删，所以不能被回收）；另放 25 份修订测 LIMIT 20
const selBaseDb = path.join(out, "sel-base.db");
const D = d.id, E = e.id;
{
  fs.copyFileSync(baseDb, selBaseDb);
  const sdb = initDatabase(selBaseDb);
  const setContent = (id: string, content: string) => sdb.run("UPDATE knowledge_items SET content = ? WHERE id = ?", [content, id]);
  sdb.run("UPDATE knowledge_items SET deleted_at = ? WHERE id = ?", [1_700_000_000_000, D]);
  setContent(D, "d 正文 local-image://sel-d.png local-image://sel-shared.png local-image://sel-crawl.png local-image://sel-keep.png");
  setContent(G, "g 正文 local-image://sel-g.png local-video://sel-g.mp4");
  setContent(E, "e 正文 local-image://sel-shared.png");
  sdb.run("INSERT INTO source_capture_revisions VALUES('rev-g', ?, '旧版', 'local-image://sel-rev.png', NULL, '[\"x\"]', 1700000000000)", [G]);
  for (let n = 0; n < 25; n++) {
    sdb.run("INSERT INTO source_capture_revisions VALUES(?, ?, ?, ?, ?, '[]', ?)", [`rv-${String(n).padStart(2, "0")}`, E, `修订 ${n}`, `内容 ${n}`, n % 2 ? `转写 ${n}` : null, 1_700_000_000_000 + (n % 5) * 1000]);
  }
  sdb.run("INSERT INTO web_source_versions VALUES('wv-g', ?, ?, 1700000000000)", [G, JSON.stringify({ markdown: "网页 local-image://sel-web.png", snapshot: { assets: [{ fileName: "sel-snap.png" }] } })]);
  sdb.run("INSERT INTO web_source_versions VALUES('wv-e', ?, ?, 1700000000000)", [E, JSON.stringify({ snapshot: { assets: [{ fileName: "sel-keep.png" }] } })]);
  sdb.run("INSERT INTO crawl_jobs VALUES('cj', '{}', 'done', 1700000000000, 1700000000000)");
  sdb.run("INSERT INTO crawl_pages VALUES('cp', 'cj', 'https://c.test/', 0, 0, 'done', ?)", [JSON.stringify({ result: { snapshot: { assets: [{ fileName: "sel-crawl.png" }] } } })]);
  sdb.run("INSERT INTO knowledge_selection_sets VALUES('old-set', '[\"x\"]', 1700000000000)");
  closeDatabase();
}
const P = (n: number) => `p-0${n}`;
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
  ],  // ── Wiki（fn 带 wiki. 前缀；wiki.flush* 用带失效记录的库）──
  "wiki.reads": [
    { fn: "wiki.catalog", args: [] }, { fn: "wiki.backlinkCounts", args: [] }, { fn: "wiki.status", args: [] }, { fn: "wiki.graph", args: [] },
    ...[P(1), P(2), P(3), P(4), P(5), "missing", "", null, undefined, 5, true, ["p-01"], { id: "p-01" }].map((id) => ({ fn: "wiki.getPage", args: [id] })),
    ...[P(1), P(2), P(5), "missing", null, undefined, 5].map((id) => ({ fn: "wiki.listRevisions", args: [id] })),
  ],
  "wiki.search": [
    ...["检索", "归知 知识库", "Electron", "桌面框架", "MyGo WebView2", "孤立", "全文检索与语义检索怎么做", "", "  ", "!!!", "[[归知]]", "NOT AND", "\"引号\""].map((q) => ({ fn: "wiki.search", args: [q] })),
    ...[null, undefined, 5, true, ["检索"], {}].map((q) => ({ fn: "wiki.search", args: [q] })),
    ...[1, 2, 3, 100, 0, -1, "2", "x", null, 2.5, 0.5].map((limit) => ({ fn: "wiki.search", args: ["检索 归知 Electron", limit] })),
  ],
  "wiki.update": [
    { fn: "wiki.updatePage", args: [{ pageId: P(1), body: "全新正文 [[检索]] [[electron]] [[归知]] [[不存在]] [[mygo]]", linkTargets: ["检索", "electron", "归知", "不存在", "mygo", "检索", 5, null] }] },
    { fn: "wiki.listRevisions", args: [P(1)] }, { fn: "wiki.getPage", args: [P(1)] }, { fn: "wiki.getPage", args: [P(2)] }, { fn: "wiki.backlinkCounts", args: [] },
    { fn: "wiki.search", args: ["全新正文"] }, { fn: "wiki.search", args: ["旧"] },
    { fn: "wiki.updatePage", args: [{ pageId: P(3), body: "放回自动", linkTargets: [], releaseToAuto: true }] },
    { fn: "wiki.updatePage", args: [{ pageId: P(2), body: "手动编辑 😀 emoji", linkTargets: ["归知"], releaseToAuto: "true" }] },
    { fn: "wiki.catalog", args: [] }, { fn: "wiki.graph", args: [] },
  ],
  "wiki.updateInvalid": [
    ...[null, undefined, 5, "x", [], {}, { pageId: "missing", body: "b" }, { pageId: 5, body: "b" }, { pageId: P(5) }, { pageId: P(5), body: 5, linkTargets: "x" },
      { pageId: [P(4)], body: ["a", "b"], linkTargets: { 0: "检索" } }, { pageId: P(4), body: "只改正文", linkTargets: [P(4), "mygo", "MyGo"] },
    ].map((x) => ({ fn: "wiki.updatePage", args: [x] })),
    { fn: "wiki.catalog", args: [] }, { fn: "wiki.listRevisions", args: [P(4)] }, { fn: "wiki.listRevisions", args: [P(5)] },
  ],
  "wiki.delete": [
    { fn: "wiki.deletePage", args: [P(1)] }, { fn: "wiki.catalog", args: [] }, { fn: "wiki.graph", args: [] }, { fn: "wiki.backlinkCounts", args: [] },
    { fn: "wiki.getPage", args: [P(1)] }, { fn: "wiki.listRevisions", args: [P(1)] }, { fn: "wiki.search", args: ["归知"] }, { fn: "wiki.getPage", args: [P(2)] },
    ...["missing", "", null, undefined, 5, [P(2)], { x: 1 }].map((x) => ({ fn: "wiki.deletePage", args: [x] })),
    { fn: "wiki.deletePage", args: [P(5)] }, { fn: "wiki.status", args: [] },
  ],
  "wiki.restore": [
    { fn: "wiki.restoreRevision", args: ["r-05"] }, { fn: "wiki.getPage", args: [P(1)] }, { fn: "wiki.listRevisions", args: [P(1)] }, { fn: "wiki.search", args: ["旧正文 5"] },
    { fn: "wiki.restoreRevision", args: ["r-20"] }, { fn: "wiki.getPage", args: [P(2)] },
    ...["missing", "", null, undefined, 5, true].map((x) => ({ fn: "wiki.restoreRevision", args: [x] })),
    { fn: "wiki.catalog", args: [] },
  ],
  "wiki.flushCatalog": [
    { fn: "wiki.catalog", args: [] }, { fn: "wiki.backlinkCounts", args: [] }, { fn: "wiki.status", args: [] },
    ...[P(1), P(2), P(3), P(4), P(5)].map((id) => ({ fn: "wiki.getPage", args: [id] })), { fn: "wiki.listRevisions", args: [P(2)] }, { fn: "wiki.graph", args: [] },
    { fn: "wiki.search", args: ["新页正文"] },
  ],
  "wiki.flushGetPage": [{ fn: "wiki.getPage", args: [P(3)] }, { fn: "wiki.catalog", args: [] }],
  "wiki.flushSearch": [{ fn: "wiki.search", args: ["新页正文 检索"] }, { fn: "wiki.catalog", args: [] }, { fn: "wiki.graph", args: [] }],
  "wiki.flushThenUpdate": [
    { fn: "wiki.updatePage", args: [{ pageId: P(2), body: "更新前未 flush", linkTargets: ["新页面"] }] }, { fn: "wiki.catalog", args: [] }, { fn: "wiki.getPage", args: [P(2)] },
  ],  // ── 编辑器草稿（fn 带 draft. 前缀；直接调用 DAO，IPC 只是一层转发）──
  "draft.ok": [
    { fn: "draft.save", args: [{ id: A, requestId: "r1", base: { title: a.title }, patch: { title: "新标题" } }] },
    { fn: "draft.save", args: [{ id: A, requestId: "r2", base: { content: "旧基线" }, patch: { content: "新正文" } }] },
    { fn: "draft.save", args: [{ id: B, requestId: "r3", base: { title: b.title, content: "采集" }, patch: { title: " 改 ", content: "改文" } }] },
    { fn: "draft.save", args: [{ id: C, requestId: "r4", base: {}, patch: {} }] },
    { fn: "draft.save", args: [{ id: C, requestId: "r5", base: 5, patch: 5 }] },
    { fn: "draft.save", args: [{ id: C, requestId: "r6", base: {}, patch: true }] },
    { fn: "draft.save", args: [{ id: C, requestId: "r7", base: [], patch: [] }] },
    { fn: "knowledge.get", args: [A] }, { fn: "knowledge.get", args: [B] },
  ],
  "draft.conflict": [
    { fn: "draft.save", args: [{ id: A, requestId: "c1", base: { title: "别人改之前" }, patch: { title: "我的标题" } }] },
    { fn: "draft.save", args: [{ id: A, requestId: "c2", base: { title: "别人改之前", content: "基线" }, patch: { title: a.title, content: "我的正文" } }] },
    { fn: "draft.save", args: [{ id: A, requestId: "c3", base: { title: "别人改之前" }, patch: { title: a.title } }] },
    { fn: "draft.save", args: [{ id: A, requestId: "c4", base: { content: "x" }, patch: { content: "y" } }] },
    { fn: "draft.save", args: [{ id: A, requestId: "c5", base: {}, patch: { title: "没有基线" } }] },
  ],
  "draft.tags": [
    { fn: "draft.save", args: [{ id: A, requestId: "t1", base: { tagNames: ["知识库", "本地"] }, patch: { tagNames: ["知识库", "新标签"] } }] },
    { fn: "knowledge.update", args: [B, { tagNames: ["桌面", "框架", "并发加的"] }] },
    { fn: "draft.save", args: [{ id: B, requestId: "t2", base: { tagNames: ["桌面", "框架"] }, patch: { tagNames: ["框架", " 新一 ", "NEW一"] } }] },
    { fn: "draft.save", args: [{ id: B, requestId: "t3", base: { tagNames: [" 框架 ", "新一"] }, patch: { tagNames: ["新一", "ＦＵＬＬ", "İstanbul", "ΑΣ"] } }] },
    { fn: "draft.save", args: [{ id: B, requestId: "t4", base: { tagNames: ["新一"] }, patch: { tagNames: [] } }] },
    { fn: "draft.save", args: [{ id: C, requestId: "t5", base: {}, patch: { tagNames: ["检索", "检索", "A", "a"] } }] },
    { fn: "draft.save", args: [{ id: C, requestId: "t6", base: { tagNames: ["检索", "FTS5"] }, patch: { tagNames: ["检索"], title: "同改标题" } }] },
  ],
  "draft.invalid": [
    ...[null, undefined, 5, "x", [], {}, { id: A }, { id: A, requestId: "r" }, { id: A, requestId: "r", base: {} }, { id: A, requestId: "r", patch: {} },
      { id: 1, requestId: "r", base: {}, patch: {} }, { id: A, requestId: 1, base: {}, patch: {} }, { id: A, requestId: "r", base: 0, patch: {} }, { id: A, requestId: "r", base: {}, patch: "" },
      { id: A, requestId: "r", base: {}, patch: "ab" }, { id: A, requestId: "r", base: {}, patch: { summary: "x" } }, { id: A, requestId: "r", base: { isPinned: true }, patch: {} },
      { id: A, requestId: "r", base: {}, patch: { title: 1 } }, { id: A, requestId: "r", base: {}, patch: { title: null } }, { id: A, requestId: "r", base: { content: 5 }, patch: {} },
      { id: A, requestId: "r", base: {}, patch: { tagNames: "x" } }, { id: A, requestId: "r", base: {}, patch: { tagNames: ["a", 1] } }, { id: A, requestId: "r", base: { tagNames: {} }, patch: {} },
      { id: A, requestId: "r", base: {}, patch: { tagNames: null } },
      { id: "missing", requestId: "r", base: {}, patch: { title: "x" } }, { id: G, requestId: "r", base: {}, patch: { title: "x" } },
    ].map((x) => ({ fn: "draft.save", args: [x] })),
  ],  sequence: [
    { fn: "update", args: [A, { tagNames: ["序列一", "序列二"] }] },
    { fn: "bulkUpdate", args: [[A, B], { addTagNames: ["序列三"], removeTagNames: ["序列一"] }] },
    { fn: "moveToTrash", args: [[A]] },
    { fn: "deleteForever", args: [[A]] },
  ],
};

// ── 后台任务场景：时钟由 job.clock 控制，T0 取 1.7e12（低于 Go 测试的 NOW 抹平阈值，时间戳逐位比较） ──
{
  const T0 = 1_700_000_000_000, MIN = 60_000;
  const j = (fn: string, ...args: unknown[]): Op => ({ fn: `job.${fn}`, args });
  const at = (ms: number) => j("clock", T0 + ms);
  const failN = (n: number, startMs: number, id: string) => {
    // 连续 n 次「到期 → 领取 → 失败」，每次把时钟推过上一次的重试等待
    const ops: Op[] = [];
    let t = startMs;
    for (let i = 0; i < n; i++) {
      ops.push(at(t), j("tick"), j("drain"), j("fail", { id, error: `第${i + 1}次失败` }), j("list"));
      t += 25 * 60 * MIN; // 超过最长 24 小时的重试间隔
    }
    return ops;
  };
  Object.assign(scenarios, {
    "job.renderer": [
      j("list"),
      j("sync", { wikiEnabled: true, semanticEnabled: true }), j("drain"),
      at(61_000), j("tick"), j("drain"), j("list"),
      j("renew", "renderer-semantic-index"), j("renew", "nope"), j("renew", ""), j("renew", "  "), j("renew", null), j("renew", 5),
      at(70_000), j("renew", "renderer-semantic-index"), j("list"),
      j("complete", "renderer-semantic-index"), j("complete", "renderer-semantic-index"), j("tick"), j("drain"),
      at(400_000), j("tick"), j("drain"), j("list"),
      j("complete", "renderer-wiki-compile"), j("list"),
    ],
    "job.fail": [
      j("sync", { wikiEnabled: true, semanticEnabled: false }),
      ...failN(6, 5 * MIN + 1, "renderer-wiki-compile"),
      at(200 * 60 * MIN), j("tick"), j("drain"),
      j("fail", { id: "renderer-wiki-compile", error: "手动暂停", pause: true }), j("list"),
      j("fail", { id: "renderer-wiki-compile", error: "再失败" }), j("fail", { id: "missing", error: "x" }),
      j("resume", "renderer-wiki-compile"), j("tick"), j("drain"), j("list"),
      j("pause", "renderer-wiki-compile"), j("list"), j("tick"), j("drain"),
      j("resume", "renderer-wiki-compile"), j("drain"), j("list"),
      j("pause", "missing"), j("resume", "missing"),
    ],
    "job.invalid": [
      ...[[], [null], [0], [""], ["x"], [{}], [{ wikiEnabled: true }], [{ wikiEnabled: true, semanticEnabled: "true" }], [{ wikiEnabled: 1, semanticEnabled: true }],
        [{ wikiEnabled: false, semanticEnabled: false }], [[true, true]]].map((a) => j("sync", ...a)),
      ...[[], [null], ["str"], [{}], [{ id: "a" }], [{ id: "", error: "x" }], [{ id: "a", error: 1 }], [{ id: 5, error: "x" }], [{ id: "a", error: "x", pause: "true" }]]
        .map((a) => j("fail", ...a)),
      ...[[], [null], [5], [" "], ["\u00a0\ufeff"], [{}], [["a"]]].flatMap((a) => [j("pause", ...a), j("resume", ...a), j("complete", ...a), j("renew", ...a)]),
      j("list"),
    ],
    "job.lease": [
      j("schedule", "x1", { kind: "semantic-index", scopeId: "s1", nextRunAt: T0 }), j("drain"),
      j("list"),
      at(30_000), j("tick"), j("drain"),
      at(100_000), j("tick"), j("drain"), j("list"),
      j("schedule", "x1", { kind: "semantic-index", scopeId: "s1", payload: { b: 1, a: 2, 2: 3, 1: { z: [1, 2] } }, intervalMinutes: 1.5 }), j("list"),
      j("schedule", "x2", { kind: "wiki-compile", scopeId: "s2", intervalMinutes: 0, nextRunAt: null }), j("drain"),
      j("schedule", "x3", { kind: "wiki-compile", scopeId: "s3", enabled: false }), j("list"),
      j("schedule", "x1", { kind: "semantic-index", scopeId: "s1", enabled: false }), j("list"),
      j("unschedule", "semantic-index", "s1"), j("unschedule", "semantic-index", "nope"), j("unschedule", "wiki-compile", "s3"), j("list"),
      j("complete", "x2"), j("list"),
      at(10 * MIN), j("tick"), j("drain"), j("complete", "x2"), j("list"),
    ],
    "job.main": [
      j("register", "backup", "ok"),
      j("register", "wiki-compile", "ok"),
      j("schedule", "b1", { kind: "backup", scopeId: "", intervalMinutes: 60, nextRunAt: T0 + 1 }), j("settle"), j("list"),
      at(10), j("schedule", "b0", { kind: "backup", scopeId: "once", nextRunAt: T0 }), j("settle"), j("list"),
      j("register", "backup", "throw:磁盘已满"),
      at(61 * MIN), j("tick"), j("settle"), j("list"),
      at(200 * 60 * MIN), j("tick"), j("settle"), j("list"),
      j("register", "backup", "throw"), at(40 * 60 * MIN * 24), j("tick"), j("settle"), j("list"),
      j("register", "backup", "pause:用户拒绝"), at(41 * 60 * MIN * 24), j("resume", "b1"), j("settle"), j("list"),
      j("register", "backup", "pause"), at(42 * 60 * MIN * 24), j("resume", "b1"), j("settle"), j("list"),
      j("register", "backup", "next:1704500000000"), at(43 * 60 * MIN * 24), j("resume", "b1"), j("settle"), j("list"),
      j("register", "backup", "nextUndefined"), at(44 * 60 * MIN * 24), j("resume", "b1"), j("settle"), j("list"),
      j("register", "backup", "next:1704500000000"), at(45 * 60 * MIN * 24), j("resume", "b0"), j("settle"), j("list"),
    ],
    "job.parallel": [
      j("register", "backup", "ok"), j("register", "platform-discovery", "throw:并发失败"),
      j("sync", { wikiEnabled: true, semanticEnabled: true }),
      j("schedule", "p1", { kind: "backup", scopeId: "a", nextRunAt: T0 }),
      j("schedule", "p2", { kind: "platform-discovery", scopeId: "a", nextRunAt: T0, intervalMinutes: 30 }),
      j("schedule", "p3", { kind: "platform-discovery", scopeId: "b", nextRunAt: T0 }), j("settle"), j("list"),
      at(6 * MIN), j("tick"), j("settle"), j("tick"), j("settle"), j("list"),
      at(16 * MIN), j("schedule", "p4", { kind: "backup", scopeId: "c", payload: { 序: "号" } }), j("settle"), j("list"),
    ],
  });
}

// ── 导入队列场景：抽取是固定替身，其余（校验、排队、去重、入库、事件、清理）走真实实现 ──
{
  const m = (fn: string, ...args: unknown[]): Op => ({ fn: `imp.${fn}`, args });
  const tk = (input: string) => ({ __taskOf: input });
  const txt = (input: string, extra: Record<string, unknown> = {}) => ({ kind: "text", input, ...extra });
  const url = (input: string, extra: Record<string, unknown> = {}) => ({ kind: "url", input, ...extra });
  const wrapUp: Op[] = [m("settle"), m("list"), m("events"), m("state")];
  Object.assign(scenarios, {
    "imp.basic": [
      m("enqueue", [txt("t1"), txt("tr:t2"), txt("short:t3", { tagNames: ["标签甲", " 标签甲 ", "新标签"] }), txt("t4", { collectionId: tech.id })]),
      ...wrapUp,
    ],
    "imp.status": [
      m("enqueue", [txt("stage:s1"), txt("fail:原因 一"), txt("degrade:降级原因"), txt("warn:转写失败"), txt("img:i1")]),
      ...wrapUp,
      m("retry", tk("fail:原因 一")), m("settle"), m("events"),
      m("retry", tk("stage:s1")), m("retry", "missing-id"), m("retry", tk("warn:转写失败"), { forceDuplicate: true }),
      ...wrapUp,
    ],
    "imp.dups": [
      m("enqueue", [url("https://dup.test/a?x=1")]), m("settle"),
      m("enqueue", [url("https://dup.test/a?x=1#frag")]), m("settle"),
      m("enqueue", [url("https://dup.test/a?x=1#force", { forceDuplicate: true })]), m("settle"),
      m("enqueue", [txt("same:1")]), m("settle"), m("enqueue", [txt("same:2")]), m("settle"),
      m("enqueue", [txt("same:3", { forceDuplicate: true })]), m("settle"),
      m("enqueue", [url("https://dup.test/boom"), url("https://dup.test/captcha")]), m("settle"),
      ...wrapUp,
    ],
    "imp.merge": [
      m("pause"), m("enqueue", [url("https://merge.test/a"), url("https://merge.test/a#2"), url("HTTPS://MERGE.test/a/")]),
      m("list"), m("events"), m("resume"), m("settle"), m("enqueue", [url("https://merge.test/a#force", { forceDuplicate: true })]), ...wrapUp,
    ],
    "imp.refresh": [
      m("enqueue", [url("https://r.test/x")]), m("settle"),
      m("enqueue", [url("https://r.test/x#again", { refreshOfItemId: { __itemOfTask: "https://r.test/x" } })]), m("settle"),
      m("enqueue", [url("https://r.test/other", { refreshOfItemId: { __itemOfTask: "https://r.test/x" } })]), m("settle"),
      m("enqueue", [url("https://r.test/y", { refreshOfItemId: "missing-item" })]), m("settle"),
      m("enqueue", [txt("not url", { refreshOfItemId: { __itemOfTask: "https://r.test/x" } })]), m("settle"),
      ...wrapUp,
    ],
    "imp.hold": [
      m("enqueue", [txt("hold:a"), txt("hold:b"), txt("hold:c")]), m("settle"), m("state"), m("events"),
      m("cancel", tk("hold:c")), m("cancel", tk("hold:a")), m("settle"), m("events"), m("state"),
      m("cancel", tk("hold:c")), m("cancel", "missing"), m("cancel", 5), m("cancel", null), m("cancel"),
      m("release", "hold:b"), m("settle"), m("events"),
      m("pause"), m("enqueue", [txt("p1")]), m("settle"), m("state"), m("resume"), m("settle"), m("state"),
      m("retry", tk("hold:a")), m("settle"), m("release", "hold:a"), ...wrapUp,
    ],
    "imp.pending-cancel": [
      m("pause"), m("enqueue", [txt("q1"), txt("q2")]), m("cancel", tk("q1")), m("resume"), ...wrapUp,
    ],
    "imp.retry-validation": [
      m("enqueue", [url("https://v.test/captcha"), url("https://www.xiaohongshu.com/explore/abc123", { captureStrategy: "authenticated" }), txt("fail:x")]), m("settle"),
      m("retry"), m("retry", ""), m("retry", 5), m("retry", null),
      m("retry", tk("fail:x"), { captureStrategy: "bogus" }), m("retry", tk("fail:x"), { captureStrategy: null }),
      m("retry", tk("fail:x"), { commentLimit: 5 }), m("retry", tk("fail:x"), { commentLimit: "10" }),
      m("retry", tk("fail:x"), { forceDuplicate: "yes" }), m("retry", tk("fail:x"), { verifyWeb: "true" }),
      m("retry", tk("fail:x"), { verifyWeb: true }),
      m("retry", tk("fail:x"), { captureStrategy: "authenticated" }),
      m("retry", tk("fail:x"), { commentLimit: 10 }),
      m("retry", "missing", { captureStrategy: "bogus" }), m("retry", "missing", { verifyWeb: "x" }), m("retry", "missing"),
      m("retry", tk("https://v.test/captcha"), { verifyWeb: true, captureStrategy: "authenticated" }),
      m("retry", tk("https://v.test/captcha"), { verifyWeb: true, commentLimit: 10 }),
      m("retry", tk("https://v.test/captcha"), { verifyWeb: true }), m("settle"), m("events"),
      m("retry", tk("https://www.xiaohongshu.com/explore/abc123"), { commentLimit: 10 }), m("settle"),
      m("retry", tk("https://www.xiaohongshu.com/explore/abc123"), { captureStrategy: "standard", commentLimit: 0, forceDuplicate: false }), m("settle"),
      ...wrapUp,
    ],
    "imp.enqueue-invalid": [
      m("enqueue"), m("enqueue", null), m("enqueue", "x"), m("enqueue", {}), m("enqueue", []), m("enqueue", [null]), m("enqueue", [5]), m("enqueue", [[]]),
      m("enqueue", [{ kind: "x", input: "a" }]), m("enqueue", [{ kind: "text", input: "   " }]), m("enqueue", [{ kind: "text", input: 5 }]), m("enqueue", [{ kind: "text" }]),
      m("enqueue", [txt("a", { captureStrategy: "bogus" })]), m("enqueue", [txt("a", { captureStrategy: null })]), m("enqueue", [txt("a", { captureStrategy: "authenticated" })]),
      m("enqueue", [url("https://example.org/a", { captureStrategy: "authenticated" })]), m("enqueue", [txt("a", { commentLimit: 10 })]),
      m("enqueue", [url("https://www.xiaohongshu.com/explore/c1", { commentLimit: 10 })]), m("enqueue", [url("https://www.xiaohongshu.com/explore/c1", { captureStrategy: "authenticated", commentLimit: 7 })]),
      m("enqueue", [txt("ok"), txt("  ")]),
      m("enqueue", Array.from({ length: 51 }, (_, n) => url(`https://www.xiaohongshu.com/explore/n${n}`, { captureStrategy: "authenticated" }))),
      m("enqueue", [txt("  两端留白  "), txt("a\u00a0b", { tagNames: ["x"] })]),
      ...wrapUp,
    ],
    "imp.clear": [
      m("pause"),
      m("enqueue", [txt("c-ok1"), txt("fail:c-fail"), txt("warn:c-warn"), txt("c-ok2"), url("https://clear.test/boom")]), m("resume"), m("settle"),
      m("pause"), m("enqueue", [txt("c-pending")]),
      m("clearTerminalPreview"), m("clearTerminalPreview", null), m("clearTerminalPreview", { scope: "bogus" }),
      m("clearTerminalPreview", { scope: "all" }), m("clearTerminalPreview", { scope: "filtered" }),
      m("clearTerminalPreview", { scope: "filtered", status: "failed" }), m("clearTerminalPreview", { scope: "filtered", status: "completed", query: "c-ok" }),
      m("clearTerminalPreview", { scope: "filtered", query: 5 }), m("clearTerminalPreview", { scope: "filtered", origin: "mobile" }), m("clearTerminalPreview", { scope: "filtered", origin: null }),
      m("clearTerminal", { scope: "filtered", status: "failed" }), m("list"),
      m("remove", tk("c-ok1")), m("remove", tk("c-ok1")), m("remove", "missing"), m("remove", 5), m("remove", null), m("remove"),
      m("clearFinished"), m("list"), m("clearFinished"),
      m("clearTerminal", { scope: "all" }), m("clearTerminal"), m("list"), m("state"),
    ],
    "imp.recover": [
      m("seed", [
        { id: "seed-1", input: "r1", status: "pending", at: 1_700_000_001_000 },
        { id: "seed-2", input: "r2", status: "processing", stage: "fetching", at: 1_700_000_002_000 },
        { id: "seed-3", kind: "url", input: "https://www.xiaohongshu.com/explore/rec1", status: "pending", strategy: "authenticated", at: 1_700_000_003_000 },
        { id: "seed-4", input: "r4", status: "completed", at: 1_700_000_004_000 },
        { id: "seed-5", input: "r5", status: "pending", at: 1_700_000_005_000 },
        { id: "seed-6", kind: "url", input: "https://rec.test/z", status: "processing", at: 1_700_000_006_000 },
      ]),
      m("recover"), m("recover"), ...wrapUp,
    ],
  });
}

// ── 批量选择 / 彻底删除 / 清空回收站（含磁盘资产回收、问答证据清除、回执幂等）──
{
  const s = (fn: string, ...args: unknown[]): Op => ({ fn: `sel.${fn}`, args });
  const sel = (input: unknown): Op => s("selection", input);
  const ex = (ids: unknown, command: unknown, runId?: unknown): Op => sel({ action: "execute", ids, command, runId });
  const res = (runId: unknown): Op => sel({ action: "results", runId });
  // 回执按 (updated_at, item_id) 排序：处理顺序与 id 顺序一致，同毫秒也不会换位
  const ord = (...x: string[]) => [...x].sort();
  const miss = ["zz-missing-1", "zz-missing-2"];
  const trashA: Op = { fn: "moveToTrash", args: [[A]] };
  Object.assign(scenarios, {
    "sel.delete-forever": [s("deleteForever", [D, G, "missing"])],
    "sel.delete-forever-steps": [s("deleteForever", [G]), s("deleteForever", [D]), s("deleteForever", [D])],
    "sel.delete-forever-live": [s("deleteForever", [E])],
    "sel.empty-trash": [s("emptyTrash"), s("emptyTrash")],
    "sel.empty-trash-evidence": [trashA, s("emptyTrash", { clearEvidence: true })],
    "sel.delete-evidence": [
      { fn: "moveToTrash", args: [[A, B]] }, s("deleteForever", [A], { clearEvidence: "yes" }), s("deleteForever", [B], { clearEvidence: true }),
      s("deleteForever", [], { clearEvidence: true }), s("deleteForever", null), s("deleteForever", "x", { clearEvidence: true }),
    ],
    "sel.freeze": [
      sel({ action: "freeze", query: { scope: "all" } }), sel({ action: "freeze", query: { scope: "trash" } }), sel({ action: "freeze", query: {} }),
      sel({ action: "freeze", query: { scope: "all", includeArchived: true } }), sel({ action: "freeze", query: { scope: "favorites" } }),
      sel({ action: "freeze", query: { scope: "archived" } }), sel({ action: "freeze", query: { scope: "uncategorized" } }),
      sel({ action: "freeze", query: { scope: "all", search: "检索" } }), sel({ action: "freeze", query: { scope: "all", search: "  桌面  ", searchMode: "recall" } }),
      sel({ action: "freeze", query: { scope: "all", search: "!!!" } }), sel({ action: "freeze", query: { scope: "all", search: "   " } }),
      sel({ action: "freeze", query: { scope: "all", excludedItemIds: [A, B] } }), sel({ action: "freeze", query: { scope: "all", excludedItemIds: [] } }),
      sel({ action: "freeze", query: { scope: "all", collectionScope: { ids: [tech.id], includeUncategorized: false } } }),
      sel({ action: "freeze", query: { scope: "all", collectionScope: { ids: [tech.id], includeUncategorized: true } } }),
      sel({ action: "freeze", query: { scope: "all", collectionScope: { ids: [], includeUncategorized: true } } }),
      sel({ action: "freeze", query: { scope: "all", collectionScope: { ids: [], includeUncategorized: false } } }),
      sel({ action: "freeze", query: { scope: "all", collectionId: tech.id } }), sel({ action: "freeze", query: { scope: "all", tagId: tagIdOf("桌面") } }),
      sel({ action: "freeze", query: { scope: "all", platform: "web" } }),
      sel({ action: "freeze", query: { scope: "all", sortBy: "title", sortOrder: "asc" } }), sel({ action: "freeze", query: { scope: "all", sortBy: "createdAt", sortOrder: "asc" } }),
      sel({ action: "freeze", query: { scope: "all", sortBy: "bogus" } }),
      sel({ action: "freeze", query: { scope: "all", search: "检索", excludedItemIds: [C], sortBy: "title" } }),
      sel({ action: "freeze" }), sel({ action: "freeze", query: null }),
    ],
    "sel.source-versions": [
      sel({ action: "source-versions", id: E }), sel({ action: "source-versions", id: G }), sel({ action: "source-versions", id: "missing" }),
      sel({ action: "source-versions", id: null }), sel({ action: "source-versions" }), sel({ action: "source-versions", id: 5 }),
    ],
    "sel.invalid": [
      sel(undefined), sel(null), sel({}), sel({ action: "bogus" }), sel("freeze"), sel({ action: "execute" }), sel({ action: "execute", command: null }),
      sel({ action: "execute", command: { kind: "trash" } }), sel({ action: "execute", ids: 5, command: { kind: "status", status: "active" } }),
      sel({ action: "execute", ids: { a: 1 }, command: { kind: "status", status: "active" } }),
      sel({ action: "execute", ids: "ab", command: { kind: "restore" } }), sel({ action: "execute", ids: null, command: { kind: "restore" } }),
      ex([A], { kind: "bogus" }, "inv"), res("inv"), ex([A], { kind: "update" }, "inv2"), ex([A], { kind: "update", patch: {} }), ex([A], "restore"),
      ex(["", null, 5, A, A], { kind: "restore" }), ex([A], { kind: "restore" }, ""), res(""), res(null), sel({ action: "results" }), res("none"),
    ],
    "sel.exec-update": [
      ex([...ord(A, B, F), A, ...miss], { kind: "update", patch: { isFavorite: true, status: "archived", addTagNames: ["批量新", "桌面"], removeTagNames: ["本地"] } }, "r1"),
      res("r1"),
      ex(ord(A, B, F), { kind: "update", patch: { isFavorite: true, status: "archived", addTagNames: ["批量新", "桌面"], removeTagNames: ["本地"] } }, "r1"),
      ex(ord(A, B), { kind: "update", patch: { isFavorite: false } }, "r1"), res("r1"),
      ex(ord(A, B), { kind: "update", patch: { collectionId: tech.id, isPinned: true, reviewStatus: "clear" } }),
      ex([G], { kind: "update", patch: { isFavorite: true } }, "r2"), res("r2"),
    ],
    "sel.exec-status-trash": [
      ex(ord(A, F), { kind: "status", status: "archived" }, "s1"), res("s1"),
      ex([...ord(A, B, G), ...miss], { kind: "trash" }, "t1"), res("t1"),
      ex(ord(A, B), { kind: "trash" }, "t1"), ex([G], { kind: "status", status: "active" }, "t2"), ex(ord(A, G), { kind: "restore" }, "t3"), res("t3"),
      ex(ord(A, F, G), { kind: "restore" }, "t3"), ex([A], { kind: "trash" }, "t4"), ex([A], { kind: "restore" }, "t4"), res("t4"),
    ],
    "sel.exec-delete": [
      ex(ord(A, D, G, E), { kind: "delete" }, "d1"), res("d1"), ex(ord(A, D, G, E), { kind: "delete" }, "d1"),
      ex([A], { kind: "trash" }), ex([A], { kind: "delete", clearEvidence: true }, "d2"), res("d2"), ex([A], { kind: "delete", clearEvidence: true }, "d2"),
      ex([B], { kind: "trash" }), ex([B], { kind: "delete", clearEvidence: false }),
    ],
    "sel.exec-delete-retry": [
      ex(ord(D, G), { kind: "delete" }, "d3"), ex(ord(D, G), { kind: "delete", clearEvidence: true }, "d3"), res("d3"),
      ex(ord(D, G), { kind: "delete" }, "d3"),
    ],
  });
}

function dumpAll(database: ReturnType<typeof initDatabase>) {
  const dump: Record<string, unknown[][]> = {};
  for (const [name, sql] of Object.entries(DUMP_SQL)) {
    dump[name] = (database.all(sql) as Array<Record<string, unknown>>).map((row) => Object.values(row));
  }
  return dump;
}

const writes: Record<string, { ops: Op[]; results: unknown[]; dump: unknown; assetFiles?: string[] }> = {};
for (const [name, ops] of Object.entries(scenarios)) {
  const work = path.join(out, `scenario-${name}.db`);
  if (name.startsWith("sel.")) resetSelAssets();
  fs.copyFileSync(name.startsWith("sel.") ? selBaseDb : name.startsWith("imp.") ? importBaseDb : name.startsWith("semantic.") ? semBaseDb : name.startsWith("wiki.flush") ? wikiFlushBaseDb : name.startsWith("wiki.") ? wikiBaseDb : baseDb, work);
  const db = initDatabase(work);
  const daos: Record<string, Record<string, (...a: unknown[]) => unknown>> = {
    knowledge: new KnowledgeItemDB(db) as never,
    collection: new CollectionDB(db) as never,
    tag: new TagDB(db) as never,
    inbox: inboxHandlers(db),
    ask: askHandlers(db),
    ai: aiUsageHandlers(db),
    semantic: semanticHandlers(db),
    wiki: wikiHandlers(db),
    draft: { save: (input: unknown) => saveKnowledgeDraft(db as never, input as never) } as never,
    job: jobHandlers(db),
    imp: name.startsWith("imp.") ? importHandlers(db) : {}, // 注册会触发启动恢复，只给导入场景开
    sel: name.startsWith("sel.") ? selHandlers(db) : {},
  };
  const resolveArgs = (args: unknown[], results: Array<{ value?: unknown }>): unknown[] =>
    JSON.parse(JSON.stringify(args.map((x) => (x === undefined ? null : x))), (_k, v) =>
      v && typeof v === "object" && "__cursorOf" in v ? ((results[v.__cursorOf]?.value as { nextCursor?: string } | undefined)?.nextCursor ?? null)
        : v && typeof v === "object" && "__taskOf" in v ? ((db.get("SELECT id FROM import_tasks WHERE source_input = ? ORDER BY created_at DESC, rowid DESC", v.__taskOf) as { id: string } | undefined)?.id ?? "missing-task")
        : v && typeof v === "object" && "__itemOfTask" in v ? ((db.get("SELECT result_item_id AS id FROM import_tasks WHERE source_input = ? ORDER BY created_at DESC, rowid DESC", v.__itemOfTask) as { id: string } | undefined)?.id ?? "missing-item") : v);
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
  writes[name] = { ops, results, dump: dumpAll(db), ...(name.startsWith("sel.") ? { assetFiles: listSelAssets() } : {}) };
  closeDatabase();
  fs.rmSync(work, { force: true });
}
fs.rmSync(selAssetRoot, { recursive: true, force: true }); // 不让 sel 场景的资产混进导出基准
// ── Markdown 批量导出：同一份库、同一批资产，TS 导出的目录树作为基准 ──
{
  const exportDbPath = path.join(out, "export-base.db");
  fs.copyFileSync(baseDb, exportDbPath);
  const edb = initDatabase(exportDbPath);
  const ids = (edb.all("SELECT id FROM knowledge_items WHERE deleted_at IS NULL ORDER BY created_at, id") as Array<{ id: string }>).map((r) => r.id);
  const coll = (id: string, name: string) => edb.run("INSERT INTO collections (id, name, icon, sort_order, created_at, updated_at) VALUES (?, ?, 'folder', 99, 1700000000000, 1700000000000)", [id, name]);
  coll("ex-con", "con"); coll("ex-Work", "Work"); coll("ex-work", "work"); coll("ex-blank", "  "); coll("ex-long", "长".repeat(70)); coll("ex-dot", "点.点. ");
  const upd = (n: number, set: Record<string, unknown>) => {
    const cols = Object.keys(set);
    edb.run(`UPDATE knowledge_items SET ${cols.map((c) => `${c} = ?`).join(", ")} WHERE id = ?`, [...cols.map((c) => set[c]), ids[n]]);
  };
  upd(0, { title: 'a/b:c*d?e"f<g>h|i', content: "图 local-image://img1.png 视频 local-video://vid1.mp4 缺失 local-image://missing.png 越界 local-image://.. 隐藏 local-image://.hid", collection_id: null });
  upd(1, { title: "a".repeat(59) + "😀😀", content: "x", transcript: "转写 文本\n第二行", is_favorite: 1, summary: '摘要 "引号" \\ 反斜杠', collection_id: "ex-blank" });
  upd(2, { title: "", collection_id: "ex-con" });
  upd(3, { title: "  ..点点.. ", collection_id: "ex-Work" });
  upd(4, { collection_id: "ex-work", content: "同一资产 local-image://img1.png 与 local-video://vid1.mp4" });
  upd(5, { title: "a\tb\nc   d\u00a0e", collection_id: "ex-long" });
  upd(6, { title: "end.", content: "", transcript: "只有转写", collection_id: "ex-dot" });
  upd(7, { content: "line1\r\nline2\n\n\n", summary: "" });
  upd(8, { title: "CON", content: "reserved title" });
  edb.run("INSERT INTO tags (id, name, color_key, created_at, updated_at) VALUES ('ex-tag', '导出标签\"', 'blue', 1700000000000, 1700000000000)");
  edb.run("INSERT INTO knowledge_item_tags (item_id, tag_id) VALUES (?, 'ex-tag')", [ids[0]]);
  closeDatabase();

  const assetRoot = path.join(isolatedAppData, "GuiZhi", "data", "assets");
  fs.mkdirSync(path.join(assetRoot, "images"), { recursive: true });
  fs.mkdirSync(path.join(assetRoot, "videos"), { recursive: true });
  fs.writeFileSync(path.join(assetRoot, "images", "img1.png"), Buffer.from([0x89, 0x50, 0x4e, 0x47, 1, 2, 3]));
  fs.writeFileSync(path.join(assetRoot, "videos", "vid1.mp4"), Buffer.from([0, 0, 0, 0x18, 0x66, 0x74, 0x79, 0x70]));
  fs.writeFileSync(path.join(assetRoot, "images", ".hid"), Buffer.from("hidden"));
  const goldenAssets = path.join(out, "export-assets");
  fs.rmSync(goldenAssets, { recursive: true, force: true });
  fs.cpSync(assetRoot, goldenAssets, { recursive: true });

  const { exportKnowledgeToMarkdown } = await import("../../apps/desktop/src/main/services/export-markdown.ts");
  const exportDir = path.join(out, "export-out");
  fs.rmSync(exportDir, { recursive: true, force: true });
  fs.mkdirSync(exportDir, { recursive: true });
  const exdb = initDatabase(exportDbPath);
  const stats = exportKnowledgeToMarkdown(exdb as never, exportDir);
  closeDatabase();
  const tree: Record<string, string> = {};
  const walkDir = (dir: string) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walkDir(full);
      else tree[path.relative(exportDir, full).split(path.sep).join("/")] = fs.readFileSync(full).toString("base64");
    }
  };
  walkDir(exportDir);
  golden.exportMarkdown = { stats, tree };
}

// ── MCP：检索/读取两个工具、AI 交接稿、MCP 范围解析、一键安装进 Cursor 的 jsonc 合并 ──
{
  const { searchKnowledge, readItem } = await import("../../apps/desktop/src/mcp/tools.ts");
  const { buildAiHandoff } = await import("../../packages/shared/utils/ai-handoff.ts");
  const { parseMcpScope, serializeMcpScope } = await import("../../packages/shared/utils/mcp-scope.ts");
  const { installIntoCursorAt } = await import("../../apps/desktop/src/main/services/mcp-install.ts");

  const toolDbPath = path.join(out, "mcp-tools.db");
  fs.copyFileSync(baseDb, toolDbPath);
  const tdb = initDatabase(toolDbPath);
  const all: { mode: "all"; allowedCollectionIds: string[]; allowUncategorized: boolean } = { mode: "all", allowedCollectionIds: [], allowUncategorized: true };
  const sel = (ids: string[], allowUncategorized: boolean) => ({ mode: "selected" as const, allowedCollectionIds: ids, allowUncategorized });
  const run = (fn: () => unknown) => { try { return { value: fn() }; } catch (error) { return { error: (error as Error).message }; } };
  const searchCases: Array<{ input: Record<string, unknown>; scope: unknown }> = [
    ...["", "   ", "检索", "  检索  ", "桌面框架 选型", "Electron", "backup", "BACKUP", "100%", "!!!", "检索 检索", "不存在的词语", "FTS5", "视频", "整理"].map((query) => ({ input: { query }, scope: all })),
    ...[1, 2, 3, 50, 100, 0, -5].map((limit) => ({ input: { query: "检索", limit }, scope: all })),
    ...["web", "douyin", "xiaohongshu", " douyin ", "bogus", ""].map((platform) => ({ input: { query: "检索", platform }, scope: all })),
    ...["技术", " 技术 ", "阅读", "空集合", "不存在", "", "  "].map((collection) => ({ input: { query: "检索", collection }, scope: all })),
    { input: { query: "检索" }, scope: sel([tech.id], false) }, { input: { query: "检索" }, scope: sel([tech.id], true) },
    { input: { query: "检索" }, scope: sel([], true) }, { input: { query: "检索" }, scope: sel([], false) },
    { input: { query: "检索", collection: "阅读" }, scope: sel([tech.id], true) }, { input: { query: "检索", collection: "技术" }, scope: sel([tech.id, reading.id], false) },
    { input: { query: "检索", collection: "技术", platform: "web" }, scope: all },
    { input: {}, scope: all }, { input: { query: undefined }, scope: all },
  ];
  const itemIdsForRead = [A, B, C, D, E, F, G, vid.id, longItem.id, emojiItem.id, h.id, i.id];
  const readCases: Array<{ input: Record<string, unknown>; scope: unknown }> = [
    ...itemIdsForRead.flatMap((id) => [{ input: { id }, scope: all }, { input: { id, includeFullText: false }, scope: all }]),
    ...["missing", "", "   ", ` ${A} `].map((id) => ({ input: { id }, scope: all })),
    { input: { id: A }, scope: sel([reading.id], false) }, { input: { id: A }, scope: sel([tech.id], false) }, { input: { id: E }, scope: sel([tech.id], true) },
    { input: { id: E }, scope: sel([tech.id], false) }, { input: { id: B }, scope: sel([], true) }, { input: {}, scope: all },
  ];
  const tools = {
    search: searchCases.map((c) => ({ ...c, result: run(() => searchKnowledge(tdb as never, c.input as never, c.scope as never)) })),
    read: readCases.map((c) => ({ ...c, result: run(() => readItem(tdb as never, c.input as never, c.scope as never)) })),
  };
  closeDatabase();

  // AI 交接稿：覆盖各 itemType、转写稿长短（含代理对、边界 180/181）、资产链接、元数据块、论坛逐楼小节、CRLF 与各种空白
  const ts0 = 1_700_000_000_000;
  const longTranscript = "转写内容。".repeat(60);
  const base = { title: "标题", content: "正文", itemType: "note", tags: [], createdAt: ts0 };
  const handoffItems: Array<Record<string, unknown>> = [
    base, { ...base, title: "" }, { ...base, title: 'He said "hi" \\ 反斜杠\n换行' },
    { ...base, sourceUri: "https://example.com/a?b=1&c=2", summary: "  摘要 \"引号\"  ", tags: [{ name: "甲" }, { name: "" }, { name: "乙\"丙" }], collectionName: "技术" },
    { ...base, summary: "   ", collectionName: "", sourceUri: "" },
    ...["video", "audio", "forum", "image", "note", "webpage", "other"].map((itemType) => ({ ...base, itemType })),
    { ...base, itemType: "video", transcript: "短转写" },
    { ...base, itemType: "video", transcript: "字".repeat(180) }, { ...base, itemType: "video", transcript: "字".repeat(181) },
    { ...base, itemType: "video", transcript: longTranscript }, { ...base, itemType: "video", transcript: "  " + longTranscript + "\n  " },
    { ...base, itemType: "video", transcript: "😀".repeat(100) }, { ...base, itemType: "video", transcript: "a" + "😀".repeat(100) },
    { ...base, itemType: "video", transcript: "多   空白\n\n换行\t制表\u00a0不换行\u3000全角".repeat(20) },
    { ...base, itemType: "video", transcript: "含图 local-image://x.png 与 ![图](local-image://y.png)" + "。".repeat(200) },
    { ...base, content: "图：![图 1](local-image://a.png) 与 [ 图2 ](local-video://v.mp4) 以及 ![](local-image://b.png) 裸引用 local-image://c.png 和 local-video://d_e-f.mp4。" },
    { ...base, content: "无资产" }, { ...base, content: "> 平台：抖音 · 作者：甲 · 时长：1:02\n> 原标题：旧标题\n> 简介：简介 · 含点\n> 文字稿来源：平台字幕\n\n\n正文第一行\n\n\n\n正文第二行" },
    { ...base, content: "> 平台：B站\n正文紧跟" }, { ...base, content: "> 平台： \n正文" }, { ...base, content: "> 平台:YouTube · 作者:乙 · 时长:12:52 · 其他:忽略\r\n正文" },
    { ...base, content: ">平台：小红书\n>作者：丙\n\n正文" }, { ...base, content: "普通引用\n> 平台：不是首行" },
    { ...base, itemType: "forum", content: "## 讨论总结\n总结内容\n\n## 讨论 1 楼\n第一楼内容\n第二行\n\n## 讨论回复\n更多回复\n\n## 其他小节\n保留" },
    { ...base, itemType: "forum", content: "## 讨论总结\n只有总结" }, { ...base, itemType: "forum", content: "## 讨论\n全是讨论\n\n\n\n" },
    { ...base, itemType: "forum", content: "##讨论 没空格\n  ## 讨论 缩进  \n内容\n## 讨论 \u3000全角空格\n内容二" },
    { ...base, content: "行一\r\n行二\r\n\r\n\r\n行三\r\n" }, { ...base, content: "\n\n\n仅换行\n\n\n" }, { ...base, content: "" }, { ...base, content: "   \n  " },
    { ...base, title: "😀 标题", content: "😀 正文", tags: [{ name: "😀" }] },
    { ...base, createdAt: 0 }, { ...base, createdAt: 1_735_689_599_999 }, { ...base, createdAt: 1_735_689_600_000 }, { ...base, createdAt: 4_102_444_800_000 },
    { ...base, itemType: "video", transcript: "文".repeat(1234), content: "> 平台：抖音\n## 讨论 x\n内容 local-image://q.png" },
    { ...base, itemType: "forum", transcript: "论坛转写 ".repeat(50), content: "## 讨论总结\n总\n## 讨论 1\n" + "楼层内容\n".repeat(400) },
  ];
  const handoff = handoffItems.flatMap((item) => [true, false].map((includeFullText) => ({ item, includeFullText, result: buildAiHandoff(item as never, { includeFullText }) })));

  const scopeRaws: unknown[] = [null, undefined, 5, "x", [], {}, { mode: "selected" }, { mode: "all", allowedCollectionIds: ["a", "a", "", 5, "b"] },
    { mode: "selected", allowedCollectionIds: ["x", "y", "x"], allowUncategorized: false }, { mode: "selected", allowedCollectionIds: "x", allowUncategorized: "no" },
    { mode: "SELECTED", allowUncategorized: true }, { allowUncategorized: false }, { mode: "selected", allowedCollectionIds: [null, 1, "k"], allowUncategorized: 0 }];
  const scopes = scopeRaws.map((raw) => ({ raw: raw === undefined ? "__undefined" : raw, parsed: parseMcpScope(raw), text: serializeMcpScope(parseMcpScope(raw)) }));

  // 一键安装：每种已有文件内容在临时路径各跑一次，记录结果与写后文件
  const spec = { command: "C:\\Program Files\\GuiZhi\\GuiZhi.exe", args: ["--mcp", "a b"], env: {} as Record<string, string> };
  const specEnv = { command: "/usr/bin/x", args: ["m.mjs"], env: { ELECTRON_RUN_AS_NODE: "1", K: "v \"q\"" } };
  const installCases: Array<{ name: string; before: string | null; spec: typeof spec | typeof specEnv }> = [
    { name: "nofile", before: null, spec }, { name: "empty", before: "", spec }, { name: "blank", before: "  \n\n", spec }, { name: "obj", before: "{}", spec },
    { name: "obj-nl", before: "{}\n", spec }, { name: "env", before: "{}", spec: specEnv },
    { name: "empty-servers", before: '{"mcpServers":{}}', spec }, { name: "oneline-other", before: '{"mcpServers":{"other":{"command":"x"}}}', spec },
    { name: "pretty-other", before: '{\n  "mcpServers": {\n    "other": {\n      "command": "x",\n      "args": ["a"]\n    }\n  }\n}\n', spec },
    { name: "pretty-replace", before: '{\n  "mcpServers": {\n    "guizhi": {\n      "command": "old",\n      "args": ["o"]\n    },\n    "other": { "command": "x" }\n  }\n}\n', spec },
    { name: "comments", before: '{\n  // 我的服务器\n  "mcpServers": {\n    "other": { "command": "x" }, // 行尾注释\n  },\n}\n', spec },
    { name: "block-comment", before: '/* head */\n{\n  "a": 1, /* mid */ "mcpServers": {}\n}\n', spec },
    { name: "no-servers", before: '{\n  "foo": 1\n}\n', spec }, { name: "no-servers-oneline", before: '{"foo": 1}', spec },
    { name: "tabs", before: '{\n\t"mcpServers": {\n\t\t"other": {"command": "x"}\n\t}\n}\n', spec },
    { name: "crlf", before: '{\r\n  "mcpServers": {\r\n    "other": { "command": "x" }\r\n  }\r\n}\r\n', spec },
    { name: "invalid", before: '{ "mcpServers": ', spec }, { name: "invalid2", before: "not json", spec }, { name: "trailing-garbage", before: '{} x', spec },
    { name: "array-root", before: "[]", spec }, { name: "null-servers", before: '{"mcpServers": null}', spec }, { name: "servers-array", before: '{"mcpServers": []}', spec },
    { name: "replace-oneline", before: '{"mcpServers":{"guizhi":{"command":"old"}}}', spec }, { name: "bom", before: '\ufeff{"mcpServers":{}}', spec },
    { name: "unicode-comment", before: '{\n  // 中文注释 😀\n  "x": "y"\n}', spec },
  ];
  const installDir = path.join(out, "mcp-install");
  fs.rmSync(installDir, { recursive: true, force: true });
  fs.mkdirSync(installDir, { recursive: true });
  const install = installCases.map((c) => {
    const dir = path.join(installDir, c.name);
    const file = path.join(dir, "mcp.json");
    if (c.before !== null) { fs.mkdirSync(dir, { recursive: true }); fs.writeFileSync(file, c.before); }
    const result = run(() => installIntoCursorAt(file, c.spec));
    const norm = (v: unknown) => JSON.parse(JSON.stringify(v ?? null).split(dir.replace(/\\/g, "\\\\")).join("<DIR>"));
    return {
      ...c, result: norm(result),
      after: fs.existsSync(file) ? fs.readFileSync(file).toString("base64") : null,
      backup: fs.existsSync(`${file}.guizhi-backup`) ? fs.readFileSync(`${file}.guizhi-backup`).toString("base64") : null,
    };
  });
  // stdio 协议层：起真实的 TS server 进程，让 Go 的 --mcp 与之逐条比对（含协议版本协商、zod 校验报错原文）。
  const { probe } = await import("./mcp-probe.mts");
  const mcpDataDir = path.join(out, "mcp-data");
  fs.mkdirSync(path.join(mcpDataDir, "data"), { recursive: true });
  fs.copyFileSync(baseDb, path.join(mcpDataDir, "data", "knowledge.db"));
  const initParams = (protocolVersion: unknown) => ({ protocolVersion, capabilities: {}, clientInfo: { name: "probe", version: "1" } });
  let rid = 0;
  const call = (name: unknown, args?: unknown) => ({ jsonrpc: "2.0", id: ++rid, method: "tools/call", params: args === undefined && name !== undefined ? { name } : { name, arguments: args } });
  const stdioRequests: Array<Record<string, unknown> | string> = [
    { jsonrpc: "2.0", id: ++rid, method: "tools/call", params: { name: "search_knowledge", arguments: { query: "检索" } } },
    ...["2024-10-07", "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25", "2026-07-28", "1999-01-01", 5].map((v) => ({ jsonrpc: "2.0", id: ++rid, method: "initialize", params: initParams(v) })),
    { jsonrpc: "2.0", id: ++rid, method: "initialize", params: {} },
    { jsonrpc: "2.0", id: ++rid, method: "initialize" },
    { jsonrpc: "2.0", method: "notifications/initialized" },
    { jsonrpc: "2.0", id: ++rid, method: "tools/list" },
    { jsonrpc: "2.0", id: ++rid, method: "tools/list", params: { cursor: "abc" } },
    { jsonrpc: "2.0", id: "s1", method: "ping" },
    { jsonrpc: "2.0", id: ++rid, method: "resources/list" },
    { jsonrpc: "2.0", id: ++rid, method: "server/discover" },
    { jsonrpc: "2.0", id: ++rid, method: "bogus/method" },
    { jsonrpc: "2.0", method: "bogus/notification" },
    "not json at all",
    { jsonrpc: "2.0", id: ++rid, method: "tools/call", params: {} },
    { jsonrpc: "2.0", id: ++rid, method: "tools/call" },
    call("nope", {}),
    call(5, {}),
    call("search_knowledge"),
    call("search_knowledge", {}),
    call("search_knowledge", null),
    call("search_knowledge", []),
    call("search_knowledge", "x"),
    call("search_knowledge", 5),
    call("search_knowledge", { query: 5 }),
    call("search_knowledge", { query: null }),
    call("search_knowledge", { query: ["a"] }),
    call("search_knowledge", { query: { a: 1 } }),
    call("search_knowledge", { query: true }),
    call("search_knowledge", { query: "检索", limit: 0 }),
    call("search_knowledge", { query: "检索", limit: 0.5 }),
    call("search_knowledge", { query: "检索", limit: 1.5 }),
    call("search_knowledge", { query: "检索", limit: 51 }),
    call("search_knowledge", { query: "检索", limit: -1 }),
    call("search_knowledge", { query: "检索", limit: 100.5 }),
    call("search_knowledge", { query: "检索", limit: "3" }),
    call("search_knowledge", { query: "检索", limit: null }),
    call("search_knowledge", { query: "检索", limit: 1 }),
    call("search_knowledge", { query: "检索", limit: 50 }),
    call("search_knowledge", { query: "检索", platform: "bad" }),
    call("search_knowledge", { query: "检索", platform: 3 }),
    call("search_knowledge", { query: "检索", platform: "web" }),
    call("search_knowledge", { query: "检索", platform: "local" }),
    call("search_knowledge", { query: "检索", collection: 5 }),
    call("search_knowledge", { query: "检索", collection: "技术" }),
    call("search_knowledge", { query: "检索", collection: "不存在" }),
    call("search_knowledge", { query: 5, limit: 0, platform: "x", collection: 1 }),
    call("search_knowledge", { limit: 0, platform: "x" }),
    call("search_knowledge", { query: "检索", extra: 1 }),
    call("search_knowledge", { query: "" }),
    call("search_knowledge", { query: "桌面框架 选型" }),
    call("search_knowledge", { query: "不存在的词语" }),
    call("read_item", { id: "nope" }),
    call("read_item", {}),
    call("read_item", { id: 5 }),
    call("read_item", { id: "x", includeFullText: "yes" }),
    call("read_item", { id: "x", includeFullText: null }),
    ...itemIdsForRead.flatMap((id) => [call("read_item", { id }), call("read_item", { id, includeFullText: false }), call("read_item", { id, includeFullText: true })]),
    { jsonrpc: "2.0", id: ++rid, method: "ping" },
  ];
  const hereDir = path.dirname((await import("node:url")).fileURLToPath(import.meta.url));
  const tsxBin = path.resolve(hereDir, "../../node_modules/.bin/tsx.CMD");
  const serverTs = path.resolve(hereDir, "../../apps/desktop/src/mcp/server.ts");
  const probed = await probe("cmd.exe", ["/c", tsxBin, serverTs], { GUIZHI_DATA_DIR: mcpDataDir }, stdioRequests);
  if (probed.stderr) console.error("[mcp stdio] TS server stderr:", probed.stderr.slice(0, 300));
  golden.mcp = { tools, handoff, scopes, install, stdio: { requests: stdioRequests, lines: probed.lines } };
}

{
  const { buildConfigGolden } = await import("./golden-config.mts");
  const cfg = await buildConfigGolden({ ipcHandlers, ipcOn, openQueue: dialogQueue, saveQueue, dialogCalls, userData: path.join(isolatedAppData, "GuiZhi"), out });
  golden.shortcuts = cfg.shortcuts;
  golden.configTransfer = cfg.configTransfer;
}

{
  const { buildLegacyGolden } = await import("./golden-legacy.mts");
  golden.legacyMigration = await buildLegacyGolden({ ipcHandlers, userData: path.join(isolatedAppData, "GuiZhi"), out, initDatabase, closeDatabase });
}

{
  const { buildDataPathGolden } = await import("./golden-datapath.mts");
  golden.dataPath = await buildDataPathGolden({ out });
}

{
  const { buildWindowGolden } = await import("./golden-window.mts");
  golden.window = await buildWindowGolden({ out });
}

{
  const { buildSchemaGolden } = await import("./golden-schema.mts");
  golden.schema = await buildSchemaGolden({ out });
}

{
  const { buildBackupGolden } = await import("./golden-backup.mts");
  golden.backup = await buildBackupGolden({ out });
}

{
  const { buildUpdaterGolden } = await import("./golden-updater.mts");
  golden.updater = await buildUpdaterGolden();
}

golden.dumpSql = DUMP_SQL;
golden.selAssets = selAssets;
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

// ── 运行时路径、诊断日志、本地图片：真实的 app-runtime.ipc.ts / image.ipc.ts ──
{
  const userData = path.join(isolatedAppData, "GuiZhi");
  const appRuntimeIpc = await import("../../apps/desktop/src/main/ipc/app-runtime.ipc.ts");
  const imageIpc = await import("../../apps/desktop/src/main/ipc/image.ipc.ts");
  ipcHandlers.clear();
  appRuntimeIpc.registerAppRuntimeIPC();
  imageIpc.registerImageIPC();
  const logsDir = path.join(userData, "logs");
  const imagesDir = path.join(userData, "data", "assets", "images");
  fs.rmSync(logsDir, { recursive: true, force: true });
  fs.rmSync(imagesDir, { recursive: true, force: true });
  fs.mkdirSync(path.join(imagesDir, "dir"), { recursive: true });
  const seedFiles: Record<string, number[]> = { "a.png": [137, 80, 78, 71, 1, 2, 3], "b.JPG": [255, 216, 255, 0, 255], "noext": [1], "空 格 名.png": [9, 9], "empty.gif": [] };
  for (const [name, bytes] of Object.entries(seedFiles)) fs.writeFileSync(path.join(imagesDir, name), Buffer.from(bytes));
  const srcDir = path.join(out, "image-src");
  fs.mkdirSync(srcDir, { recursive: true });
  const srcFiles: Record<string, number[]> = { "x1.png": [1, 1, 1], "x2.JPEG": [2, 2], "x3.txt": [3], "x4": [4], ".png": [5], "x5.WebP": [6, 6, 6, 6], "x6.tar.gif": [7], "gone.png": [8] };
  for (const [name, bytes] of Object.entries(srcFiles)) fs.writeFileSync(path.join(srcDir, name), Buffer.from(bytes));
  const src = (name: string) => path.join(srcDir, name);
  const call = async (channel: string, ...args: unknown[]) => {
    try { return { value: (await ipcHandlers.get(channel)!({}, ...args)) ?? null }; } catch (error) { return { error: (error as Error).message }; }
  };
  const readNames: unknown[] = ["a.png", "b.JPG", "noext", "空 格 名.png", "empty.gif", "missing.png", "dir", "../x", "a/b", "a\\b", "C:x", "", ".", "..", "a..b", null, 5, ["a.png"], "a.png\u0000", "tab\t", "a.png ", "A.PNG", "nul"];
  const reads = [];
  for (const n of readNames) reads.push({ arg: n ?? null, ...(await call("image:readBase64", n)) });
  // 保存：dialog 队列里放下一次选择结果；选择后才允许保存
  type SaveOp = { select?: string[] | "cancel"; save?: unknown };
  const saveOps: SaveOp[] = [
    { save: [src("x1.png")] },
    { select: [src("x1.png"), src("x2.JPEG"), src("x3.txt"), src("x5.WebP")] },
    { save: [src("x1.png"), src("x2.JPEG"), src("x3.txt"), src("x4"), src("x5.WebP"), src("x6.tar.gif"), "nonexist.png"] },
    { save: [src("x1.png")] },
    { select: "cancel" }, { save: [src("x1.png")] },
    { select: [src(".png"), src("x4"), src("x6.tar.gif"), src("gone.png")] },
    { save: [src(".png"), src("x4"), src("x6.tar.gif"), src("gone.png"), 5, null] },
    { select: [src("x1.png")] }, { save: "notarray" }, { save: null }, { save: [src("x1.png"), src("x1.png")] },
    { select: [src("x1.png").replaceAll("\\", "/")] }, { save: [src("x1.png")] }, { save: [] },
  ];
  const saves = [];
  for (const op of saveOps) {
    if (op.select !== undefined) {
      dialogQueue.push(op.select === "cancel" ? { canceled: true, filePaths: [] } : { canceled: false, filePaths: op.select });
      saves.push({ op, ...(await call("dialog:selectImage")) });
    } else {
      saves.push({ op, ...(await call("image:save", op.save)) });
    }
  }
  const files = fs.readdirSync(imagesDir).filter((n) => fs.statSync(path.join(imagesDir, n)).isFile()).map((n) => ({ name: n, bytes: [...fs.readFileSync(path.join(imagesDir, n))] }));
  // 日志
  const longMsg = "长".repeat(2500);
  const home = (await import("node:os")).homedir();
  const logEntries: unknown[] = [
    { scope: "backup", action: "自动备份", message: "失败了" }, { message: "只有消息" }, { scope: "  trim 我  ", action: " 动作 ", message: "  前后空白  " },
    { scope: "s", action: "a", message: "   " }, { scope: "s", action: "a", message: 5 }, { scope: "s", action: "a" }, { scope: "x".repeat(100), action: "y".repeat(100), message: "长字段" },
    { scope: "s", action: "a", message: longMsg }, { scope: "s", action: "a", message: "a".repeat(1999) + "😀" + "tail" }, { scope: 5, action: null, message: "scope 非字符串" },
    { scope: "s", action: "a", message: `EPERM: unlink '${home}\\x\\y' 与 '${home.toUpperCase()}\\z'` },
    { scope: "s", action: "a", message: "多行\n消息\t带制表符 \"引号\" \\反斜杠 </script>" }, { scope: "s", action: "a", message: "\u00a0\u2003 nbsp 两侧 \ufeff" },
    { scope: "s", action: "a", message: "extra", extra: { nested: 1 } },
    null, 5, "x", [], {}, { message: ["a"] }, true,
  ];
  ipcOn.get("log:appError")!({}, undefined);
  for (const e of logEntries) ipcOn.get("log:appError")!({}, e);
  const logFile = path.join(logsDir, "error.log");
  const logLines = fs.existsSync(logFile)
    ? fs.readFileSync(logFile, "utf8").split("\n").filter(Boolean).map((line) => { const r = JSON.parse(line); delete r.ts; delete r.pid; delete r.version; return r; })
    : [];
  golden.appRuntime = {
    userData, home, srcDir,
    paths: await call("app:getRuntimePaths"),
    seedFiles, srcFiles, reads, saveOps, saves, files, logEntries, logLines,
  };
  ipcHandlers.clear();
}
// ── 导入管线的纯函数基准：URL 规范化、来源标识、链接抠取、复核评估、文本连接器 ──
{
  const { normalizeUrl } = await import("../../apps/desktop/src/main/services/import/url-normalize.ts");
  const { sourceIdentity } = await import("../../apps/desktop/src/main/services/import/source-identity.ts");
  const { extractUrlsFromText } = await import("../../packages/shared/utils/url-text.ts");
  const { assessImportReview } = await import("../../apps/desktop/src/main/services/import/review-assessment.ts");
  const { extractContent } = await import("../../apps/desktop/src/main/services/import/connectors.ts");

  // localeCompare：可打印 ASCII 全排列的符号矩阵
  const ascii = Array.from({ length: 95 }, (_, i) => String.fromCharCode(32 + i));
  const localeMatrix = ascii.map((a) => ascii.map((b) => { const c = a.localeCompare(b); return c < 0 ? "-" : c > 0 ? "+" : "0"; }).join(""));
  // 多字符：大小写、前缀、标点混合
  const names = ["a", "A", "b", "B", "a_b", "a-b", "ab", "aB", "Ab", "AB", "a1", "a10", "a2", "_x", "-x", "x", "X", "utm", "UTM", "id", "ID", "Id", "page", "Page", "p", "p-1", "p_1", "p.1", "p1", "", "0", "00", "9", "z", "Z", "é", "e", "f", "中", "a中", "ａ"];
  const localePairs = names.flatMap((a) => names.map((b) => [a, b, Math.sign(a.localeCompare(b))]));

  const urls: string[] = [
    "https://Example.COM/Path/?b=2&a=1&utm_source=x#frag", "http://example.com:80/a/b/", "https://example.com:443", "https://example.com:8443/x/",
    "https://example.com/", "https://example.com", "https://example.com/?", "https://example.com/a?", "https://example.com/a/?x=", "https://example.com/a/?ref=1&source=2",
    "https://example.com/a?B=1&a=2&A=3&b=4", "https://example.com/a?a=1&a=0&A=2", "https://example.com/a?q=hello world&x=中文", "https://example.com/a?q=a+b%20c&r=%E4%B8%AD",
    "https://example.com/a?q=%ZZ&r=%", "https://example.com/a?_x=1&-x=2&x=3&1=4&=5", "https://example.com/a?utm_medium=1", "https://example.com/a/?utm_medium=1",
    "  https://example.com/a  ", "ftp://example.com/a", "example.com/a", "https://", "http://[::1]:80/a/", "https://user:pw@example.com/a/b/", "https://中文.com/路径/?参数=值",
    "https://example.com/a%2Fb/", "https://example.com/a/./b/../c/", "https://example.com//a//", "HTTPS://EXAMPLE.com/X", "https://example.com/a?x=1#", "https://example.com/#top",
    "https://example.com/a?x=a%2Bb&y=a+b", "https://example.com/a?refer=1&REF=2&Spm=3&vd_source=4&from_source=5&share_tag=6&SHARE_PLAT=7&fbclid=8&gclid=9", "http://1.2.3.4:8080/x/",
    "https://example.com/a?x=*&y=~&z=!&w='", "https://example.com/a?x=%7E%2A", "", " ", "https://example.com/a b", "https://exa mple.com/",
  ];
  const identityInputs: string[] = [
    ...urls,
    "https://www.nodeseek.com/post-123-1", "https://nodeseek.com/post-123-4/", "https://www.nodeseek.com/post-0-1", "https://www.nodeseek.com/post-123-1/x", "https://www.nodeseek.com/post-123",
    "https://www.xiaohongshu.com/explore/65A1B2C3D4E5F6A7B8C9D0E1?xsec_token=x", "http://xhslink.com/a/xxx，复制本条信息", "https://www.xiaohongshu.com/discovery/item/65a1b2c3d4e5f6a7b8c9d0e1",
    "https://www.xiaohongshu.com/explore/65a1b2c3d4e5f6a7b8c9d0e", "https://notxiaohongshu.com/explore/65a1b2c3d4e5f6a7b8c9d0e1",
    "https://www.douyin.com/video/7300000000000000001?x=1", "https://www.douyin.com/note/7300000", "https://www.douyin.com/jingxuan?modal_id=7300000000000000002", "https://www.douyin.com/jingxuan?modal_id=123",
    "https://www.douyin.com/jingxuan?modal_id=", "https://www.douyin.com/video/123", "https://www.douyin.com/video/12345?modal_id=7300000",
    "https://www.bilibili.com/video/BV1xx411c7mD/?p=2", "https://www.bilibili.com/video/bv1xx411c7md", "https://m.bilibili.com/video/BV1xx411c7mD", "https://www.bilibili.com/read/cv123",
    "https://youtu.be/dQw4w9WgXcQ?t=1", "https://youtu.be/", "https://youtu.be/short", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=x", "https://www.youtube.com/watch?v=", "https://www.youtube.com/watch?v=abc",
    "https://www.youtube.com/shorts/dQw4w9WgXcQ", "https://www.youtube.com/live/dQw4w9WgXcQ?feature=share", "https://www.youtube.com/watch?x=1&v=dQw4w9WgXcQ&v=other", "https://youtube.com/embed/dQw4w9WgXcQ",
    "看看这个 https://www.bilibili.com/video/BV1xx411c7mD?share_source=copy 很好", "没有链接的文本", "(见 https://example.com/a_(b))", "https://example.com/a).", "https://example.com/a]!",
  ];
  const textInputs: string[] = [
    "", "没有链接", "https://a.com", "HTTP://A.COM/x", "看 https://a.com/x，然后 https://b.com/y。再 https://a.com/x", "（https://a.com/x）", "[https://a.com/x]", "https://a.com/x).", "https://a.com/wiki/Foo_(bar)",
    "https://a.com/wiki/Foo_(bar)).", "https://a.com/x?y=1&z=2;", "https://a.com/x!!!", "https://a.com/中文路径", "https://", "http://", "https://\u00a0x", "https://a.com/x\u3000https://b.com/y",
    "https://a.com/x「引」https://b.com/y", "https://a.com/x'https://b.com/y", "https://a.com/a`b", "x https://a.com/x#frag. y", "https://a.com/x]]", "https://a.com/[x]]", "https://a.com/(((",
    "https://exa mple.com", "https://[::1]/x", "http://a b.com", "xhttps://a.com/p", "ahttps://a.com", "https://a.com?x", "https://a.com/x…", "https://😀.com/x", "https://a.com/😀.", "HtTpS://A.com/Q",
  ];
  const mk = (over: Record<string, unknown>) => ({ title: "t", content: "c", itemType: "note", sourceUri: null, ...over });
  const reviewCases: Array<[Record<string, unknown>, string]> = [
    [mk({ content: "" }), "url"], [mk({ content: "短", itemType: "webpage" }), "url"], [mk({ content: "短", itemType: "webpage" }), "text"], [mk({ content: "短", itemType: "note" }), "file"],
    [mk({ content: "短", itemType: "document" }), "file"], [mk({ content: "短", itemType: "forum" }), "url"], [mk({ content: "x".repeat(80), itemType: "webpage" }), "url"], [mk({ content: "x".repeat(79), itemType: "webpage" }), "url"],
    [mk({ content: "😀".repeat(40), itemType: "webpage" }), "url"], [mk({ content: "😀".repeat(39), itemType: "webpage" }), "url"],
    [mk({ content: "> 元数据\n> 更多\n\n正文很短", itemType: "webpage" }), "url"], [mk({ content: ">only quote", itemType: "webpage" }), "url"], [mk({ content: "> a\r\n> b\r\n\r\nbody", itemType: "webpage" }), "url"],
    [mk({ content: ">\n>\n  \u00a0 body", itemType: "document" }), "file"], [mk({ content: "![图](http://x/y.png) [链接](http://z) 文字", itemType: "webpage" }), "url"],
    [mk({ content: "![图\n片](http://x/y.png)", itemType: "webpage" }), "url"], [mk({ content: "[a](b)[c](d)", itemType: "document" }), "url"], [mk({ content: "`*_#>|~-` 符号 ** __", itemType: "webpage" }), "url"],
    [mk({ content: "含\uFFFD坏字符" }), "file"], [mk({ content: "含\u0000控制" }), "file"], [mk({ content: "含\u0001控制", itemType: "webpage" }), "url"], [mk({ content: "含\t制表\n换行\r回车\u000b\u000c" }), "file"], [mk({ content: "含\u000e" }), "file"],
    [mk({ content: "正常", warningReason: "转写失败" }), "url"], [mk({ content: "正常", warningReason: "  转写失败  " }), "url"], [mk({ content: "正常", warningReason: "   " }), "url"], [mk({ content: "含\uFFFD", warningReason: "解析文本含有不可识别字符，建议与原始文件核对。" }), "url"],
    [mk({ content: "短", itemType: "webpage", warningReason: "解析出的正文仅 1 字，建议确认是否漏掉了正文或结构内容。" }), "url"], [mk({ content: "a\u00a0\u2003\ufeff\u3000b", itemType: "webpage" }), "url"],
    [mk({ content: "  \n\t ", itemType: "webpage" }), "url"], [mk({ content: "- - -", itemType: "webpage" }), "url"],
  ];
  const textExtractInputs: string[] = [
    "", "  ", "标题\n正文", "# 标题\n正文", "###### 六级\nx", "####### 七级\nx", "#标题无空格", "#\u00a0nbsp 标题", "#\u3000全角空格标题", "# \n正文", "#", "  \n\n  首行在后\n第二行  ", "A\r\nB", "A\rB", "A\r",
    "x".repeat(120), "x".repeat(121), "😀".repeat(60), "😀".repeat(61), "# " + "字".repeat(130), `${"x".repeat(119)}😀尾`, "\ufeff带BOM", "标题\u2028第二行",
  ];
  const { detectVideoPlatform } = await import("../../packages/shared/utils/video-platforms.ts");
  const { detectForumPlatform } = await import("../../packages/shared/utils/forum-platforms.ts");
  const { resolveSourcePlatform } = await import("../../packages/shared/utils/source-platforms.ts");
  const { detectPlatformCapturePlatform } = await import("../../packages/shared/utils/platform-capture.ts");
  const { needsWebVerification } = await import("../../packages/shared/utils/web-verification.ts");
  const platformUrls: string[] = [
    ...identityInputs, "https://b23.tv/abc", "https://www.bilibili.com/bangumi/play/ep1", "https://evilbilibili.com/video/BV1", "https://m.youtube.com/watch?v=x", "https://www.youtube.com/channel/x",
    "https://v.douyin.com/abc/", "https://www.iesdouyin.com/share/video/1", "https://evildouyin.com/x", "https://xhslink.com/a/b", "http://xhslink.cn/x", "https://www.xiaohongshu.com/user/profile/1",
    "https://www.v2ex.com/t/1227616#reply1", "https://fakev2ex.com/t/1", "https://v2ex.com/t/abc", "https://bbs.nga.cn/read.php?tid=37194262&fav=1", "https://bbs.nga.cn/read.php?tid=abc", "https://ngabbs.com/read.php?tid=%2037194262%20",
    "https://nga.178.com/READ.PHP?tid=12", "https://linux.do/t/topic/2702071", "https://linux.do/t/2702071", "https://meta.linux.do/t/some-slug/2702071/5", "https://linux.do/t/a/b/c", "https://meta.appinn.net/t/topic/123", "https://appinn.net/t/topic/123",
    "https://2libra.com/post/node/AbC_-12", "https://www.2libra.com/post/node/AbC_-12/", "https://2libra.com/post/node/short", "https://nodeseek.com:8443/post-1-1", "https://user@www.nodeseek.com/post-1-1", "https://www.nodeseek.com/",
    "https://mp.weixin.qq.com/s/abc", "http://www.xiaohongshu.com/explore/1", "https://sub.linux.do/", "https://linux.do.evil.com/", "file:///c:/a", "mailto:a@b.c", "not a url", "",
  ];
  const wvTasks = [
    { status: "failed", sourceKind: "url", sourceInput: "https://a.com/x", error: "x web_verification_required y", displayName: "t" }, { status: "failed", sourceKind: "url", sourceInput: "https://a.com/x", error: null, displayName: "Just a moment..." },
    { status: "failed", sourceKind: "url", sourceInput: "https://a.com/x", error: "网页要求完成验证码", displayName: "t" }, { status: "failed", sourceKind: "url", sourceInput: "https://a.com/x", error: "403", displayName: "t" },
    { status: "completed", sourceKind: "url", sourceInput: "https://a.com/x", error: "web_verification_required", displayName: "t" }, { status: "failed", sourceKind: "text", sourceInput: "x", error: "web_verification_required", displayName: "t" },
    { status: "failed", sourceKind: "url", sourceInput: "https://www.douyin.com/video/1", error: "web_verification_required", displayName: "t" }, { status: "failed", sourceKind: "url", sourceInput: "https://a.com/x", error: null, displayName: "验证 人机验证" },
    { status: "failed", sourceKind: "url", sourceInput: "https://a.com/x", error: null, displayName: "人机验证中" }, { status: "failed", sourceKind: "url", sourceInput: "https://a.com/x", error: null, displayName: "VERIFY YOU ARE HUMAN" },
  ];  golden.importUtil = {
    localeMatrix, localePairs,
    normalize: urls.map((u) => [u, normalizeUrl(u)]),
    identity: identityInputs.map((u) => [u, sourceIdentity(u)]),
    urlText: textInputs.map((t) => [t, extractUrlsFromText(t)]),
    review: reviewCases.map(([e, k]) => [e, k, assessImportReview(e as never, k as never)]),
    textExtract: await Promise.all(textExtractInputs.map(async (t) => [t, await extractContent("text", t)])),
    platforms: platformUrls.map((u) => [u, detectVideoPlatform(u), detectForumPlatform(u), resolveSourcePlatform("url", u), resolveSourcePlatform("file", u), resolveSourcePlatform("text", u), resolveSourcePlatform("url", null), detectPlatformCapturePlatform(u)]),
    webVerification: wvTasks.map((tk) => [tk, needsWebVerification(tk as never)]),
  };
}
fs.writeFileSync(path.join(out, "golden.json"), JSON.stringify(golden, null, 2));
console.log("golden ok:", out, `fts=${fts.length} counts=${counts.length} list=${listCases.length} writes=${Object.keys(writes).length}`);
