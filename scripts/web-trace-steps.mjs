/**
 * pnpm shot 专用：网页采集耗时拆账与「单次采集内代理连接复用」A/B 对照。
 * 真实公网，使用隔离副本与临时 userData；每个网址采集完成后删除生成的条目与任务。
 *
 * 用法（仓库根目录；归知开发实例无需关闭，验证不会连接它）：
 *   GUIZHI_TEST_PROXY=<本机代理地址> pnpm shot --steps scripts/web-trace-steps.mjs --out <新目录>
 *
 * 环境变量（均可选）：
 *   GUIZHI_TRACE_CORPUS      语料 JSON（{ urls: string[] } 或 string[]），默认 scripts/web-trace-corpus.json
 *   GUIZHI_TRACE_LIMIT       只跑前 N 个网址（冒烟）
 *   GUIZHI_TRACE_REPS        轮数，默认 1；奇数轮先 A 后 B，偶数轮先 B 后 A，抵消时间漂移
 *   GUIZHI_TRACE_VARIANT     对照变量：reuse（默认，连接复用）| settle（事件驱动等待 vs 固定等待）|
 *                            frames（跨源子框架不加载 vs 加载全部子框架）|
 *                            identity（静态请求带渲染窗口同一身份 vs 不带）|
 *                            inert（图片请求以占位图本地应答 vs 真实加载）
 *   GUIZHI_TRACE_CONDITIONS  默认 A,B；A=旧行为，B=新行为，含义随 GUIZHI_TRACE_VARIANT 而定
 *   GUIZHI_TRACE_PROXY_MODE  manual（默认，使用 GUIZHI_TEST_PROXY）| system | direct
 *                            system 会在隔离实例内固定代理规则为 GUIZHI_TEST_PROXY 来模拟系统代理，不改动操作系统设置
 *   GUIZHI_TRACE_TIMEOUT_S   单个网址等待上限，默认 180
 *   GUIZHI_TRACE_SAVE_CONTENT  设为 1 时把每次采集的正文存入 <out>/web-trace/content/，便于定位内容差异
 *   GUIZHI_TRACE_BASELINE    先前一次运行的 results.jsonl；逐页对照本次与它的 A 条件第 1 轮的正文哈希与字符数
 *
 * 产物（<out>/web-trace/）：trace-<条件>-r<轮>.jsonl（应用内追踪原文）、results.jsonl、summary.json；
 * 终端打印对照表。网页组件运行时是被忽略的构建产物，隔离副本里没有，脚本会为副本建立只读联接。
 */
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { testProxySettings } from "./lib/test-proxy.mjs";
import { createImporter } from "./lib/web-import.mjs";
import { ensureWebRuntime } from "./lib/web-runtime.mjs";

const REPO = path.resolve(import.meta.dirname, "..");
/**
 * 对照实验的变量：同一套脚本、同一份语料，A 为旧行为、B 为新行为。
 *   reuse  A=GUIZHI_WEB_CONNECTION_REUSE=0（每个请求新建连接） B=开启单次采集内连接复用
 *   settle A=GUIZHI_WEB_SETTLE=fixed（固定等待约 1.84 s）       B=事件驱动等待；两边都开启连接复用
 *   frames A=GUIZHI_WEB_SUBFRAMES=allow（加载全部子框架）       B=不加载跨源子框架；两边都开启连接复用与事件驱动等待
 *   identity A=GUIZHI_WEB_IDENTITY=none（静态请求不带 UA/Accept） B=静态请求带渲染窗口同一身份；两边都开启连接复用与事件驱动等待
 *   inert  A=GUIZHI_WEB_INERT_RESOURCES=load（图片真实加载）    B=图片请求以占位图本地应答；两边都开启连接复用与事件驱动等待
 */
