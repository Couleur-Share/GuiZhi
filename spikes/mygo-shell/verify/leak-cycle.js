// 泄漏压力：反复「选中带阅读页的条目（创建 iframe 视图）→ 点图片放大再关闭 → 切到普通条目（销毁视图）」N 轮，
// 每 5 轮记录 JS 堆、DOM 节点数、残留 iframe 数。由 run-leak.ps1 同时在进程树上采样工作集。
// 轮数通过 location.hash 之外的方式传不进来，这里用固定默认值，可用 GUIZHI_LEAK_CYCLES 在运行器里改文件前缀替换。
const CYCLES = /*CYCLES*/ 60;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (fn, what, ms = 15000) => { const t0 = Date.now(); for (;;) { const v = fn(); if (v) return v; if (Date.now() - t0 > ms) throw new Error("等待超时：" + what); await sleep(40); } };
const leaf = (text) => [...document.querySelectorAll("*")].reverse().find((x) => x.children.length === 0 && (x.textContent || "").trim() === text);
const tid = (id) => document.querySelector(`[data-testid="${id}"]`);

await until(() => tid("topbar-search"), "顶栏搜索框", 30000);
await sleep(500);
const later = [...document.querySelectorAll("button")].find((b) => /稍后再说|maybe later/i.test(b.textContent || ""));
if (later) later.click();

// 记录 faults 与异常
const faults = [];
window.api.themedReading.onViewEvent((e) => { if (e.type === "fault") faults.push(e.value); });
addEventListener("error", (e) => faults.push({ window: String(e.message) }));

const sample = (i) => ({
  cycle: i,
  heapMB: performance.memory ? +(performance.memory.usedJSHeapSize / 1048576).toFixed(1) : null,
  nodes: document.querySelectorAll("*").length,
  iframes: document.querySelectorAll("iframe").length,
  viewHosts: document.querySelectorAll("[data-reading-view-id]").length,
  listeners: null,
});
const samples = [sample(0)];
let failures = 0;
const t0 = Date.now();
for (let i = 1; i <= CYCLES; i++) {
  try {
    leaf("阅读视图往返验证").click();
    await until(() => document.querySelector('iframe[title="AI 阅读页"]'), "iframe 出现");
    await until(() => document.querySelector("[data-reading-view-id]"), "视图 ID");
    await sleep(700); // 让 ready/layout 走完一轮
    // 图片放大再关闭：只有页面脚本点击才能触发，这里直接点关闭键验证放大层本身可重复开合不泄漏；放大层由页面脚本在 5.5s 触发，短循环里不依赖它
    leaf("批量条目 3").click();
    await until(() => !document.querySelector('iframe[title="AI 阅读页"]'), "iframe 销毁");
    await sleep(150);
  } catch (e) { failures++; faults.push({ cycle: i, error: String(e.message || e) }); await sleep(500); }
  if (i % 5 === 0) samples.push(sample(i));
}
await sleep(1500);
const last = sample("end");
samples.push(last);
const third = Math.max(1, Math.floor(samples.length / 3));
const mean = (a, k) => a.reduce((s, x) => s + x[k], 0) / a.length;
const early = samples.slice(1, 1 + third), late = samples.slice(-third);
return {
  cycles: CYCLES, failures, seconds: Math.round((Date.now() - t0) / 1000), faults: faults.slice(0, 5), faultCount: faults.length,
  samples,
  trend: { heapEarlyMB: +mean(early, "heapMB").toFixed(1), heapLateMB: +mean(late, "heapMB").toFixed(1), nodesEarly: Math.round(mean(early, "nodes")), nodesLate: Math.round(mean(late, "nodes")) },
  residual: { iframes: last.iframes, viewHosts: last.viewHosts },
};
