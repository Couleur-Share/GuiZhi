// 探针：新建条目后各种查询能否命中（用来校准备份链路脚本里的断言）。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
while (!document.querySelector('[data-testid="topbar-search"]')) await sleep(100);
const created = await window.api.knowledge.create({ title: "VERIFY-AFTER-BACKUP", content: "备份之后才有的条目 zzqq" });
const q = async (s) => {
  const r = await window.api.knowledge.list({ scope: "all", search: s });
  return { q: s, n: (r.entries || []).length, keys: Object.keys(r) };
};
return { id: created.id, status: created.status, hits: [await q("zzqq"), await q("VERIFY"), await q("VERIFY-AFTER-BACKUP"), await q("备份之后"), await q("条目")] };
