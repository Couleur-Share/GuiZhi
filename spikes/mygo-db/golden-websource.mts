// WebSourceDB 差分基线：check（重采更新/待采用）与 adopt（采用版本）在多种条目状态下的行为。
// 起始库（setup）由 TS 构造并原样拷贝成 websource-base.db，Go 侧在副本上重放同一操作序列。
import fs from "node:fs";
import path from "node:path";
import { initDatabase, closeDatabase, KnowledgeItemDB, WebSourceDB, webContentHash } from "../../packages/db/src/index.ts";

const sleepMs = (ms: number) => {
  const end = Date.now() + ms;
  while (Date.now() < end);
};

export async function buildWebSourceGolden(out: string): Promise<unknown> {
  const dbPath = path.join(out, "websource-scenario.db");
  const database = initDatabase(dbPath);
  const items = new KnowledgeItemDB(database);
  const web = new WebSourceDB(database);

  const cap = (md: string, title: string, o: Record<string, unknown> = {}) => ({
    $cap: { md, title, ...o },
  });
  const build = (spec: Record<string, unknown>, n: number) => ({
    taskId: "t",
    entryUrl: "https://a.test/p",
    finalUrl: "https://a.test/p",
    title: spec.title,
    author: "",
    publishedAt: null,
    dateConfidence: "none",
    markdown: spec.md,
    links: [],
    paragraphs: [],
    contentHash: "",
    capturedAt: 1_700_000_000_000 + n * 1000,
    engineVersion: (spec.engine as string) ?? "readability/1-static",
    complete: spec.complete ?? true,
    truncated: spec.truncated ?? false,
    warnings: [],
    ...(spec.error ? { error: spec.error } : {}),
  });

  // ── 起始库 ──
  const mk = (title: string, content: string) => items.create({ title, content }).id;
  const ids = [
    mk("标题A", "正文A"), // 0 A：用户未改，可自动更新
    mk("标题B", "正文B"), // 1 B：有摘要
    mk("标题C", "正文C"), // 2 C：初始化后用户又改了正文
    mk("标题D", "正文D"), // 3 D：已删除
    mk("标题E", "正文E"), // 4 E：没有基线
    mk("标题F", "正文F"), // 5 F：首个版本不完整
  ];
  web.initialize(ids[0], build({ md: "正文A", title: "标题A" }, 1) as never);
  web.initialize(ids[1], build({ md: "正文B", title: "标题B" }, 2) as never);
  items.update(ids[1], { summary: "B 的摘要" });
  web.initialize(ids[2], build({ md: "正文C", title: "标题C" }, 3) as never);
  items.update(ids[2], { content: "用户改动C" });
  web.initialize(ids[3], build({ md: "正文D", title: "标题D" }, 4) as never);
  database.run("UPDATE knowledge_items SET deleted_at=? WHERE id=?", 1_700_000_000_500, ids[3]);
  web.initialize(ids[5], build({ md: "正文F", title: "标题F", complete: false }, 5) as never);
  for (const id of [ids[0], ids[1]]) {
    database.run("INSERT INTO knowledge_embeddings VALUES (?,?,?,?,?,?,?,?)", id, 0, "chunk", "h", "m", 2, Buffer.from([1, 2, 3, 4, 5, 6, 7, 8]), 1_700_000_000_000);
    database.run("INSERT INTO wiki_ingestions VALUES (?,?,?,?,?,?,?)", id, "h", "m", "p", 0, null, 1_700_000_000_000);
  }
  closeDatabase();
  fs.copyFileSync(dbPath, path.join(out, "websource-base.db"));
  for (const ext of ["-wal", "-shm"]) {
    if (fs.existsSync(dbPath + ext)) fs.copyFileSync(dbPath + ext, path.join(out, `websource-base.db${ext}`));
  }

  // ── 重放 ──
  const db2 = initDatabase(dbPath);
  const items2 = new KnowledgeItemDB(db2);
  const web2 = new WebSourceDB(db2);
  type Op = { fn: string; args: unknown[] };
  const I = (n: number) => ({ $item: n });
  const V = (item: number, n: number) => ({ $ver: [item, n] });
  const ops: Op[] = [
    { fn: "check", args: [I(0), cap("正文A", "标题A")] }, // unchanged
    { fn: "check", args: [I(0), cap("新正文A", "新标题A")] }, // updated，向量与摄入记录被清
    { fn: "check", args: [I(0), cap("新正文A", "新标题A")] }, // unchanged
    { fn: "check", args: [I(0), cap("新正文A", "新标题A", { engine: "readability/1-render" })] }, // 换路由不算变化
    { fn: "check", args: [I(0), cap("改版正文A", "新标题A", { engine: "crawl4ai/1-static" })] }, // 换提取器 → pending-version
    { fn: "check", args: [I(0), cap("改版正文A", "新标题A", { engine: "crawl4ai/1-static" })] }, // 再次 → pending-version
    { fn: "adopt", args: [{ itemId: I(0), versionId: V(0, 0), expectedContentHash: { $hash: 0 }, expectedTitle: { $title: 0 } }] },
    { fn: "adopt", args: [{ itemId: I(0), versionId: V(0, 0), expectedContentHash: "bad", expectedTitle: { $title: 0 } }] },
    { fn: "adopt", args: [{ itemId: I(0), versionId: V(0, 0), expectedContentHash: { $hash: 0 }, expectedTitle: "bad" }] },
    { fn: "adopt", args: [{ itemId: I(0), versionId: "nope", expectedContentHash: { $hash: 0 }, expectedTitle: { $title: 0 } }] },
    { fn: "adopt", args: [{ itemId: I(5), versionId: V(5, 0), expectedContentHash: { $hash: 5 }, expectedTitle: { $title: 5 } }] }, // 版本不完整
    { fn: "check", args: [I(1), cap("B 新正文", "标题B 新")] }, // updated + 有摘要 → stale
    { fn: "__update", args: [I(1), { title: "我改的标题B" }] },
    { fn: "check", args: [I(1), cap("B 再新正文", "标题B 再新")] }, // 用户改了标题 → 标题保留
    { fn: "check", args: [I(2), cap("远程C新", "标题C")] }, // 用户改过正文 → pending-version
    { fn: "check", args: [I(3), cap("D", "D")] }, // 已删除
    { fn: "check", args: [I(4), cap("e", "E")] }, // 没有基线 → pending-version
    { fn: "check", args: [I(4), cap("e", "E")] }, // 同 hash，基线为空 → pending-version
    { fn: "check", args: [I(0), cap("x", "x", { complete: false })] },
    { fn: "check", args: [I(0), cap("x", "x", { truncated: true })] },
    { fn: "check", args: [I(0), cap("x", "x", { error: { code: "x", message: "m", retryable: false } })] },
    { fn: "check", args: ["missing", cap("x", "x")] },
    { fn: "adopt", args: [{ itemId: I(4), versionId: V(4, 0), expectedContentHash: { $hash: 4 }, expectedTitle: { $title: 4 } }] }, // 无基线 → __legacy_unknown_title__
    { fn: "adopt", args: [{ itemId: I(1), versionId: V(1, 1), expectedContentHash: { $hash: 1 }, expectedTitle: { $title: 1 } }] }, // 采用较旧版本，用户改过标题
    { fn: "adopt", args: [{ itemId: I(3), versionId: V(3, 0), expectedContentHash: { $hash: 3 }, expectedTitle: { $title: 3 } }] }, // 已删除
    { fn: "adopt", args: [{ itemId: "missing", versionId: "x", expectedContentHash: "h", expectedTitle: "t" }] },
  ];
  const versionsOf = (id: string) => web2.versions(id);
  const resolve = (v: unknown, n: number): unknown => {
    if (Array.isArray(v)) return v.map((x) => resolve(x, n));
    if (v && typeof v === "object") {
      const o = v as Record<string, unknown>;
      if ("$item" in o) return ids[o.$item as number];
      if ("$ver" in o) {
        const [i, k] = o.$ver as [number, number];
        return versionsOf(ids[i])[k]?.id ?? "missing-version";
      }
      if ("$hash" in o) return webContentHash(items2.get(ids[o.$hash as number])!.content);
      if ("$title" in o) return items2.get(ids[o.$title as number])!.title;
      if ("$cap" in o) return build(o.$cap as Record<string, unknown>, 100 + n);
      return Object.fromEntries(Object.entries(o).map(([k, x]) => [k, resolve(x, n)]));
    }
    return v;
  };
  const state = () =>
    ids.map((id) => {
      const it = db2.get("SELECT title, content, summary, deleted_at FROM knowledge_items WHERE id=?", id) as Record<string, unknown>;
      const b = db2.get("SELECT version_id, content_hash, title, checked_at, remote_hash, summary_stale FROM web_source_baselines WHERE item_id=?", id) as Record<string, unknown> | undefined;
      return {
        item: Object.values(it),
        baseline: b ? Object.values(b) : null,
        versions: versionsOf(id),
        embeddings: (db2.get("SELECT COUNT(*) AS n FROM knowledge_embeddings WHERE item_id=?", id) as { n: number }).n,
        wiki: (db2.get("SELECT COUNT(*) AS n FROM wiki_ingestions WHERE item_id=?", id) as { n: number }).n,
      };
    });
  const results = ops.map((op, n) => {
    sleepMs(3);
    const args = resolve(op.args, n) as unknown[];
    let r: Record<string, unknown>;
    try {
      let value: unknown;
      if (op.fn === "__update") value = items2.update(args[0] as string, args[1] as never) && null;
      else value = (web2 as unknown as Record<string, (...a: unknown[]) => unknown>)[op.fn](...args);
      r = { op, value: value === undefined ? null : value };
    } catch (e) {
      r = { op, error: (e as Error).message };
    }
    return { ...r, state: state() };
  });
  closeDatabase();
  return { ids, ops: results };
}