const VARIANTS = {
  reuse: {
    labels: { A: "A 旧行为（关闭复用）", B: "B 新行为（开启复用）" },
    env: (cond) => ({ GUIZHI_WEB_CONNECTION_REUSE: cond === "B" ? "1" : "0" }),
  },
  settle: {
    labels: { A: "A 旧行为（固定等待）", B: "B 新行为（事件驱动等待）" },
    env: (cond) => ({
      GUIZHI_WEB_CONNECTION_REUSE: "1",
      GUIZHI_WEB_SETTLE: cond === "B" ? "quiet" : "fixed",
    }),
  },
  frames: {
    labels: { A: "A 旧行为（加载全部子框架）", B: "B 新行为（跨源子框架不加载）" },
    env: (cond) => ({
      GUIZHI_WEB_CONNECTION_REUSE: "1",
      GUIZHI_WEB_SETTLE: "quiet",
      GUIZHI_WEB_SUBFRAMES: cond === "B" ? "block" : "allow",
    }),
  },
  identity: {
    labels: { A: "A 旧行为（静态请求不带身份）", B: "B 新行为（静态请求带渲染窗口同一身份）" },
    env: (cond) => ({
      GUIZHI_WEB_CONNECTION_REUSE: "1",
      GUIZHI_WEB_SETTLE: "quiet",
      GUIZHI_WEB_IDENTITY: cond === "B" ? "browser" : "none",
    }),
  },
  inert: {
    labels: { A: "A 旧行为（渲染窗口真实加载图片）", B: "B 新行为（图片请求以占位图本地应答）" },
    env: (cond) => ({
      GUIZHI_WEB_CONNECTION_REUSE: "1",
      GUIZHI_WEB_SETTLE: "quiet",
      GUIZHI_WEB_INERT_RESOURCES: cond === "B" ? "stub" : "load",
    }),
  },
};
const variantName = process.env.GUIZHI_TRACE_VARIANT || "reuse";
if (!(variantName in VARIANTS)) throw new Error("GUIZHI_TRACE_VARIANT 只能是 reuse、settle、frames、identity 或 inert");
const VARIANT = VARIANTS[variantName];
const CONDITION_LABEL = VARIANT.labels;
const intEnv = (name, fallback) => {
  const value = Number.parseInt(process.env[name] ?? "", 10);
  return Number.isFinite(value) && value > 0 ? value : fallback;
};

function median(values) {
  const sorted = values.filter(Number.isFinite).sort((a, b) => a - b);
  if (!sorted.length) return null;
  const mid = sorted.length >> 1;
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}
/** 最近秩法分位数；空集返回 null。 */
function percentile(values, p) {
  const sorted = values.filter(Number.isFinite).sort((a, b) => a - b);
  return sorted.length ? sorted[Math.min(sorted.length - 1, Math.ceil(p * sorted.length) - 1)] : null;
}
const sec = (ms) => (ms == null ? "—" : `${(ms / 1000).toFixed(2)}s`);
const pct = (value) => (value == null ? "—" : `${(value * 100).toFixed(1)}%`);

function shingles(text, size = 5) {
  const flat = text.replace(/\s+/g, " ").trim();
  const set = new Set();
  for (let i = 0; i + size <= flat.length; i++) set.add(flat.slice(i, i + size));
  return set;
}
/** a 的片段中同时出现在 b 里的比例（单向包含率）。 */
function containment(a, b) {
  if (!a?.size || !b?.size) return null;
  let shared = 0;
  for (const item of a) if (b.has(item)) shared++;
  return shared / a.size;
}
/** 双向包含率的较小值：1 表示完全一致，对页面局部差异和排序变化不敏感。 */
function similarity(a, b) {
  const forward = containment(a, b);
  const backward = containment(b, a);
  return forward == null || backward == null ? null : Math.min(forward, backward);
}
const range = (values) => ({
  n: values.length,
  median: median(values),
  min: values.length ? Math.min(...values) : null,
  below098: values.filter((value) => value < 0.98).length,
});

