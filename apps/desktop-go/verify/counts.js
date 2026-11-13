// 探针：等界面起来后返回条目计数（用来确认损坏修复 / 恢复后数据仍完整）。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const t0 = Date.now();
while (!document.querySelector('[data-testid="topbar-search"]')) {
  if (Date.now() - t0 > 20000) throw new Error("界面未就绪");
  await sleep(100);
}
const c = await window.api.knowledge.counts();
return { all: c.all, trash: c.trash, favorites: c.favorites };
