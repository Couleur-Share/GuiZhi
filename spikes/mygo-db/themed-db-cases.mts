// 主题阅读页数据层差分夹具：真实 packages/db 的 ThemedReadingDB 在临时库上按剧本逐步执行，
// 每步记录返回值（或错误文案）与三张表的完整快照；Go 端 themed_db_test.go 用同一剧本回放并对拍。
// 时间戳（Date.now()）统一抹成 "NOW"，SQLite 原始约束错误只比对「是约束错误」。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

/* eslint-disable @typescript-eslint/no-explicit-any */
export type ThemedDbOp = { op: string; [k: string]: unknown };

const hex = (c: string) => c.repeat(64);
export function ver(id: string, item: string, kind: string, role: string, extra: Record<string, unknown> = {}): any {
  return {
    id, itemId: item, sourceKind: kind, role, formatVersion: 1,
    source: { title: "标题", content: "正文", sourceUri: null, fingerprint: hex("a"), blocks: [{ id: "b0", markdown: "正文", html: "<p>正文</p>", text: "正文" }] },
    options: { style: "s", generateImages: false, maxImages: 0 }, design: null, assets: [], warnings: [], createdAt: 10, updatedAt: 10, ...extra,
  };
}
const design = (assets: any[] = []) => ({ direction: "d", html: "<p>x</p>", css: "", assets });
export function task(id: string, item: string, kind: string, versionId: string, state: string, extra: Record<string, unknown> = {}): any {
  return { id, itemId: item, sourceKind: kind, versionId, title: "标题", state, stage: "prepare", completed: 0, total: 0, createdAt: 20, updatedAt: 20, ...extra };
}
const asset = (id: string, file: string | undefined, sha: string | undefined, bytes: number | undefined, status = "ready") =>
  ({ id, role: "original", purpose: "p", prompt: "", alt: "a", aspectRatio: "4:3", status, ...(file ? { fileName: file } : {}), ...(sha ? { sha256: sha } : {}), ...(bytes !== undefined ? { bytes } : {}) });

export const themedDbItems = [
  { id: "n1", deletedAt: null }, { id: "n2", deletedAt: 1700000000000 }, { id: "n3", deletedAt: null },
];