function loadCorpus() {
  const file = path.resolve(
    process.env.GUIZHI_TRACE_CORPUS || path.join(REPO, "scripts/web-trace-corpus.json"),
  );
  const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
  const urls = Array.isArray(parsed) ? parsed : parsed.urls;
  if (!Array.isArray(urls) || !urls.length || urls.some((url) => typeof url !== "string"))
    throw new Error(`语料格式不正确：${file}`);
  return urls.slice(0, intEnv("GUIZHI_TRACE_LIMIT", urls.length));
}

/**
 * 失败请求画像：按「错误码 / 连接新建或复用 / 失败前是否已响应 / 同源或第三方 / 资源粗类」计数。
 * 追踪每页只保留前 8 条失败明细，所以这里是抽样画像，不等于失败总数。
 */
function profileFailures(records) {
  const counts = {};
  for (const record of records) {
    for (const failure of record.trace.egress?.failures ?? []) {
      const key = [
        failure.error,
        failure.reused ? "复用" : "新建",
        failure.responded ? "已响应" : "未响应",
        failure.origin === record.trace.origin ? "同源" : "第三方",
        failure.type,
      ].join(" / ");
      counts[key] = (counts[key] ?? 0) + 1;
    }
  }
  return Object.fromEntries(Object.entries(counts).sort((left, right) => right[1] - left[1]));
}

/** 按 Accept 粗分的资源类型汇总：请求数、字节数、请求耗时总和（并发重叠，不等于墙钟）。 */
function profileTypes(records) {
  const totals = {};
  for (const record of records) {
    for (const [type, entry] of Object.entries(record.trace.egress?.byType ?? {})) {
      const sum = (totals[type] ??= { n: 0, bytes: 0, ms: 0 });
      sum.n += entry.n;
      sum.bytes += entry.bytes;
      sum.ms += entry.ms;
    }
  }
  return totals;
}

