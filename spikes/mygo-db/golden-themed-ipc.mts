// 主题阅读同步频道差分基线：真实 themed-reading.ipc.ts（electron 换桩，ipcMain 收集处理函数）
// 在临时库与隔离数据目录上按剧本调用，记录每个频道的返回值；导出的 HTML 文件与图片夹具一并落盘，
// Go 端 themed_ipc_test.go 用同一份库种子与图片回放。时间戳统一抹成 "NOW"，search 字段只比对存在性。
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { MARKDOWNS, TEXT, v1Page, v3Page } from "./golden-themed.mts";

/* eslint-disable @typescript-eslint/no-explicit-any */
export type IpcCall = { ch: string; args: unknown[]; note?: string };

export const sha = (b: Buffer) => crypto.createHash("sha256").update(b).digest("hex");
export function pngBytes(tag: string, extra = 0): Buffer { return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), Buffer.from(tag), Buffer.alloc(extra, 7)]); }

export const imageFixtures: Record<string, Buffer> = {
  "ok-a.png": pngBytes("okA", 20),
  "ok-b.png": pngBytes("okB", 40),
  "tampered.png": pngBytes("tamper", 30),
  "wrong-size.png": pngBytes("size", 50),
  "not-image.png": Buffer.from("this is not an image at all!!"),
};
export const fingerprint = (title: string, content: string, uri: string | null, kind: string) => crypto.createHash("sha256").update(JSON.stringify([title, content, uri, kind])).digest("hex");

const forumBody = "> 平台：NGA\n\n## 正文\n\n楼主正文内容\n\n## 讨论总结\n\n这是讨论总结文本\n\n## 讨论（共 1 楼）\n\n#1 甲\n内容";
export const ipcItems = [
  { id: "it", title: "笔记", content: "笔记正文。\n\n第二段。", itemType: "note", deletedAt: null as number | null, sourceUri: "https://example.com/a" },
  { id: "it-nosrc", title: "无来源", content: TEXT, itemType: "note", deletedAt: null, sourceUri: null },
  { id: "gone", title: "已删除", content: "x", itemType: "note", deletedAt: 1700000000000, sourceUri: null },
  { id: "forum", title: "论坛帖", content: forumBody, itemType: "forum", deletedAt: null, sourceUri: "https://nga.example/t/1" },
  { id: "img", title: "图文", content: "文案很长\n\n![a](u)\n\n## 图中文字\n\nocr", itemType: "image", deletedAt: null, sourceUri: null },
  { id: "vid", title: "视频", content: "> 平台：抖音 · 作者：甲 · 时长：1:02\n\n视频正文", itemType: "video", deletedAt: null, sourceUri: null },
];

const asset = (id: string, file: string | undefined, over: Record<string, unknown> = {}) => ({
  id, role: "original", purpose: "p", prompt: "", alt: `图${id}`, aspectRatio: "4:3", status: "ready", ...(file ? { fileName: file } : {}), ...over,
});
const goodAsset = (id: string, file: string) => asset(id, file, { sha256: sha(imageFixtures[file]), bytes: imageFixtures[file].length });

