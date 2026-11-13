// 大库下的真实 IPC 耗时（-shotjs 脚本）。数值是渲染进程里 invoke 往返的毫秒数。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const t = {};
const time = async (k, f) => {
  const s = performance.now();
  const v = await f();
  t[k] = Math.round(performance.now() - s);
  return v;
};
const t0 = Date.now();
while (!document.querySelector('[data-testid="topbar-search"]')) {
  if (Date.now() - t0 > 120000) throw new Error("界面 120 s 内未就绪");
  await sleep(100);
}
t.pageReadyMs = Math.round(performance.now());
const later = [...document.querySelectorAll("button")].find((b) => /稍后再说|maybe later/i.test(b.textContent || ""));
if (later) later.click();

// 界面自己启动时会并发发出 counts/list 等查询，它们与本脚本共用同一条 SQLite 连接会排队；先等其稳定，再测各项的独立耗时。
await sleep(15000);
const out = { t };
out.counts = await time("counts", () => window.api.knowledge.counts());
out.counts = { all: out.counts.all, trash: out.counts.trash };
const page1 = await time("listFirstPage", () => window.api.knowledge.list({ scope: "all", limit: 50 }));
out.firstPage = { n: page1.entries.length, total: page1.total };
await time("listNextPage", () => window.api.knowledge.list({ scope: "all", limit: 50, cursor: page1.nextCursor }));
await time("listFavorites", () => window.api.knowledge.list({ scope: "favorites", limit: 50 }));
const rare = await time("searchRare", () => window.api.knowledge.list({ scope: "all", search: "ZXQVBIG10000", limit: 50 }));
out.rareHits = (rare.entries || []).length;
const common = await time("searchCommon", () => window.api.knowledge.list({ scope: "all", search: "SQLite", limit: 50 }));
out.commonHits = { n: (common.entries || []).length, total: common.total };
await time("searchCommonZh", () => window.api.knowledge.list({ scope: "all", search: "知识库 检索", limit: 50 }));
await time("searchRecall", () => window.api.knowledge.list({ scope: "all", search: "备份 恢复 迁移", searchMode: "recall", limit: 50 }));
await time("facetCountsWithSearch", () => window.api.knowledge.counts({ scope: "all", search: "SQLite" }));
const first = page1.entries[0];
await time("getOne", () => window.api.knowledge.get(first.id));
const created = await time("createOne", () => window.api.knowledge.create({ title: "大库写入探针", content: "probe zzbigprobe" }));
await time("searchJustCreated", () => window.api.knowledge.list({ scope: "all", search: "zzbigprobe", limit: 5 }));
await time("trashOne", () => window.api.knowledge.moveToTrash([created.id]));
const bk = await time("backupCreate(单文件快照)", () => window.api.backup.create());
out.backup = { success: bk.success, size: bk.backup && bk.backup.sizeBytes };
return out;
