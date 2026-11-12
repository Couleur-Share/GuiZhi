// 真实 UI 备份 → 改库 → 恢复 → 重启 整链路（-shotjs 脚本；由 run-backup-chain.ps1 驱动）。
// 一个脚本两个阶段：恢复会让应用 relaunch，子进程沿用同样的命令行，所以脚本按「备份列表里是否已有 pre-restore」判断阶段
// （页面 origin 带随机端口，localStorage 跨进程不共享，不能用它做标记）。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (fn, what, ms = 20000) => {
  const t0 = Date.now();
  for (;;) {
    const v = fn();
    if (v) return v;
    if (Date.now() - t0 > ms) throw new Error("等待超时：" + what);
    await sleep(100);
  }
};
const tid = (id) => document.querySelector(`[data-testid="${id}"]`);
const btnByText = (root, re) => [...root.querySelectorAll("button")].find((b) => re.test(b.textContent || ""));
const out = { steps: [] };
const step = (name, data) => out.steps.push({ name, ...(data === undefined ? {} : { data }) });

await until(() => tid("topbar-search"), "顶栏搜索框");
await sleep(500);
const later = btnByText(document, /稍后再说|maybe later/i);
if (later) later.click();

const backupsBefore = await window.api.backup.list();
const phase = backupsBefore.some((b) => b.kind === "pre-restore") ? 1 : 0;
out.phase = phase;
const counts = await window.api.knowledge.counts();
step("counts", counts);

if (phase === 0) {
  // 1) 真实 UI：设置 → 数据 → 立即备份
  tid("rail-settings").click();
  await until(() => tid("settings-nav-data"), "设置导航");
  tid("settings-nav-data").click();
  await until(() => tid("data-settings"), "数据设置页");
  const t0 = performance.now();
  tid("backup-create").click();
  const row = await until(
    () => [...document.querySelectorAll('[data-testid="data-settings"] li')].find((li) => /knowledge-manual-/.test(li.textContent || "")),
    "备份列表出现 knowledge-manual-",
  );
  step("ui-backup-created", { ms: Math.round(performance.now() - t0), row: row.textContent.trim().slice(0, 120) });

  // 2) 改库：新增条目 + 把一条放进回收站，再确认列表确实变了
  const created = await window.api.knowledge.create({ title: "VERIFY-AFTER-BACKUP", content: "备份之后才有的条目 zzqq" });
  const all = await window.api.knowledge.list({ scope: "all", pageSize: 1 });
  step("created", { id: created.id, total: all.total ?? all.entries?.length });
  const hit = await window.api.knowledge.list({ scope: "all", search: "zzqq" });
  step("fts-hit-before-restore", { n: (hit.entries || []).length });
  step("counts-after-mutation", await window.api.knowledge.counts());

  // 3) 真实 UI：点「恢复」→ 确认。成功后主进程 relaunch，此进程随之退出
  btnByText(row, /恢复|restore/i).click();
  const confirmBtn = await until(() => {
    const dlg = document.querySelector('[role="dialog"],[role="alertdialog"]');
    return dlg && btnByText(dlg, /^(确认|confirm)$/i);
  }, "恢复确认框");
  const tr = performance.now();
  confirmBtn.click();
  step("ui-restore-clicked", { at: Math.round(tr) });
  // 不能再等：恢复成功后主进程很快 relaunch 并退出，窗口一销毁 Eval 就拿不到返回值。立即返回。
} else {
  // 阶段 1：已重启。数据应回到备份时刻
  const gone = await window.api.knowledge.list({ scope: "all", search: "zzqq" });
  step("fts-hit-after-restore", { n: (gone.entries || []).length });
  const title = await window.api.knowledge.list({ scope: "all", search: "VERIFY-AFTER-BACKUP" });
  step("title-hit-after-restore", { n: (title.entries || []).length });
  step("backup-kinds", backupsBefore.map((b) => b.kind + ":" + b.fileName));
  // 正向对照：备份时刻就存在的条目恢复后仍可被全文检索命中（防止“搜不到”其实是索引整体坏了）
  const known = await window.api.knowledge.list({ scope: "all", search: "FTS5" });
  step("fts-known-item-after-restore", { n: (known.entries || []).length });
  out.ok = (gone.entries || []).length === 0 && (title.entries || []).length === 0 && (known.entries || []).length >= 1 && counts.all === 22;
}
return out;