export function ipcSeed(): { versions: any[]; tasks: any[] } {
  const note = ipcItems[0];
  const fpNote = fingerprint(note.title, note.content, note.sourceUri, "body");
  const withFp = (p: any, fp: string) => ({ ...p, source: { ...p.source, fingerprint: fp } });
  const base = (id: string, item: string, kind: string, role: string, over: Record<string, unknown> = {}) => ({ ...v1Page("links", { renderThemedReadingBlocks: (md: string) => [{ id: "b0", markdown: md, html: `<p>${md}</p>`, text: md }] }), id, itemId: item, sourceKind: kind, role, ...over });
  const v1Design = (assets: any[]) => ({ direction: "d", css: ":root{--theme-text:#123}", assets: [], html: `<main>${assets.map(a => `<img data-theme-asset="${a.id}" alt="x">`).join("")}<div data-source-block="b0"></div></main>` });
  const versions: any[] = [];
  const tasks: any[] = [];
  const fixRec = (p: any) => { p.reconstruction.visuals ??= []; p.reconstruction.animations ??= []; return p; };
  const t = (id: string, item: string, kind: string, versionId: string, state: string, createdAt: number, over: Record<string, unknown> = {}) =>
    tasks.push({ id, itemId: item, sourceKind: kind, versionId, title: "标题", state, stage: "done", completed: 1, total: 1, createdAt, updatedAt: createdAt + 5, ...over });

  // it/body：v3 当前页（新鲜）+ 早期旧任务；it/summary：旧页（对笔记没有摘要来源）
  const v3 = v3Page({ id: "it-v3", itemId: "it", sourceKind: "body", role: "current", createdAt: 100, updatedAt: 100 });
  v3.source = { ...v3.source, title: note.title, content: note.content, sourceUri: note.sourceUri, fingerprint: fpNote };
  v3.generation = { route: "short", sections: [{ id: "s1", html: "<h2 id=\"s1\">一</h2><p>甲</p>" }, { id: "s2", html: "<p>乙</p>" }], css: ".x{color:red}", scripts: [], libraries: [], revision: 3, done: true, repairs: {}, issues: [] };
  v3.designDirection = "阅读优先";
  v3.reconstruction.references = [
    { id: "r1", url: "https://r.example/1", title: "资料一", status: "ready", text: "全文一", capturedAt: 5 },
    { id: "r2", url: "https://r.example/2", title: "资料二", status: "ready", text: "全文二", capturedAt: 5 },
    { id: "r3", url: "https://r.example/3", title: "资料三", status: "failed", error: "x", text: "", capturedAt: 5 },
    { id: "r4", url: "https://r.example/4", title: "资料四", status: "ready", text: "全文四", capturedAt: 5 },
  ];
  v3.reconstruction.draft = [{ title: "完整正文", markdown: TEXT, referenceIds: ["r1", "r2"] }];
  versions.push(fixRec(v3));
  t("t-it-old", "it", "body", "it-v3", "completed", 1000);
  t("t-it-new", "it", "body", "it-v3", "partial", 2000, { error: "有警告" });

  // it-nosrc/body：过期的 v1 页（指纹不匹配），带文档与图片
  const a1 = goodAsset("a1", "ok-a.png");
  versions.push({ ...base("ns-v1", "it-nosrc", "body", "current", { assets: [a1], design: v1Design([a1]) }), source: { ...base("x", "x", "body", "current").source, fingerprint: "a".repeat(64) } });

  // forum/body：v1 页，三张图：缺失 / 长度不符 / 内容被改；forum/summary：当前 + 上一版
  const lost = goodAsset("lost", "ok-b.png"); lost.fileName = "missing.png";
  const wrongSize = { ...goodAsset("ws", "wrong-size.png"), bytes: 7 };
  const tampered = { ...goodAsset("tp", "tampered.png"), sha256: "e".repeat(64) };
  const notImage = { ...goodAsset("ni", "not-image.png") };
  const forumFp = (kind: string) => fingerprint("论坛帖", kind === "body" ? "楼主正文内容" : "这是讨论总结文本", "https://nga.example/t/1", kind);
  const forumSrc = (kind: string) => ({ ...base("x", "x", kind, "current").source, title: "论坛帖", content: kind === "body" ? "楼主正文内容" : "这是讨论总结文本", sourceUri: "https://nga.example/t/1", fingerprint: forumFp(kind) });
  versions.push({ ...base("fb-v1", "forum", "body", "current", { assets: [lost, wrongSize, tampered, notImage, goodAsset("ok", "ok-a.png")], design: v1Design([lost, wrongSize, tampered, notImage]) }), source: forumSrc("body") });
  versions.push({ ...base("fs-cur", "forum", "summary", "current", { assets: [goodAsset("a", "ok-a.png")], design: v1Design([goodAsset("a", "ok-a.png")]) }), source: forumSrc("summary") });
  versions.push({ ...base("fs-prev", "forum", "summary", "previous", { assets: [goodAsset("b", "ok-b.png")], design: v1Design([goodAsset("b", "ok-b.png")]), createdAt: 5, updatedAt: 5 }), source: forumSrc("summary") });
  t("t-forum", "forum", "summary", "fs-cur", "completed", 3000);

  // img/body：上一版图片已损坏（可恢复检查）；vid/body：上一版里原本就不完整的素材
  versions.push({ ...base("img-cur", "img", "body", "current", { assets: [goodAsset("a", "ok-a.png")], design: v1Design([goodAsset("a", "ok-a.png")]) }), source: { ...base("x", "x", "body", "current").source, title: "图文", content: "文案很长", sourceUri: null, fingerprint: fingerprint("图文", "文案很长", null, "body") } });
  versions.push({ ...base("img-prev", "img", "body", "previous", { assets: [{ ...lost }], design: v1Design([lost]), createdAt: 5, updatedAt: 5 }), source: base("x", "x", "body", "current").source });
  versions.push({ ...base("vid-cur", "vid", "body", "current", { assets: [goodAsset("a", "ok-a.png")], design: v1Design([goodAsset("a", "ok-a.png")]) }) });
  const pending = asset("p", undefined, { status: "pending" });
  versions.push({ ...base("vid-prev", "vid", "body", "previous", { assets: [pending, goodAsset("a", "ok-a.png")], design: v1Design([pending, goodAsset("a", "ok-a.png")]), createdAt: 5, updatedAt: 5 }) });

  // it-nosrc/summary 工作版本 + 运行中的任务（预览与取消）；gone 的页（回收站只读）
  const working = v3Page({ id: "ns-work", itemId: "it-nosrc", sourceKind: "summary", role: "working", createdAt: 200, updatedAt: 200 });
  working.generation = { route: "short", sections: [{ id: "s1", html: "<p>草稿</p>" }], css: "", scripts: [], libraries: [], revision: 1, done: false, repairs: {}, issues: [] };
  working.designDirection = "d";
  versions.push(fixRec(working));
  t("t-work", "it-nosrc", "summary", "ns-work", "queued", 4000, { stage: "write" });
  const emptyWork = v3Page({ id: "ns-empty", itemId: "it-nosrc", sourceKind: "body", role: "working", createdAt: 210, updatedAt: 210 });
  versions.push(fixRec(emptyWork));
  t("t-empty", "it-nosrc", "body", "ns-empty", "completed", 4100);
  versions.push({ ...base("gone-v1", "gone", "body", "current", { assets: [], design: v1Design([]) }) });
  return { versions, tasks };
}