export function themedDbScript(): { name: string; op: ThemedDbOp }[] {
  const s: { name: string; op: ThemedDbOp }[] = [];
  const add = (name: string, op: ThemedDbOp) => s.push({ name, op });
  const A = (v: any) => ({ op: "saveVersion", version: v });
  add("working-1", A(ver("w1", "n1", "body", "working", { updatedAt: 11 })));
  add("working-1-update", A(ver("w1", "n1", "body", "working", { updatedAt: 12, warnings: ["x"] })));
  add("working-replace-other-id", A(ver("w1b", "n1", "body", "working", { updatedAt: 13 })));
  add("get-working", { op: "get", item: "n1", kind: "body", role: "working" });
  add("get-default-role-none", { op: "get", item: "n1", kind: "body" });
  add("getVersion-missing", { op: "getVersion", id: "nope" });
  add("deleted-item", A(ver("wd", "n2", "body", "working")));
  add("missing-item", A(ver("wm", "zz", "body", "working")));
  add("bad-shape", A({ id: "x" }));
  add("invalid-source", A(ver("wb", "n1", "summary", "working", { source: { title: "t" } })));
  add("change-owner", A(ver("w1b", "n3", "body", "working")));
  add("change-role", A(ver("w1b", "n1", "body", "current")));
  add("assets-ok", A(ver("wa", "n1", "summary", "working", { assets: [asset("o1", "a.png", hex("b"), 5), asset("o2", "a.png", hex("b"), 5), asset("o3", "b.png", undefined, undefined, "pending"), asset("o4", undefined, undefined, undefined, "pending")] })));
  add("assets-mismatch", A(ver("wa", "n1", "summary", "working", { assets: [asset("o1", "a.png", hex("b"), 5), asset("o2", "a.png", hex("c"), 5)] })));
  add("assets-pending-then-ready", A(ver("wa", "n1", "summary", "working", { assets: [asset("o1", "a.png", undefined, undefined, "pending"), asset("o2", "a.png", hex("d"), 7)] })));
  add("current-direct", A(ver("c1", "n3", "body", "current", { design: design() })));
  add("current-replace-denied", A(ver("c2", "n3", "body", "current", { design: design() })));
  add("task-queued", { op: "saveTask", task: task("t1", "n1", "body", "w1b", "queued") });
  add("task-owner-change", { op: "saveTask", task: task("t1", "n3", "body", "w1b", "queued") });
  add("task-version-change", { op: "saveTask", task: task("t1", "n1", "body", "wa", "queued") });
  add("task-bad", { op: "saveTask", task: task("t9", "n1", "body", "w1b", "nope") });
  add("task-second-active", { op: "saveTask", task: task("t2", "n1", "body", "w1b", "running") });
  add("working-replace-while-active", A(ver("w1c", "n1", "body", "working")));
  add("save-with-task-mismatch", { op: "saveVersion", version: ver("w1b", "n1", "body", "working"), task: task("t1", "n1", "summary", "w1b", "running") });
  add("save-with-task", { op: "saveVersion", version: ver("w1b", "n1", "body", "working", { updatedAt: 14 }), task: task("t1", "n1", "body", "w1b", "running", { updatedAt: 21, usage: { textCalls: 1, imageCalls: 0, imagesSaved: 0 } }) });
  add("listTasks", { op: "listTasks" });
  add("getTask", { op: "getTask", id: "t1" });
  add("getTask-missing", { op: "getTask", id: "zz" });
  add("hasActive-yes", { op: "hasActiveTasks" });
  add("remove-active", { op: "remove", item: "n1", kind: "body" });
  add("restore-active", { op: "restorePrevious", item: "n1", kind: "body" });
  add("publish-no-design", { op: "publish", id: "w1b" });
  add("add-design-version", A(ver("w2", "n3", "body", "working", { design: design([asset("g1", "g.png", hex("e"), 9)]), assets: [asset("g1", "g.png", hex("e"), 9)] })));
  add("publish-missing", { op: "publish", id: "nope" });
  add("publish-not-working", { op: "publish", id: "c1" });
  add("publish-w2-task-mismatch", { op: "publish", id: "w2", task: task("t3", "n3", "body", "w2", "failed") });
  add("task-for-w2", { op: "saveTask", task: task("t3", "n3", "body", "w2", "running", { updatedAt: 30 }) });
  add("publish-w2", { op: "publish", id: "w2", task: task("t3", "n3", "body", "w2", "completed", { updatedAt: 31 }) });
  add("get-current-n3", { op: "get", item: "n3", kind: "body", role: "current" });
  add("get-previous-n3", { op: "get", item: "n3", kind: "body", role: "previous" });
  add("another-working", A(ver("w3", "n3", "body", "working", { design: design() })));
  add("task-w3-cancelled", { op: "saveTask", task: task("t4", "n3", "body", "w3", "cancelled", { updatedAt: 40 }) });
  add("publish-w3-stopped", { op: "publish", id: "w3" });
  add("task-w3-completed", { op: "saveTask", task: task("t4", "n3", "body", "w3", "completed", { updatedAt: 41 }) });
  add("publish-w3", { op: "publish", id: "w3" });
  add("restore-previous", { op: "restorePrevious", item: "n3", kind: "body" });
  add("restore-previous-again", { op: "restorePrevious", item: "n3", kind: "body" });
  add("restore-none", { op: "restorePrevious", item: "n1", kind: "summary" });
  add("restore-deleted", { op: "restorePrevious", item: "n2", kind: "body" });
  add("sql-corrupt-payload", { op: "sql", sql: "UPDATE themed_reading_versions SET payload=? WHERE id=?", args: ["{}", "w2"] });
  add("get-corrupt", { op: "getVersion", id: "w2" });
  add("get-corrupt-by-role", { op: "get", item: "n3", kind: "body", role: "previous" });
  add("listTasks-after", { op: "listTasks" });
  add("sql-mismatch-index", { op: "sql", sql: "UPDATE themed_reading_versions SET role='previous' WHERE id=?", args: ["w1b"] });
  add("get-mismatch", { op: "getVersion", id: "w1b" });
  add("sql-restore-index", { op: "sql", sql: "UPDATE themed_reading_versions SET role='working' WHERE id=?", args: ["w1b"] });
  add("sql-task-state", { op: "sql", sql: "UPDATE themed_reading_tasks SET state='failed' WHERE id=?", args: ["t1"] });
  add("get-task-mismatch", { op: "getTask", id: "t1" });
  add("sql-task-state-back", { op: "sql", sql: "UPDATE themed_reading_tasks SET state='running' WHERE id=?", args: ["t1"] });
  add("interrupt", { op: "interruptRunning" });
  add("hasActive-no", { op: "hasActiveTasks" });
  add("listAssetFiles", { op: "listAssetFiles" });
  add("remove-n1-summary", { op: "remove", item: "n1", kind: "summary" });
  add("remove-n1-body", { op: "remove", item: "n1", kind: "body" });
  add("remove-n3", { op: "remove", item: "n3", kind: "body" });
  add("listAssetFiles-after", { op: "listAssetFiles" });
  return s;
}