export default async function ({ win, app, outDir, mainEntry }) {
  const urls = loadCorpus();
  const reps = intEnv("GUIZHI_TRACE_REPS", 1);
  const timeoutMs = intEnv("GUIZHI_TRACE_TIMEOUT_S", 180) * 1000;
  const conditions = (process.env.GUIZHI_TRACE_CONDITIONS || "A,B")
    .split(",")
    .map((item) => item.trim().toUpperCase())
    .filter((item) => item in CONDITION_LABEL);
  if (!conditions.length) throw new Error("GUIZHI_TRACE_CONDITIONS 只能包含 A、B");
  // 保存每次采集的正文到 <out>/web-trace/content/，用于定位 A/B 的内容差异（体积较大，默认关闭）。
  const saveContent = process.env.GUIZHI_TRACE_SAVE_CONTENT === "1";
  const proxyMode = process.env.GUIZHI_TRACE_PROXY_MODE || "manual";
  if (!["manual", "system", "direct"].includes(proxyMode))
    throw new Error("GUIZHI_TRACE_PROXY_MODE 只能是 manual、system 或 direct");

  const out = path.join(outDir, "web-trace");
  fs.mkdirSync(out, { recursive: true });
  const resultsFile = path.join(out, "results.jsonl");
  fs.writeFileSync(resultsFile, "");

  ensureWebRuntime(mainEntry);
  await win.evaluate(
    (networkProxy) => window.api.settings.set({ networkProxy }),
    proxyMode === "manual" ? testProxySettings() : { mode: proxyMode },
  );
  if (proxyMode === "system") {
    // 模拟「操作系统代理已开启」：不改动 Windows 设置，只在这个隔离实例的会话里固定代理规则，
    // 让 resolveProxy 返回 PROXY host:port，从而走 webNetworkRequest 的系统代理分支。
    const { host, port } = testProxySettings();
    await app.evaluate(
      ({ session }, rules) => session.defaultSession.setProxy({ proxyRules: rules }),
      `http://${host}:${port}`,
    );
    const resolved = await app.evaluate(({ session }) =>
      session.defaultSession.resolveProxy("https://example.com/"),
    );
    if (!/^PROXY\s/.test(resolved))
      throw new Error(`系统代理模拟未生效，resolveProxy 返回：${resolved}`);
    console.log(`系统代理模拟：resolveProxy → ${resolved}`);
  }
  const runtime = await win.evaluate(() => window.api.webCapture.status());
  if (!runtime?.ok || !runtime.data?.available)
    throw new Error(`网页组件不可用：${runtime?.data?.reason ?? runtime?.error ?? "未知原因"}`);

  // 在主进程设置本条件的开关与追踪文件；开关由采集路径在每次采集时读取，无需重启。
  const setEnv = (cond, traceFile) =>
    app.evaluate(
      (_electron, arg) => {
        Object.assign(process.env, arg.vars);
        process.env.GUIZHI_WEB_TRACE_FILE = arg.traceFile;
      },
      { vars: VARIANT.env(cond), traceFile },
    );
  const importer = createImporter({ win, outDir: out, timeoutMs, saveContent });
  /** 经导入队列采集一次，并补充正文的字符数、哈希与片段签名（用于 A/B 内容对照）。 */
  async function importOnce(url, traceFile, label = "") {
    const { content, ...result } = await importer(url, traceFile, label);
    return {
      ...result,
      chars: content.length,
      contentHash: content ? createHash("sha1").update(content).digest("hex").slice(0, 12) : null,
      shingles: content ? shingles(content) : null,
    };
  }

  console.log(
    `语料 ${urls.length} 个网址 × 条件 ${conditions.join("/")} × ${reps} 轮；代理模式 ${proxyMode}`,
  );
  // 预热：第一次采集会启动提取器进程并创建渲染会话，不计入统计。
  await setEnv("B", "");
  for (const url of ["https://example.com/", urls[0]]) {
    const warm = await importOnce(url, "");
    console.log(`预热 ${warm.status} ${sec(warm.wallMs)} ${new URL(url).host}`);
  }

  const records = [];
  for (let rep = 1; rep <= reps; rep++) {
    const order = rep % 2 === 1 ? conditions : [...conditions].reverse();
    for (const cond of order) {
      const traceFile = path.join(out, `trace-${cond}-r${rep}.jsonl`);
      await setEnv(cond, traceFile);
      console.log(`\n== 第 ${rep} 轮 / ${CONDITION_LABEL[cond]} ==`);
      for (const [index, url] of urls.entries()) {
        const { shingles: signature, ...result } = await importOnce(
          url,
          traceFile,
          `${cond}-r${rep}-${String(index + 1).padStart(2, "0")}`,
        );
        const record = { cond, rep, index, url, ...result };
        records.push({ ...record, shingles: signature });
        fs.appendFileSync(resultsFile, JSON.stringify(record) + "\n");
        const egress = result.trace?.egress;
        console.log(
          `[${cond} r${rep} ${String(index + 1).padStart(2)}/${urls.length}] ${result.status.padEnd(9)}` +
            ` 导入 ${sec(result.wallMs)} 采集 ${sec(result.trace?.ms?.total)}` +
            ` ${(result.trace?.engine ?? "-").padEnd(8)}` +
            ` 请求 ${egress?.requests ?? 0} 复用 ${egress?.reusedSocket ?? 0}` +
            (result.trace?.settle ? ` 沉降 ${result.trace.settle.reason}/${sec(result.trace.settle.initialMs)}` : "") +
            ` ${new URL(url).host}${new URL(url).pathname.slice(0, 40)}`,
        );
      }
    }
  }
  await setEnv("B", "");

  // ---- 汇总 ----
  const ok = (record) => record.status === "completed" && record.trace;
  const summary = { proxyMode, reps, urls: urls.length, conditions: {}, paired: null, similarity: null };
  for (const cond of conditions) {
    const own = records.filter((record) => record.cond === cond);
    const done = own.filter(ok);
    const rendered = done.filter((record) => record.trace.engine === "electron");
    const egressOf = (list) => list.map((record) => record.trace.egress).filter(Boolean);
    const requests = egressOf(done).reduce((total, egress) => total + egress.requests, 0);
    const reused = egressOf(done).reduce((total, egress) => total + egress.reusedSocket, 0);
    const heavy = egressOf(done).filter((egress) => egress.requests >= 30);
    const stageNames = new Set(rendered.flatMap((record) => Object.keys(record.trace.ms)));
    summary.conditions[cond] = {
      pages: own.length,
      completed: own.filter((record) => record.status === "completed").length,
      failed: own.filter((record) => record.status === "failed").map((record) => ({ url: record.url, error: record.error })),
      wallMedianMs: median(done.map((record) => record.wallMs)),
      captureMedianMs: median(done.map((record) => record.trace.ms.total)),
      renderedPages: rendered.length,
      renderedCaptureMedianMs: median(rendered.map((record) => record.trace.ms.total)),
      renderedStageMedianMs: Object.fromEntries(
        [...stageNames].map((name) => [name, median(rendered.map((record) => record.trace.ms[name] ?? 0))]),
      ),
      // 沉降等待：结束原因分布、首段用时与整个沉降阶段（含三次滚动）的分布。
      settleReasons: rendered.reduce((acc, record) => {
        const reason = record.trace.settle?.reason ?? "无记录";
        acc[reason] = (acc[reason] ?? 0) + 1;
        return acc;
      }, {}),
      settleInitialMedianMs: median(rendered.map((record) => record.trace.settle?.initialMs)),
      // 轮询自身的开销：读取往返与页面侧计算的最大值，以及首段超出上限 100 ms 以上的次数。
      settlePollMaxMs: Math.max(0, ...rendered.map((record) => record.trace.settle?.maxPollMs ?? 0)),
      settleCostMaxMs: Math.max(0, ...rendered.map((record) => record.trace.settle?.maxCostMs ?? 0)),
      settleOverCap: rendered.filter((record) => (record.trace.settle?.initialMs ?? 0) > 1300).length,
      settleStageP95Ms: percentile(rendered.map((record) => record.trace.ms.settle), 0.95),
      settleStageMaxMs: percentile(rendered.map((record) => record.trace.ms.settle), 1),
      // 被取消的跨源子框架：页数与个数。
      blockedFramesPages: rendered.filter((record) => record.trace.blockedFrames).length,
      blockedFramesTotal: rendered.reduce((total, record) => total + (record.trace.blockedFrames ?? 0), 0),
      // load 比 DOMContentLoaded 晚多少：是“以 DOMContentLoaded 作沉降起点”最多能省的时间上限。
      domReadyMedianMs: median(rendered.map((record) => record.trace.domReadyMs)),
      loadStageMedianMs: median(rendered.map((record) => record.trace.ms.load)),
      domReadyToLoad: (() => {
        const gaps = rendered
          .filter((record) => Number.isFinite(record.trace.domReadyMs))
          .map((record) => record.trace.ms.load - record.trace.domReadyMs);
        return { n: gaps.length, median: median(gaps), p90: percentile(gaps, 0.9), max: percentile(gaps, 1), over300: gaps.filter((value) => value > 300).length };
      })(),
      requests,
      reusedSocket: reused,
      reuseRate: requests ? reused / requests : null,
      reuseRateHeavyPages: heavy.length
        ? heavy.reduce((total, egress) => total + egress.reusedSocket, 0) /
          heavy.reduce((total, egress) => total + egress.requests, 0)
        : null,
      retried: egressOf(done).reduce((total, egress) => total + egress.retried, 0),
      failedRequests: egressOf(done).reduce((total, egress) => total + egress.failed, 0),
      failedRequestErrors: egressOf(done).reduce((acc, egress) => {
        for (const [label, count] of Object.entries(egress.errors ?? {})) acc[label] = (acc[label] ?? 0) + count;
        return acc;
      }, {}),
      failureProfile: profileFailures(done),
      typeProfile: profileTypes(done),
      freshRequestP50MedianMs: median(egressOf(done).map((egress) => egress.freshMs.p50).filter(Boolean)),
      reusedRequestP50MedianMs: median(egressOf(done).map((egress) => egress.reusedMs.p50).filter(Boolean)),
    };
  }

  if (conditions.includes("A") && conditions.includes("B")) {
    const key = (record) => `${record.rep}|${record.url}`;
    const byCond = (cond) => new Map(records.filter((record) => record.cond === cond && ok(record)).map((record) => [key(record), record]));
    const a = byCond("A");
    const b = byCond("B");
    const pairs = [...a.keys()].filter((id) => b.has(id)).map((id) => ({ a: a.get(id), b: b.get(id) }));
    const rendered = pairs.filter((pair) => pair.a.trace.engine === "electron" && pair.b.trace.engine === "electron");
    const delta = (list, pick) => list.map((pair) => pick(pair.b) - pick(pair.a));
    const captureOf = (record) => record.trace.ms.total;
    const faster = (list) => delta(list, captureOf).filter((value) => value < 0).length;
    summary.paired = {
      pairs: pairs.length,
      captureDeltaMedianMs: median(delta(pairs, captureOf)),
      renderedPairs: rendered.length,
      renderedCaptureDeltaMedianMs: median(delta(rendered, captureOf)),
      renderedBFaster: faster(rendered),
      renderedSettleDeltaMedianMs: median(delta(rendered, (record) => record.trace.ms.settle)),
      wallDeltaMedianMs: median(delta(pairs, (record) => record.wallMs)),
    };
    // “从不更慢”的验收：B 没能提前结束（cap/shell）的配对，沉降阶段本应与 A 相同，差值只应是读取开销与噪声。
    const capped = rendered.filter((pair) => ["cap", "shell"].includes(pair.b.trace.settle?.reason));
    const cappedDeltas = delta(capped, (record) => record.trace.ms.settle);
    summary.paired.cappedSettle = {
      pairs: capped.length,
      medianMs: median(cappedDeltas),
      p90Ms: percentile(cappedDeltas, 0.9),
      maxMs: percentile(cappedDeltas, 1),
      over100Ms: cappedDeltas.filter((value) => value > 100).length,
    };
    const cross = pairs.map((pair) => similarity(pair.a.shingles, pair.b.shingles)).filter((value) => value != null);
    // 逐对明细：用于定位“哪一页的哪一侧少了内容”，以及 B 的沉降结束原因。
    summary.pairDetails = pairs.map((pair) => ({
      url: pair.a.url,
      rep: pair.a.rep,
      aCaptureMs: pair.a.trace.ms.total,
      bCaptureMs: pair.b.trace.ms.total,
      aSettleMs: pair.a.trace.ms.settle ?? null,
      bSettleMs: pair.b.trace.ms.settle ?? null,
      bSettle: pair.b.trace.settle ?? null,
      aChars: pair.a.chars,
      bChars: pair.b.chars,
      similarity: similarity(pair.a.shingles, pair.b.shingles),
      recallAinB: containment(pair.a.shingles, pair.b.shingles),
    }));
    // 噪声基线：同条件跨轮次（需要至少 2 轮），反映网站自身的内容波动。
    const baseline = [];
    for (const cond of conditions) {
      const first = records.filter((record) => record.cond === cond && record.rep === 1 && ok(record));
      for (const left of first) {
        const right = records.find((record) => record.cond === cond && record.rep === 2 && record.url === left.url && ok(record));
        const value = right && similarity(left.shingles, right.shingles);
        if (value != null) baseline.push(value);
      }
    }
    // 召回：A 的内容有多少还在 B 里；B 多出的内容占 B 的比例（新增内容不一定是好事，如界面按钮文字）。
    const recallAinB = pairs.map((pair) => containment(pair.a.shingles, pair.b.shingles)).filter((value) => value != null);
    const extraInB = pairs
      .map((pair) => containment(pair.b.shingles, pair.a.shingles))
      .filter((value) => value != null)
      .map((value) => 1 - value);
    summary.similarity = {
      aVsB: range(cross),
      recallAinB: range(recallAinB),
      extraInB: { n: extraInB.length, median: median(extraInB), max: extraInB.length ? Math.max(...extraInB) : null, above002: extraInB.filter((value) => value > 0.02).length },
      sameConditionAcrossReps: range(baseline),
    };
  }

  // 与改动前基线逐页对照：基线取自先前一次运行 results.jsonl 中的 A 条件第 1 轮（同一批网址）。
  if (process.env.GUIZHI_TRACE_BASELINE) {
    const base = new Map();
    for (const line of fs.readFileSync(process.env.GUIZHI_TRACE_BASELINE, "utf8").split("\n").filter(Boolean)) {
      const row = JSON.parse(line);
      if (row.cond === "A" && row.rep === 1 && row.status === "completed" && row.contentHash) base.set(row.url, row);
    }
    summary.baseline = {};
    for (const cond of conditions) {
      const rows = records.filter((record) => record.cond === cond && ok(record) && base.has(record.url));
      const changed = rows
        .filter((record) => record.contentHash !== base.get(record.url).contentHash)
        .map((record) => ({ url: record.url, rep: record.rep, baselineChars: base.get(record.url).chars, chars: record.chars }));
      summary.baseline[cond] = { compared: rows.length, same: rows.length - changed.length, changed };
    }
  }

  fs.writeFileSync(
    path.join(out, "summary.json"),
    JSON.stringify({ ...summary, records: records.map(({ shingles, trace, ...rest }) => ({ ...rest, captureMs: trace?.ms?.total ?? null, engine: trace?.engine ?? null, route: trace?.route?.decision ?? null, settle: trace?.settle ?? null, settleMs: trace?.ms?.settle ?? null, requests: trace?.egress?.requests ?? null, reusedSocket: trace?.egress?.reusedSocket ?? null })) }, null, 2),
  );

  console.log("\n================ 汇总 ================");
  for (const cond of conditions) {
    const s = summary.conditions[cond];
    console.log(`${CONDITION_LABEL[cond]}：完成 ${s.completed}/${s.pages}，导入中位数 ${sec(s.wallMedianMs)}，采集中位数 ${sec(s.captureMedianMs)}`);
    console.log(`  渲染路径 ${s.renderedPages} 页：采集中位数 ${sec(s.renderedCaptureMedianMs)}；请求 ${s.requests}，复用 ${s.reusedSocket}（${pct(s.reuseRate)}），≥30 请求的页面复用率 ${pct(s.reuseRateHeavyPages)}，重试 ${s.retried}，失败请求 ${s.failedRequests} ${JSON.stringify(s.failedRequestErrors)}`);
    console.log(`  资源粗分：${Object.entries(s.typeProfile).map(([type, t]) => `${type} ${t.n} 个/${(t.bytes / 1048576).toFixed(1)}MiB`).join("，")}`);
    for (const [profile, count] of Object.entries(s.failureProfile).slice(0, 8))
      console.log(`  失败画像 ×${count}：${profile}`);
    console.log(`  请求耗时 p50 的页间中位数：新建 ${sec(s.freshRequestP50MedianMs)}，复用 ${sec(s.reusedRequestP50MedianMs)}`);
    console.log(`  渲染路径阶段中位数(ms)：${Object.entries(s.renderedStageMedianMs).map(([name, value]) => `${name}=${Math.round(value)}`).join(" ")}`);
    console.log(`  沉降等待：结束原因 ${JSON.stringify(s.settleReasons)}，首段中位数 ${sec(s.settleInitialMedianMs)}，整段 P95 ${sec(s.settleStageP95Ms)} 最大 ${sec(s.settleStageMaxMs)}`);
    console.log(`  沉降轮询开销：读取往返最大 ${s.settlePollMaxMs} ms，页面侧计算最大 ${s.settleCostMaxMs} ms，首段超出上限 100 ms 以上 ${s.settleOverCap} 次`);
    const gap = s.domReadyToLoad;
    console.log(`  DOMContentLoaded 中位数 ${sec(s.domReadyMedianMs)}，load 阶段中位数 ${sec(s.loadStageMedianMs)}；load 晚于 DOMContentLoaded：${gap.n} 页，中位数 ${sec(gap.median)} P90 ${sec(gap.p90)} 最大 ${sec(gap.max)}，超过 300 ms 的 ${gap.over300} 页`);
    console.log(`  被取消的跨源子框架：${s.blockedFramesPages} 页，共 ${s.blockedFramesTotal} 个`);
    for (const failure of s.failed) console.log(`  失败：${failure.url} ${String(failure.error).slice(0, 120)}`);
  }
  if (summary.paired) {
    const p = summary.paired;
    console.log(`\n配对差（B−A）：全部 ${p.pairs} 对，采集中位数 ${sec(p.captureDeltaMedianMs)}；渲染路径 ${p.renderedPairs} 对，采集中位数 ${sec(p.renderedCaptureDeltaMedianMs)}，B 更快 ${p.renderedBFaster}/${p.renderedPairs}，其中沉降阶段中位数 ${sec(p.renderedSettleDeltaMedianMs)}；导入墙钟中位数 ${sec(p.wallDeltaMedianMs)}`);
    const c = p.cappedSettle;
    console.log(`B 未能提前结束的配对（cap/shell）${c.pairs} 对：沉降阶段 B−A 中位数 ${c.medianMs} ms，p90 ${c.p90Ms} ms，最大 ${c.maxMs} ms，多于 100 ms 的 ${c.over100Ms} 对`);
    const s = summary.similarity;
    const fixed = (value) => value?.toFixed(3) ?? "—";
    console.log(`正文相似度 A vs B（双向取小）：n=${s.aVsB.n} 中位数 ${fixed(s.aVsB.median)} 最低 ${fixed(s.aVsB.min)} <0.98 的页数 ${s.aVsB.below098}`);
    console.log(`A 的内容在 B 中的召回：中位数 ${fixed(s.recallAinB.median)} 最低 ${fixed(s.recallAinB.min)} <0.98 的页数 ${s.recallAinB.below098}；B 相对 A 多出的内容占 B 的比例：中位数 ${fixed(s.extraInB.median)} 最大 ${fixed(s.extraInB.max)}，>2% 的页数 ${s.extraInB.above002}`);
    console.log(`噪声基线（同条件跨轮）：n=${s.sameConditionAcrossReps.n} 中位数 ${s.sameConditionAcrossReps.median?.toFixed(3) ?? "—"} 最低 ${s.sameConditionAcrossReps.min?.toFixed(3) ?? "—"} <0.98 的页数 ${s.sameConditionAcrossReps.below098}`);
    const suspicious = summary.pairDetails.filter((row) => (row.similarity ?? 1) < 0.98 || (row.recallAinB ?? 1) < 0.99);
    console.log(`\n内容有差异的配对（相似度<0.98 或召回<0.99）：${suspicious.length}/${summary.pairDetails.length}`);
    for (const row of suspicious)
      console.log(`  r${row.rep} ${row.url.replace("https://", "").slice(0, 60)} 相似度 ${fixed(row.similarity)} 召回 ${fixed(row.recallAinB)} 字符 A ${row.aChars} → B ${row.bChars}；B 沉降 ${row.bSettle?.reason ?? "-"}/${sec(row.bSettle?.initialMs)}（阶段 ${sec(row.bSettleMs)}），A 沉降阶段 ${sec(row.aSettleMs)}`);
  }
  for (const [cond, result] of Object.entries(summary.baseline ?? {})) {
    console.log(`\n与基线逐页对照（${CONDITION_LABEL[cond]}）：比较 ${result.compared} 次，正文哈希相同 ${result.same} 次，不同 ${result.changed.length} 次`);
    for (const row of result.changed)
      console.log(`  第 ${row.rep} 轮 ${row.url.replace("https://", "")}：${row.baselineChars} → ${row.chars} 字符（${row.chars - row.baselineChars >= 0 ? "+" : ""}${row.chars - row.baselineChars}）`);
  }
  console.log(`\n产物：${out}`);
}
