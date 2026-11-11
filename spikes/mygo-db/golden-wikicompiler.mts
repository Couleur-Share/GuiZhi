// wiki:compiler 差分基线：真实的 wiki-compiler.ipc.ts（含 WikiCompilerDB / wiki-publish）。
//  - blocks：wikiBlocks / wikiFingerprint 纯函数在各类文本上的输出（偏移为 UTF-16 码元）；
//  - scenarios：在同一份种子库上重放的步骤脚本，记录每步响应与关键表的转储，Go 侧逐步比对。
// 随机/时间量的处理：任务 id 在响应与转储里替换成 <jobN>；页面用 normalized_title 代替 uuid；
// 时间戳列不转储（只留「是否为空」）。种子页面与建议使用固定时间戳，建议令牌因此可复现。
import fs from "node:fs";
import path from "node:path";
import { initDatabase, closeDatabase, KnowledgeItemDB } from "../../packages/db/src/index.ts";
import { wikiBlocks, wikiFingerprint } from "../../packages/db/src/wiki-material.ts";
import { segmentTextForFts } from "../../packages/db/src/fts.ts";

type Json = Record<string, unknown>;
type Handler = (event: unknown, input: unknown) => Promise<unknown>;

export interface WikiCompilerStep {
  op: "call" | "sql" | "register";
  input?: unknown;
  sql?: string;
  args?: unknown[];
  response?: unknown;
  dump?: unknown;
}

const FIX = 1_700_000_000_000;

function mat(title: string, content: string, transcript: string | null) {
  return { id: "x", title, content, transcript, review_status: "clear", deleted_at: null };
}

/** 一批覆盖围栏、标题、换行、长段与超长单行的文本。 */
function blockInputs(): Array<{ name: string; title: string; content: string; transcript: string | null }> {
  const long = (s: string, n: number) => s.repeat(n);
  const cases: Array<[string, string, string, string | null]> = [
    ["empty", "标题", "", null],
    ["titleOnlyShort", "只有标题", "", null],
    ["titleOnlyLong", long("长标题", 2500), "", null],
    ["blankTitleBlankBody", "   ", "", null],
    ["plain", "纯文本", "第一段\n第一段第二行\n\n第二段\n\n\n\n第三段", null],
    ["leadingBlank", "t", "\n\n\n段落一\n\n段落二\n", null],
    ["crlf", "t", "# 标题 A\r\n正文一\r\n\r\n## 标题 B\r\n正文二", null],
    ["loneCr", "t", "# 标题\r正文\r\r另一段", null],
    ["spacesLine", "t", "第一行\n   \n第二行\n\n\t\n第三行", null],
    ["headers", "t", "# 一级\n内容\n\n## 二级 标题\n内容\n\n###### 六级\n内容\n\n####### 七级不算\n内容\n\n#无空格\n内容", null],
    ["headerCrossLine", "t", "#\n跨行标题\n后续", null],
    ["headerTrailingSpaces", "t", "#  \n后文", null],
    ["headerOnlySpaces", "t", "# ", null],
    ["headerNbsp", "t", "#\u00a0不换行空格标题\n内容", null],
    ["headerInBody", "t", "前文\n## 嵌入标题\n后文", null],
    ["fenceBasic", "t", "前文\n\n```js\nconst a = 1;\n# 不是标题\n```\n\n后文\n\n# 真标题\n文字", null],
    ["fenceTilde", "t", "~~~\ncode\n~~~\n\n## 之后", null],
    ["fenceLongerClose", "t", "````\n```\n里面的三反引号\n```\n````\n\n后文", null],
    ["fenceInfoClose", "t", "```py\ncode\n``` extra\nstill code\n```\n\nafter", null],
    ["fenceIndent4", "t", "    ```\n四空格缩进不是围栏\n    ```\n\n# 标题", null],
    ["fenceIndent3", "t", "   ```\n三空格缩进是围栏\n   ```\n\n# 标题", null],
    ["fenceMixed", "t", "```~~~ title\ncode\n```", null],
    ["fenceUnclosedLong", "t", "```text\n" + long("这是一行代码内容 line\n", 400) + "```\n\n结尾", null],
    ["fenceCrossBlocks", "t", "```\n" + long("x".repeat(80) + "\n", 120) + "\n\n" + long("y".repeat(80) + "\n", 120) + "```\n\n# 尾", null],
    ["longLine", "t", long("长", 7000), null],
    ["longLineEmoji", "t", "a".repeat(100) + "😀" + "b".repeat(5000), null],
    ["longLines", "t", long("一行比较长的文字，用来测试按完整行边界切分。\n", 200), null],
    ["longWithHeader", "t", "# 起始标题\n" + long("内容行内容行内容行\n", 300) + "## 中途标题\n" + long("更多内容\n", 400), null],
    ["duplicates", "t", "重复段落\n\n重复段落\n\n重复段落\n\n其他", null],
    ["transcript", "视频", "# 描述\n简介", "说话人 1：你好\n说话人 2：你好啊\n\n继续的文字稿"],
    ["transcriptOnly", "视频", "", "只有文字稿"],
    ["emojiParagraph", "t", "😀 表情 🎉\n\n第二段 👍🏽 家族", null],
    ["unicodeSep", "t", "行一\u2028行二\n\n段\u2029落", null],
  ];
  return cases.map(([name, title, content, transcript]) => ({ name, title, content, transcript }));
}