const mask = (v: any): any => {
  if (Array.isArray(v)) return v.map(mask);
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, (k === "updatedAt" || k === "updated_at") && typeof x === "number" && x > 1e11 ? "NOW" : mask(x)]));
  return v;
};
const errText = (e: unknown) => { const m = e instanceof Error ? e.message : String(e); return /constraint|UNIQUE|FOREIGN|CHECK/i.test(m) ? "SQLERR" : m; };

export async function buildThemedDbGolden() {
  const { initDatabase, closeDatabase } = await import("../../packages/db/src/index.ts");
  const { ThemedReadingDB } = await import("../../packages/db/src/themed-reading.ts");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-themeddb-"));
  const db: any = initDatabase(path.join(dir, "t.db"));
  const ins = db.prepare("INSERT INTO knowledge_items (id,title,content,transcript,item_type,deleted_at,created_at,updated_at) VALUES (?,?,?,?,?,?,1,1)");
  for (const it of themedDbItems) ins.run(it.id, "t", "c", null, "note", it.deletedAt);
  const pages = new ThemedReadingDB(db);
  const dump = () => mask({
    versions: db.prepare("SELECT id,item_id,source_kind,role,payload,created_at,updated_at FROM themed_reading_versions ORDER BY id").all().map((r: any) => ({ ...r, payload: JSON.parse(r.payload) })),
    assets: db.prepare("SELECT version_id,file_name,sha256,bytes FROM themed_reading_assets ORDER BY version_id,file_name").all(),
    tasks: db.prepare("SELECT id,item_id,source_kind,version_id,state,payload,created_at,updated_at FROM themed_reading_tasks ORDER BY id").all().map((r: any) => ({ ...r, payload: JSON.parse(r.payload) })),
  });
  const recs: { kind: string; name: string; input: unknown; output: unknown }[] = [];
  for (const { name, op } of themedDbScript()) {
    let out: any;
    try {
      switch (op.op) {
        case "saveVersion": out = { ok: pages.saveVersion(op.version as any, op.task as any) }; break;
        case "saveTask": out = { ok: pages.saveTask(op.task as any) }; break;
        case "get": out = { ok: op.role ? pages.get(op.item as string, op.kind as any, op.role as any) : pages.get(op.item as string, op.kind as any) }; break;
        case "getVersion": out = { ok: pages.getVersion(op.id as string) }; break;
        case "getTask": out = { ok: pages.getTask(op.id as string) }; break;
        case "listTasks": out = { ok: pages.listTasks() }; break;
        case "publish": out = { ok: pages.publish(op.id as string, op.task as any) }; break;
        case "restorePrevious": out = { ok: pages.restorePrevious(op.item as string, op.kind as any) }; break;
        case "remove": out = { ok: pages.remove(op.item as string, op.kind as any) }; break;
        case "interruptRunning": out = { ok: pages.interruptRunning() }; break;
        case "hasActiveTasks": out = { ok: pages.hasActiveTasks() }; break;
        case "listAssetFiles": out = { ok: pages.listAssetFiles() }; break;
        case "sql": db.prepare(op.sql as string).run(...(op.args as unknown[])); out = { ok: null }; break;
        default: throw new Error("未知操作 " + op.op);
      }
      out = { ok: out.ok === undefined ? null : mask(out.ok) };
    } catch (e) { out = { error: errText(e) }; }
    recs.push({ kind: "thmDb", name, input: op, output: { result: out, state: dump() } });
  }
  closeDatabase();
  fs.rmSync(dir, { recursive: true, force: true });
  return { recs, items: themedDbItems };
}