export function ipcScript(): IpcCall[] {
  const req = (item: string, kind: string, extra: Record<string, unknown> = {}) => ({ itemId: item, sourceKind: kind, ...extra });
  const c: IpcCall[] = [];
  const add = (ch: string, ...args: unknown[]) => c.push({ ch, args });
  for (const bad of [undefined, null, {}, { itemId: "" }, { itemId: "  ", sourceKind: "body" }, { itemId: "a".repeat(201), sourceKind: "body" }, { itemId: "it", sourceKind: "other" }, "str", 5]) {
    add("themedReading:state", bad === undefined ? "@@undef@@" : bad);
  }
  add("themedReading:state", req("it", "body"));
  add("themedReading:state", req("it", "summary"));
  add("themedReading:state", req("it-nosrc", "body"));
  add("themedReading:state", req("it-nosrc", "summary"));
  add("themedReading:state", req("forum", "body"));
  add("themedReading:state", req("forum", "summary"));
  add("themedReading:state", req("img", "body"));
  add("themedReading:state", req("zzz", "body"));
  add("themedReading:state", req("gone", "body"));
  add("themedReading:state", req("vid", "body"));

  add("themedReading:get", req("it", "body"));
  add("themedReading:get", req("it", "body", { metadataOnly: true }));
  add("themedReading:get", req("it", "body", { instanceId: "inst-1" }));
  add("themedReading:get", req("it", "summary"));
  add("themedReading:get", req("it-nosrc", "body"));
  add("themedReading:get", req("it-nosrc", "body", { instanceId: "inst_2-x" }));
  add("themedReading:get", req("it-nosrc", "body", { instanceId: "bad id!" }));
  add("themedReading:get", req("it-nosrc", "body", { metadataOnly: true }));
  add("themedReading:get", req("it-nosrc", "summary"));
  add("themedReading:get", req("forum", "body"));
  add("themedReading:get", req("forum", "summary"));
  add("themedReading:get", req("forum", "summary", { metadataOnly: true }));
  add("themedReading:get", req("img", "body"));
  add("themedReading:get", req("vid", "body"));
  add("themedReading:get", req("gone", "body"));
  add("themedReading:get", req("zzz", "body"));
  add("themedReading:get", { itemId: "it", sourceKind: "weird" });

  add("themedReading:references", req("it", "body"));
  add("themedReading:references", req("forum", "body"));
  add("themedReading:references", req("it", "summary"));
  add("themedReading:references", { itemId: "", sourceKind: "body" });
  add("themedReading:listTasks");

  add("themedReading:preview", req("it-nosrc", "summary", { taskId: "t-work" }));
  add("themedReading:preview", req("it-nosrc", "body", { taskId: "t-empty" }));
  add("themedReading:preview", req("it", "body", { taskId: "t-it-new" }));
  add("themedReading:preview", req("it", "summary", { taskId: "t-it-new" }));
  add("themedReading:preview", req("it", "body", { taskId: "nope" }));
  add("themedReading:preview", req("it", "body", {}));
  add("themedReading:preview", { itemId: "it" });

  add("themedReading:exportHtml", req("forum", "summary"));
  add("themedReading:exportHtml", req("forum", "summary", { withoutImages: true }));
  add("themedReading:exportHtml", req("forum", "body"));
  add("themedReading:exportHtml", req("forum", "body", { withoutImages: true }));
  add("themedReading:exportHtml", req("it", "body"));
  add("themedReading:exportHtml", req("it", "body", { staticOnly: true }));
  add("themedReading:exportHtml", req("it", "body", { staticOnly: true, withoutImages: true }));
  add("themedReading:exportHtml", req("it-nosrc", "summary"));
  add("themedReading:exportHtml", req("it-nosrc", "summary", { versionId: "ns-work" }));
  add("themedReading:exportHtml", req("it-nosrc", "body", { versionId: "ns-v1" }));
  add("themedReading:exportHtml", req("it", "body", { versionId: "ns-v1" }));
  add("themedReading:exportHtml", req("it", "summary"));
  add("themedReading:exportHtml", req("zzz", "body"));

  add("themedReading:restorePrevious", req("forum", "summary"));
  add("themedReading:restorePrevious", req("forum", "summary"));
  add("themedReading:restorePrevious", req("it", "body"));
  add("themedReading:restorePrevious", req("img", "body"));
  add("themedReading:restorePrevious", req("vid", "body"));
  add("themedReading:restorePrevious", req("gone", "body"));
  add("themedReading:restorePrevious", req("it-nosrc", "summary"));

  add("themedReading:remove", req("gone", "body"));
  add("themedReading:remove", req("zzz", "body"));
  add("themedReading:remove", req("it-nosrc", "summary"));
  add("themedReading:remove", req("it-nosrc", "summary"));
  add("themedReading:remove", req("it", "body"));
  add("themedReading:listTasks");
  add("themedReading:get", req("it", "body"));
  return c;
}