/** 把响应与转储里的任务 id 换成稳定标记；去掉时间戳。 */
function makeNormalizer() {
  const jobs = new Map<string, string>();
  // 键排序后再编号：编号只取决于值在规范文本里的出现顺序，与两边对象的键序无关
  const canon = (v: unknown): unknown => Array.isArray(v) ? v.map(canon)
    : v && typeof v === "object" ? Object.fromEntries(Object.keys(v as Json).sort().map((k) => [k, canon((v as Json)[k])])) : v;
  const norm = (v: unknown): unknown => {
    const text = JSON.stringify(canon(v ?? null));
    let out = text.replace(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/g, (m) => {
      if (!jobs.has(m)) jobs.set(m, `<uuid${jobs.size + 1}>`);
      return jobs.get(m)!;
    });
    out = out.replace(/"createdAt":\d+,?/g, "").replace(/,}/g, "}");
    return JSON.parse(out);
  };
  return { norm, jobs };
}

const DUMPS: Record<string, string> = {
  jobs: "SELECT model,status,error FROM wiki_compile_jobs ORDER BY created_at,rowid",
  items: "SELECT title,fingerprint,published FROM wiki_compile_items ORDER BY rowid",
  blocks: "SELECT item_id,block_key,block_json,status,contributions_json,error,attempts FROM wiki_compile_blocks ORDER BY rowid",
  cache: "SELECT item_id,block_hash,model,contributions_json FROM wiki_block_cache ORDER BY item_id,block_hash,model",
  contributions: "SELECT item_id,block_key,page_title,contribution_json FROM wiki_contributions ORDER BY item_id,block_key,page_title",
  ingestions: "SELECT item_id,content_hash,model,prompt_version,failure_count,CASE WHEN next_attempt_at IS NULL THEN NULL WHEN next_attempt_at > CAST(strftime('%s','now') AS INTEGER)*1000 THEN 'future' ELSE 'past' END AS next FROM wiki_ingestions ORDER BY item_id",
  pages: "SELECT normalized_title,title,kind,summary,body,aliases_json,provider,model,prompt_version,manual_edited_at IS NOT NULL AS manual FROM wiki_pages ORDER BY normalized_title",
  sources: "SELECT p.normalized_title AS page,s.item_id FROM wiki_page_sources s JOIN wiki_pages p ON p.id=s.page_id ORDER BY p.normalized_title,s.item_id",
  links: "SELECT f.normalized_title AS a,t.normalized_title AS b FROM wiki_page_links l JOIN wiki_pages f ON f.id=l.from_page_id JOIN wiki_pages t ON t.id=l.to_page_id ORDER BY a,b",
  revisions: "SELECT p.normalized_title AS page,r.title,r.summary,r.body,r.model FROM wiki_page_revisions r JOIN wiki_pages p ON p.id=r.page_id ORDER BY p.normalized_title,r.rowid",
  suggestions: "SELECT p.normalized_title AS page,s.draft_json,s.reason FROM wiki_page_suggestions s JOIN wiki_pages p ON p.id=s.page_id ORDER BY p.normalized_title",
  fts: "SELECT title,summary,body FROM wiki_fts ORDER BY title,body",
  invalidated: "SELECT page_title FROM wiki_invalidated_pages ORDER BY page_title",
};