const mask = (v: any): any => {
  if (Array.isArray(v)) return v.map(mask);
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, (k === "updatedAt" || k === "updated_at") && typeof x === "number" && x > 1e11 ? "NOW" : mask(x)]));
  return v;
};

export async function buildThemedIpcGolden(outDir: string) {
  const { configureRuntimePaths, getImagesDir } = await import("../../apps/desktop/src/main/runtime-paths.ts");
  configureRuntimePaths({ userDataPath: path.join(outDir, "userdata") });
  fs.mkdirSync(getImagesDir(), { recursive: true });
  for (const [name, data] of Object.entries(imageFixtures)) fs.writeFileSync(path.join(getImagesDir(), name), data);
  const { initDatabase, closeDatabase } = await import("../../packages/db/src/index.ts");
  const { ThemedReadingDB } = await import("../../packages/db/src/themed-reading.ts");
  let db: any = initDatabase(path.join(outDir, "seed.db"));
  const ins = db.prepare("INSERT INTO knowledge_items (id,title,content,transcript,item_type,deleted_at,created_at,updated_at) VALUES (?,?,?,?,?,?,1,1)");
  for (const it of ipcItems) {
    ins.run(it.id, it.title, it.content, null, it.itemType, null);
    if (it.sourceUri) db.prepare("INSERT INTO source_records (id,item_id,source_type,source_uri,normalized_uri,content_hash,captured_at,platform) VALUES (?,?,?,?,?,?,?,?)").run(`sr-${it.id}`, it.id, "url", it.sourceUri, it.sourceUri, "h", 1, null);
  }
  const seed = ipcSeed();
  const pages = new ThemedReadingDB(db);
  for (const v of seed.versions) pages.saveVersion(v);
  // 任务按版本外键写入；活动任务（queued）放最后，避免被启动清理改动
  for (const task of seed.tasks) pages.saveTask(task);
  // 回收站条目在写入页面之后才标记删除（保存页面要求条目在用）
  for (const it of ipcItems) if (it.deletedAt !== null) db.prepare("UPDATE knowledge_items SET deleted_at=? WHERE id=?").run(it.deletedAt, it.id);
  closeDatabase();
  fs.copyFileSync(path.join(outDir, "seed.db"), path.join(outDir, "ipc.db"));
  db = initDatabase(path.join(outDir, "ipc.db"));

  const handlers = new Map<string, (...a: any[]) => any>();
  const sender = { mainFrame: {} } as any;
  const win = { webContents: sender, isDestroyed: () => false };
  const exportDir = path.join(outDir, "exports");
  fs.mkdirSync(exportDir, { recursive: true });
  let exportN = 0;
  const g: any = globalThis;
  g.__themedIpcElectron = {
    ipcMain: { handle: (ch: string, fn: any) => handlers.set(ch, fn), removeHandler() {}, on() {}, removeListener() {} },
    BrowserWindow: { getAllWindows: () => [], fromWebContents: (wc: unknown) => (wc === sender ? win : null) },
    dialog: { showSaveDialog: async (_w: any, o?: any) => { const opt = o ?? _w; return { canceled: false, filePath: path.join(exportDir, `export-${++exportN}-${String(opt.defaultPath).replace(/[\\/:*?"<>|]/g, "_")}`) }; } },
  };
  const { registerThemedReadingIPC } = await import("../../apps/desktop/src/main/ipc/themed-reading.ipc.ts");
  registerThemedReadingIPC(db);

  const recs: { kind: string; name: string; input: unknown; output: unknown }[] = [];
  const script = ipcScript();
  for (const [i, call] of script.entries()) {
    const fn = handlers.get(call.ch);
    let out: any;
    if (!fn) out = { missing: true };
    else {
      const args = call.args.map(a => (a === "@@undef@@" ? undefined : a));
      try { out = await fn({ sender, senderFrame: sender.mainFrame }, ...args); } catch (e) { out = { thrown: e instanceof Error ? e.message : String(e) }; }
    }
    out = JSON.parse(JSON.stringify(out ?? null));
    if (out && typeof out === "object" && "search" in out) out.search = "@@present@@";
    const rec: any = { kind: "thmIpc", name: `${String(i).padStart(3, "0")}-${call.ch.split(":")[1]}`, input: call, output: mask(out) };
    if (call.ch.endsWith("exportHtml") && out?.path) {
      rec.output.fileContent = fs.readFileSync(out.path, "utf8");
      rec.output.path = "@@file@@";
    }
    recs.push(rec);
  }
  const dump = mask({
    versions: db.prepare("SELECT id,item_id,source_kind,role FROM themed_reading_versions ORDER BY id").all(),
    tasks: db.prepare("SELECT id,state FROM themed_reading_tasks ORDER BY id").all(),
  });
  closeDatabase();
  return { recs, items: ipcItems, seed, dump, images: Object.keys(imageFixtures) };
}