function dumpAll(db: { all(sql: string): unknown[] }): Json {
  const out: Json = {};
  for (const [k, sql] of Object.entries(DUMPS)) out[k] = db.all(sql);
  return out;
}

export async function buildWikiCompilerGolden(out: string, ipcHandlers: Map<string, Handler>) {
  fs.mkdirSync(out, { recursive: true });
  // ── 1. 纯函数 ──
  const blocks = blockInputs().map((c) => {
    const m = mat(c.title, c.content, c.transcript);
    return { ...c, fingerprint: wikiFingerprint(m), blocks: wikiBlocks(m) };
  });

  // ── 2. 种子库 ──
  const base = path.join(out, "wikicompiler-base.db");
  fs.rmSync(base, { force: true });
  const seedDb = initDatabase(base);
  const items = new KnowledgeItemDB(seedDb);
  const mk = (title: string, content: string, transcript?: string) => items.create({ title, content, transcript, tagNames: [] } as never) as { id: string; title: string; content: string };
  const A = mk("归知笔记", "# 概览\n归知是本地优先的知识库。\n\n## 检索\n全文检索与语义检索。\n\n结尾段落。");
  const B = mk("待复核", "需要复核的内容");
  const C = mk("只有标题的资料", "");
  const D = mk("已删除", "删除前的内容");
  const E = mk("已是最新", "最新内容");
  const F = mk("旧版本覆盖", "旧版本内容");
  const G = mk("失败三次", "多次失败的内容");
  const H = mk("退避中", "等待重试的内容");
  const I = mk("退避已过", "可以重试的内容");
  const J = mk("   ", "");
  const K = mk("视频", "# 描述\n简介", "说话人 1：你好\n\n说话人 2：再见");
  const L = mk("暂停任务中的资料", "暂停任务里的内容");
  const M = mk("两块资料", "第一块内容\n\n第二块内容");
  const N = mk("旧页面来源", "旧页面的来源内容");
  seedDb.run("UPDATE knowledge_items SET review_status='needs_review' WHERE id=?", [B.id]);
  seedDb.run("UPDATE knowledge_items SET deleted_at=? WHERE id=?", [FIX, D.id]);
  const fpOf = (it: { id: string }) => {
    const row = seedDb.get("SELECT id,title,content,transcript,review_status,deleted_at FROM knowledge_items WHERE id=?", [it.id]) as never;
    return wikiFingerprint(row);
  };
  const ing = (it: { id: string }, hash: string, model: string, prompt: string, failures: number, next: number | null) =>
    seedDb.run("INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,failure_count,next_attempt_at,updated_at) VALUES(?,?,?,?,?,?,?)", [it.id, hash, model, prompt, failures, next, FIX]);
  ing(E, fpOf(E), "model-a", "fulltext-v2", 0, null);
  ing(F, fpOf(F), "model-a", "old-v1", 0, null);
  ing(G, fpOf(G), "model-a", "", 3, null);
  ing(H, fpOf(H), "model-a", "", 1, 4_000_000_000_000);
  ing(I, fpOf(I), "model-a", "", 1, FIX);
  // A 的第一块有缓存（model-a）
  const aBlocks = wikiBlocks(seedDb.get("SELECT id,title,content,transcript,review_status,deleted_at FROM knowledge_items WHERE id=?", [A.id]) as never);
  seedDb.run("INSERT INTO wiki_block_cache VALUES(?,?,?,?)", [A.id, aBlocks[0].hash, "model-a", JSON.stringify([{ title: "缓存页", normalizedTitle: "缓存页", kind: "topic", summary: "缓存摘要", aliasesJson: null, body: "缓存正文", linkTargets: [] }])]);
  // 暂停任务：L 未发布 → 预览里是 waiting
  const pausedJob = "11111111-1111-4111-8111-111111111111";
  seedDb.run("INSERT INTO wiki_compile_jobs VALUES(?,?,?,?,?)", [pausedJob, "model-a", "paused", null, FIX]);
  seedDb.run("INSERT INTO wiki_compile_items(job_id,item_id,title,fingerprint,published) VALUES(?,?,?,?,0)", [pausedJob, L.id, L.title, fpOf(L)]);
  // 已有页面：普通页（可被覆盖）、人工编辑页（只出建议）、旧版页（旧来源→建议）
  const page = (id: string, title: string, kind: string, summary: string, body: string, prompt: string, manual: number | null, aliases: string | null = null) => {
    seedDb.run("INSERT INTO wiki_pages(id,title,normalized_title,kind,summary,body,aliases_json,provider,model,prompt_version,generated_at,manual_edited_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
      [id, title, title.toLowerCase(), kind, summary, body, aliases, "guizhi", "model-a", prompt, FIX, manual, FIX, FIX]);
    seedDb.run("INSERT INTO wiki_fts(page_id,title,summary,body) VALUES(?,?,?,?)", [id, segmentTextForFts(title), segmentTextForFts(summary), segmentTextForFts(body)]);
  };
  page("page-plain", "普通页", "topic", "旧摘要", "旧正文", "fulltext-v2", null);
  page("page-manual", "人工页", "topic", "人工摘要", "人工正文", "fulltext-v2", FIX);
  page("page-legacy", "旧版页", "topic", "旧版摘要", "旧版正文", "old-v1", null);
  page("page-target", "链接目标", "entity", "目标摘要", "目标正文", "fulltext-v2", null);
  seedDb.run("INSERT INTO wiki_page_sources VALUES(?,?,?)", ["page-plain", A.id, FIX]);
  seedDb.run("INSERT INTO wiki_page_sources VALUES(?,?,?)", ["page-legacy", N.id, FIX]);
  ing(N, fpOf(N), "model-a", "old-v1", 0, null);
  // 建议：直接在种子里给两页造建议（时间固定，令牌可复现）
  const draft = (title: string, body: string, links: string[]) => ({ title, normalizedTitle: title.toLowerCase(), kind: "topic", summary: "建议摘要", aliasesJson: '["别名"]', body, linkTargets: links });
  seedDb.run("INSERT INTO wiki_page_suggestions VALUES(?,?,?,?)", ["page-manual", JSON.stringify({ draft: draft("人工页", "建议正文", ["链接目标", "不存在", "人工页"]), model: "model-b", sourceIds: [A.id, D.id, "missing-item"] }), "人工编辑内容保持不变，需审阅更新建议", FIX]);
  seedDb.run("INSERT INTO wiki_page_suggestions VALUES(?,?,?,?)", ["page-legacy", JSON.stringify({ draft: draft("旧版页", "新版建议正文", []), model: "model-b", sourceIds: [] }), "旧页面仍有未升级来源，保留原内容，需审阅更新建议", FIX]);
  // 失效重建：一个页面有贡献且被标记失效（会在读取建议时 flush）
  seedDb.run("INSERT INTO wiki_contributions VALUES(?,?,?,?)", [E.id, "b1", "普通页", JSON.stringify(draft("普通页", "来自贡献的新正文", ["链接目标"]))]);
  seedDb.run("INSERT INTO wiki_invalidated_pages VALUES(?)", ["普通页"]);
  closeDatabase();

  const ids = { A: A.id, B: B.id, C: C.id, D: D.id, E: E.id, F: F.id, G: G.id, H: H.id, I: I.id, J: J.id, K: K.id, L: L.id, M: M.id, N: N.id };
  const wikiIpc = await import("../../apps/desktop/src/main/ipc/wiki-compiler.ipc.ts");
  const fingerprints: Record<string, string> = {};
  {
    fs.copyFileSync(base, path.join(out, "wikicompiler-fp.db"));
    const db = initDatabase(path.join(out, "wikicompiler-fp.db"));
    for (const [k, id] of Object.entries(ids)) {
      const row = db.get("SELECT id,title,content,transcript,review_status,deleted_at FROM knowledge_items WHERE id=?", [id]) as never;
      fingerprints[k] = wikiFingerprint(row);
    }
    closeDatabase();
  }
  const sel = (...keys: string[]) => keys.map((k) => ({ id: ids[k as keyof typeof ids], fingerprint: fingerprints[k] }));
  const call = (input: unknown): WikiCompilerStep => ({ op: "call", input });
  const sql = (s: string, ...args: unknown[]): WikiCompilerStep => ({ op: "sql", sql: s, args });
  const ref = (name: string, extra: Json = {}) => ({ $ref: name, ...extra });
  const contrib = (title: string, over: Json = {}) => ({ title, normalizedTitle: title.toLowerCase(), kind: "topic", summary: `${title}摘要`, aliasesJson: null, body: `${title}正文`, linkTargets: [], ...over });
  const finishOk = (...pages: unknown[]) => call({ action: "finish", work: ref("work"), contributions: pages });
  const drain = (n: number, contributions: (i: number) => unknown[]) => {
    const steps: WikiCompilerStep[] = [];
    for (let i = 0; i < n; i++) steps.push(call({ action: "next", id: ref("job") }), finishOk(...contributions(i)));
    return steps;
  };

  const scenarios: Record<string, WikiCompilerStep[]> = {
    // 预览：各状态、模型不同、无模型、重复与未知 id、空 ids
    previewAll: [call({ action: "preview", model: "model-a" }), call({ action: "preview", model: "model-b" }), call({ action: "preview", model: "" }),
      call({ action: "preview" }), call({ action: "preview", model: null }), call({ action: "preview", model: 5 }), call({ action: "preview", model: "model-a", ids: [ids.A] }),
    ],
    previewIds: [
      call({ action: "preview", model: "model-a", ids: [ids.A, ids.A, "unknown", ids.E, ids.F] }),
      call({ action: "preview", model: "model-a", ids: [] }),
      call({ action: "preview", model: "model-a", ids: "oops" }),
      call({ action: "preview", model: "model-a", ids: "" }),
      call({ action: "preview", model: "model-a", ids: 5 }),
      call({ action: "preview", model: "model-a", ids: { length: 2 } }),
      call({ action: "preview", model: "model-a", ids: {} }),
    ],
    startInvalid: [
      call({ action: "start", model: "", selected: sel("A") }),
      call({ action: "start", model: "model-a", selected: [] }),
      call({ action: "start", model: "model-a", selected: "x" }),
      call({ action: "start", model: "model-a", selected: "" }),
      call({ action: "start", model: "model-a", selected: { length: 3 } }),
      call({ action: "start", model: "model-a", selected: { id: ids.A } }),
      call({ action: "start", model: "model-a", selected: [null] }),
      call({ action: "start", model: "model-a", selected: [{ id: ids.A, fingerprint: "stale" }] }),
      call({ action: "start", model: "model-a", selected: sel("B") }),
      call({ action: "start", model: "model-a", selected: sel("E") }),
      call({ action: "start", model: "model-a", selected: sel("F") }),
      call({ action: "start", model: "model-a", selected: [...sel("A"), { id: 7 }] }),
    ],
    startUpgrade: [call({ action: "start", model: "model-a", selected: sel("F"), allowUpgrade: true }), call({ action: "start", model: "model-a", selected: sel("A") }), call({ action: "jobs" })],
    flowBasic: [
      call({ action: "start", model: "model-a", selected: sel("A", "M") }),
      call({ action: "start", model: "model-a", selected: sel("C") }),
      ...drain(4, (i) => [contrib(`页面${i % 2}`, { linkTargets: ["页面0", "页面1", "链接目标"] }), contrib("共享页", { body: `共享正文${i}` })]),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
      call({ action: "preview", model: "model-a" }),
    ],
    flowCacheReuse: [
      call({ action: "start", model: "model-a", selected: sel("A") }),
      call({ action: "next", id: ref("job") }),
      finishOk(contrib("A块1")),
      call({ action: "next", id: ref("job") }),
      finishOk(),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
    ],
    flowEmptyContributions: [
      call({ action: "start", model: "model-a", selected: sel("C") }),
      call({ action: "next", id: ref("job") }),
      call({ action: "finish", work: ref("work") }),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
    ],
    flowTitleOnlyTwoPages: [
      call({ action: "start", model: "model-a", selected: sel("K") }),
      ...drain(2, (i) => [contrib("视频主题"), contrib(`说话人${i}`, { kind: "entity" })]),
      call({ action: "next", id: ref("job") }),
    ],
    flowOverwritePlain: [
      call({ action: "start", model: "model-a", selected: sel("A") }),
      ...drain(3, (i) => [contrib("普通页", { body: `覆盖正文${i}`, linkTargets: ["链接目标", "普通页", "新页"] }), contrib("新页", { linkTargets: ["普通页"] })]),
      call({ action: "next", id: ref("job") }),
    ],
    flowManualAndLegacy: [
      call({ action: "start", model: "model-a", selected: sel("A", "N"), allowUpgrade: true }),
      ...drain(4, (i) => [contrib("人工页", { body: `新人工正文${i}` }), contrib("旧版页", { body: `新旧版正文${i}` })]),
      call({ action: "next", id: ref("job") }),
      call({ action: "suggestion", pageId: "page-manual" }),
    ],
    failureRetry: [
      call({ action: "start", model: "model-a", selected: sel("M") }),
      call({ action: "next", id: ref("job") }),
      call({ action: "finish", work: ref("work"), error: "模型超时" }),
      call({ action: "next", id: ref("job") }),
      finishOk(contrib("失败后页")),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
      call({ action: "control", id: ref("job"), status: "running" }),
      call({ action: "next", id: ref("job") }),
      finishOk(contrib("重试页")),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
    ],
    failureAllBlocks: [
      call({ action: "start", model: "model-a", selected: sel("M") }),
      call({ action: "next", id: ref("job") }),
      call({ action: "finish", work: ref("work"), error: "失败一" }),
      call({ action: "next", id: ref("job") }),
      call({ action: "finish", work: ref("work"), error: "失败二" }),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
      call({ action: "preview", model: "model-a", ids: [ids.M] }),
    ],
    invalidContributions: [
      call({ action: "start", model: "model-a", selected: sel("M") }),
      call({ action: "next", id: ref("job") }),
      call({ action: "finish", work: ref("work"), contributions: [contrib("a"), contrib("b"), contrib("c"), contrib("d"), contrib("e")] }),
      call({ action: "finish", work: ref("work"), contributions: [contrib("a", { kind: "bogus" })] }),
      call({ action: "finish", work: ref("work"), contributions: [contrib("a", { body: "长".repeat(16001) })] }),
      call({ action: "finish", work: ref("work"), contributions: [contrib("a", { title: "" })] }),
      call({ action: "finish", work: ref("work"), contributions: [contrib("a", { body: "" })] }),
      call({ action: "finish", work: ref("work"), contributions: [null] }),
      call({ action: "finish", work: ref("work"), contributions: [5] }),
      call({ action: "finish", work: ref("work"), contributions: "abcdef" }),
      call({ action: "finish", work: ref("work"), contributions: "abc" }),
      call({ action: "finish", work: ref("work"), contributions: { length: 9 } }),
      call({ action: "finish", work: ref("work"), contributions: { x: 1 } }),
      call({ action: "next", id: ref("job") }),
      call({ action: "finish", work: ref("work"), contributions: [contrib("ok", { body: "长".repeat(16000) })] }),
      call({ action: "finish", work: ref("work"), contributions: [contrib("ok")] }),
      call({ action: "jobs" }),
    ],
    badReceipts: [
      call({ action: "finish", work: null }),
      call({ action: "finish", work: { jobId: "none" } }),
      call({ action: "start", model: "model-a", selected: sel("M") }),
      call({ action: "finish", work: ref("work") }),
      call({ action: "next", id: ref("job") }),
      call({ action: "finish", work: ref("work", { setItemId: "other" }) }),
      call({ action: "finish", work: ref("work", { setBlockKey: "zzz" }) }),
      call({ action: "finish", work: { jobId: ref("job") } }),
      call({ action: "finish", work: { jobId: ref("job"), block: {} } }),
      finishOk(contrib("p")),
      finishOk(contrib("p")),
      call({ action: "next", id: "unknown" }),
      call({ action: "next" }),
    ],
    control: [
      call({ action: "control", id: "none", status: "paused" }),
      call({ action: "start", model: "model-a", selected: sel("M") }),
      call({ action: "start", model: "model-a", selected: sel("A") }),
      call({ action: "control", id: ref("job"), status: "paused" }),
      call({ action: "next", id: ref("job") }),
      call({ action: "start", model: "model-a", selected: sel("A") }),
      call({ action: "control", id: ref("job"), status: "running" }),
      call({ action: "next", id: ref("job") }),
      call({ action: "control", id: ref("job"), status: "paused" }),
      call({ action: "control", id: ref("job"), status: "running" }),
      call({ action: "control", id: ref("job"), status: "cancelled" }),
      call({ action: "finish", work: ref("work"), contributions: [] }),
      call({ action: "jobs" }),
      call({ action: "preview", model: "model-a", ids: [ids.M] }),
    ],
    controlCompleted: [
      call({ action: "start", model: "model-a", selected: sel("C") }),
      ...drain(1, () => [contrib("p")]),
      call({ action: "next", id: ref("job") }),
      call({ action: "control", id: ref("job"), status: "paused" }),
    ],
    sourceChanged: [
      call({ action: "start", model: "model-a", selected: sel("M") }),
      sql("UPDATE knowledge_items SET content=? WHERE id=?", "被改动的内容", ids.M),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
    ],
    sourceChangedBeforePublish: [
      call({ action: "start", model: "model-a", selected: sel("C") }),
      call({ action: "next", id: ref("job") }),
      finishOk(contrib("p")),
      sql("UPDATE knowledge_items SET title=? WHERE id=?", "改了标题", ids.C),
      call({ action: "next", id: ref("job") }),
      call({ action: "jobs" }),
    ],
    sourceDeletedMidway: [
      call({ action: "start", model: "model-a", selected: sel("M") }),
      sql("DELETE FROM knowledge_items WHERE id=?", ids.M),
      call({ action: "jobs" }),
      call({ action: "next", id: ref("job") }),
    ],
    interrupt: [
      call({ action: "start", model: "model-a", selected: sel("M") }),
      call({ action: "next", id: ref("job") }),
      { op: "register" },
      call({ action: "jobs" }),
      call({ action: "control", id: ref("job"), status: "running" }),
      call({ action: "next", id: ref("job") }),
    ],
    suggestionView: [
      call({ action: "suggestion", pageId: "page-manual" }),
      call({ action: "suggestion", pageId: "page-legacy" }),
      call({ action: "suggestion", pageId: "page-plain" }),
      call({ action: "suggestion", pageId: "missing" }),
    ],
    suggestionAccept: [
      call({ action: "suggestion", pageId: "page-manual", accept: true }),
      call({ action: "suggestion", pageId: "page-manual", accept: true, token: "bad-token" }),
      call({ action: "suggestion", pageId: "missing", accept: true, token: "t" }),
      call({ action: "suggestion", pageId: "page-manual", accept: true, token: ref("suggestionToken", { page: "page-manual" }) }),
      call({ action: "suggestion", pageId: "page-manual" }),
      call({ action: "suggestion", pageId: "page-legacy", accept: true, token: ref("suggestionToken", { page: "page-legacy" }) }),
    ],
    unknownAction: [call({ action: "nope" }), call({}), call(null), call({ action: "preview", model: "model-a", ids: [ids.A] })],
  };

  // ── 4. 重放（真实 IPC）──
  const results: Record<string, WikiCompilerStep[]> = {};
  for (const [name, steps] of Object.entries(scenarios)) {
    const file = path.join(out, `wikicompiler-${name}.db`);
    fs.copyFileSync(base, file);
    let db = initDatabase(file);
    wikiIpc.registerWikiCompilerIPC(db as never);
    const normalizer = makeNormalizer();
    let job = "", work: unknown = null;
    const resolve = (v: unknown): unknown => {
      if (Array.isArray(v)) return v.map(resolve);
      if (v && typeof v === "object") {
        const o = v as Json;
        if (typeof o.$ref === "string") {
          if (o.$ref === "job") return job;
          if (o.$ref === "work") {
            const w = structuredClone(work) as Json | null;
            if (w && o.setItemId) w.itemId = o.setItemId;
            if (w && o.setBlockKey) (w.block as Json).key = o.setBlockKey;
            return w;
          }
          if (o.$ref === "suggestionToken") {
            const rows = db.all("SELECT draft_json FROM wiki_page_suggestions WHERE page_id=?", [o.page]) as Array<{ draft_json: string }>;
            return rows.length ? (resolveToken as (id: string, d: string) => string)(String(o.page), rows[0].draft_json) : "";
          }
        }
        return Object.fromEntries(Object.entries(o).map(([k, x]) => [k, resolve(x)]));
      }
      return v;
    };
    const publish = await import("../../packages/db/src/wiki-publish.ts");
    const resolveToken = (pageId: string, draftJson: string) => publish.wikiSuggestionToken(db as never, pageId, draftJson);
    const recorded: WikiCompilerStep[] = [];
    for (const step of steps) {
      if (step.op === "sql") {
        db.run(step.sql!, step.args ?? []);
        recorded.push({ ...step, dump: normalizer.norm(dumpAll(db as never)) });
        continue;
      }
      if (step.op === "register") {
        wikiIpc.registerWikiCompilerIPC(db as never);
        recorded.push({ ...step, dump: normalizer.norm(dumpAll(db as never)) });
        continue;
      }
      const input = resolve(step.input);
      const handler = ipcHandlers.get("wiki:compiler");
      if (!handler) throw new Error("wiki:compiler 未注册");
      const response = (await handler({}, input)) as Json;
      const job0 = (response as { job?: { id?: string } })?.job;
      if (job0?.id && (input as Json | null)?.action === "start") job = job0.id;
      if ((input as Json | null)?.action === "next" && (response as { work?: unknown }).work) work = (response as { work: unknown }).work;
      recorded.push({ ...step, input: normalizer.norm(input), response: normalizer.norm(response), dump: normalizer.norm(dumpAll(db as never)) });
    }
    closeDatabase();
    void db;
    db = undefined as never;
    results[name] = steps.map((s, i) => ({ ...s, response: recorded[i].response, dump: recorded[i].dump }));
  }
  void sel;
  return { blocks, ids, fingerprints, dumpSql: DUMPS, scenarios: results };
}
