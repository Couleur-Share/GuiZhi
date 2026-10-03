/**
 * pnpm shot 专用：事件驱动沉降等待的受控夹具验收。不依赖公网与本机代理：
 * 本脚本起一个本机 HTTP 服务，把它当作应用的手动代理，并以虚构站点 settle.fixture.example 提供页面。
 *
 * 每个夹具都包含一段“晚到内容”（标记以 MARK- 开头），对比旧行为 A（GUIZHI_WEB_SETTLE=fixed）与
 * 新行为 B（事件驱动）能否采到它，以及沉降阶段用时。分两类：
 *   must   B 必须采到（网络驱动、DOM 变化可见、空壳保护等设计内保证的场景）；
 *          带 quick 的还要求 B 快速结束（在途的是图片/样式表/预取/子框架等不改动主框架 DOM 的请求，
 *          不应拖慢沉降）；脚本、XHR/fetch、字体、信标在协议处理器里无法与脚本区分，保守等待
 *   limit  已知限制（既不发请求也不改 DOM 的纯定时器渲染等），只记录 B 是否丢失，不作为失败
 *
 * 用法（仓库根目录；归知开发实例无需关闭，验证不会连接它）：
 *   pnpm shot --steps scripts/web-settle-fixtures-steps.mjs --out <新目录>
 * 环境变量：GUIZHI_FIXTURE_REPS 轮数，默认 2；奇数轮先 A 后 B，偶数轮相反。
 *          GUIZHI_FIXTURE_ONLY 只跑指定夹具（逗号分隔的 id），用于改动后的快速复验。
 * 产物：<out>/settle-fixtures/results.json 与 trace-<条件>-r<轮>.jsonl。任一硬性检查不通过则以错误退出。
 */
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { createImporter } from "./lib/web-import.mjs";
import { ensureWebRuntime } from "./lib/web-runtime.mjs";

const ORIGIN = "http://settle.fixture.example";
const CONDITIONS = {
  A: { label: "A 固定等待", settle: "fixed" },
  B: { label: "B 事件驱动", settle: "quiet" },
};
// 旧版固定等待总长；新行为任何夹具的沉降阶段都不应显著超过它（容差覆盖脚本往返与窗口显示）。
const LEGACY_SETTLE_MS = 1800;
const TOLERANCE_MS = 150;
// quick 夹具的首段上限：必须明显低于等满首段的 1200 ms。
const QUICK_MAX_MS = 600;

// 1×1 透明 PNG。
const PIXEL = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);
const sentence = (marker) => `验收标记段落 ${marker}：这一段文字用于判断页面晚到的内容是否被采集进正文。`;
const FILLER = "归知网页采集验收正文，用于保证可见文字超过空壳判定阈值。".repeat(12);

// 页面侧公共脚本：late(marker) 把晚到内容追加到正文末尾；api(marker, delayMs) 取一个延迟响应。
const COMMON = `
function late(m){var p=document.createElement('p');p.textContent='验收标记段落 '+m+'：这一段文字用于判断页面晚到的内容是否被采集进正文。';document.getElementById('late').appendChild(p)}
function api(m,d){return fetch('/api/x?d='+d+'&t='+m).then(function(r){return r.json()})}
`;
const doc = (title, content, script = "") =>
  `<!doctype html><html><head><meta charset="utf-8"><title>${title}</title></head><body>${content}${
    script ? `<script>${COMMON}${script}</script>` : ""
  }</body></html>`;
// <x-note> 是自定义元素标签：静态路由据此判定“需要渲染”，保证每个夹具都走 Electron 渲染路径。
const article = (title, body = "") =>
  `<article><h1>${title}</h1><p>${FILLER}</p><x-note></x-note>${body}<div id="late"></div></article>`;
const page = (title, { body = "", script = "" } = {}) => doc(title, article(title, body), script);
const APP =
  "function app(m){document.getElementById('app').innerHTML='<article><h1>应用页面</h1><p>验收标记段落 '+m+'：晚到内容。</p><p>'+'正文填充文字，保证可见文字超过阈值。'.repeat(12)+'</p></article>'}";
const shell = (title, script) =>
  `<!doctype html><html><head><meta charset="utf-8"><title>${title}</title></head><body><div id="app">加载中</div><script>${COMMON}${APP}${script}</script></body></html>`;
const card = (render) =>
  `customElements.define('x-card',class extends HTMLElement{connectedCallback(){var root=this.attachShadow({mode:'open'});root.innerHTML='<p>卡片加载中</p>';${render}}})`;

const FIXTURES = [
  {
    id: "ssr",
    kind: "must",
    marker: "MARK-ssr",
    note: "静态正文齐全，没有晚到内容",
    html: page("静态正文", { body: `<p>${sentence("MARK-ssr")}</p>` }),
  },
  {
    id: "dom300",
    kind: "must",
    marker: "MARK-dom300",
    note: "300 ms 后写入 DOM（定时器）",
    html: page("定时写入", { script: "setTimeout(function(){late('MARK-dom300')},300)" }),
  },
  {
    id: "xhr900",
    kind: "must",
    marker: "MARK-xhr900",
    note: "请求耗时 900 ms，返回后渲染",
    html: page("慢接口", { script: "api('MARK-xhr900',900).then(function(j){late(j.text)})" }),
  },
  {
    id: "chain",
    kind: "must",
    marker: "MARK-chain",
    note: "两个串联请求，间隔 100 ms（小于静止窗口）",
    html: page("请求链", {
      script:
        "api('a',200).then(function(){setTimeout(function(){api('MARK-chain',200).then(function(j){late(j.text)})},100)})",
    }),
  },
  {
    id: "hydrate",
    kind: "must",
    marker: "MARK-hydrate",
    note: "DOMContentLoaded 后经 rAF + 120 ms 激活",
    html: page("激活", {
      script:
        "document.addEventListener('DOMContentLoaded',function(){requestAnimationFrame(function(){setTimeout(function(){late('MARK-hydrate')},120)})})",
    }),
  },
  {
    id: "scrollLazy",
    kind: "must",
    marker: "MARK-scroll",
    note: "滚动到哨兵元素后才请求（IntersectionObserver）",
    html: page("滚动加载", {
      body: '<div style="height:1400px"></div><div id="sentinel">哨兵</div>',
      script:
        "new IntersectionObserver(function(es,o){if(es.some(function(e){return e.isIntersecting})){o.disconnect();api('MARK-scroll',150).then(function(j){late(j.text)})}}).observe(document.getElementById('sentinel'))",
    }),
  },
  {
    id: "shellXhr",
    kind: "must",
    marker: "MARK-shellxhr",
    note: "空壳页，接口 600 ms 后渲染",
    html: shell("空壳接口", "api('MARK-shellxhr',600).then(function(j){app(j.text)})"),
  },
  {
    id: "shellSilent",
    kind: "must",
    marker: "MARK-shellsilent",
    note: "空壳页，静默定时器 900 ms 后渲染（靠空壳保护等满）",
    html: shell("空壳定时", "setTimeout(function(){app('MARK-shellsilent')},900)"),
  },
  {
    id: "shadow",
    kind: "must",
    marker: "MARK-shadow",
    note: "web component 在 shadow root 内，接口 300 ms 后渲染",
    html: page("组件", {
      body: "<x-card></x-card>",
      script: card("api('MARK-shadow',300).then(function(j){root.innerHTML='<p>'+j.text+'</p>'})"),
    }),
  },
  {
    id: "poll",
    kind: "must",
    marker: "MARK-poll",
    note: "对抗：每 100 ms 轮询一次，网络永不静止",
    html: page("轮询", { script: "late('MARK-poll');setInterval(function(){api('ping',20)},100)" }),
  },
  {
    id: "animate",
    kind: "must",
    marker: "MARK-animate",
    note: "对抗：每 40 ms 改一次 DOM，永不静止",
    html: page("动画", {
      script:
        "late('MARK-animate');var n=0,el=document.createElement('span');document.getElementById('late').appendChild(el);setInterval(function(){el.textContent='tick '+(n++)},40)",
    }),
  },
  {
    id: "big",
    kind: "must",
    marker: "MARK-big",
    note: "大页面：8000 个段落，探针读取开销",
    html: page("大页面", {
      body:
        `<p>${sentence("MARK-big")}</p>` +
        Array.from({ length: 8000 }, (_, index) => `<p>第 ${index} 段：${"内容".repeat(8)}</p>`).join(""),
    }),
  },
  // ---- 在途但不会改动主框架 DOM 的请求（按 Accept 分类）：不应拖慢沉降 ----
  {
    id: "imageHang",
    kind: "must",
    quick: true,
    marker: "MARK-imagehang",
    note: "load 后插入一张 4 s 才返回的图片，不应阻止提前结束",
    html: page("慢图片", {
      script:
        "addEventListener('load',function(){setTimeout(function(){var i=new Image();i.src='/api/img?d=4000';document.body.appendChild(i);late('MARK-imagehang')},50)})",
    }),
  },
  {
    id: "styleHang",
    kind: "must",
    quick: true,
    marker: "MARK-stylehang",
    note: "load 后插入一个 4 s 才返回的样式表，不应阻止提前结束",
    html: page("慢样式", {
      script:
        "addEventListener('load',function(){setTimeout(function(){var l=document.createElement('link');l.rel='stylesheet';l.href='/api/css?d=4000';document.head.appendChild(l);late('MARK-stylehang')},50)})",
    }),
  },
  {
    id: "prefetchHang",
    kind: "must",
    quick: true,
    marker: "MARK-prefetchhang",
    note: "load 后加入 <link rel=prefetch>，目标 4 s 才返回（预取不执行，不应阻止提前结束）",
    html: page("慢预取", {
      script:
        "addEventListener('load',function(){setTimeout(function(){var l=document.createElement('link');l.rel='prefetch';l.href='/api/x?d=4000&t=p';document.head.appendChild(l);late('MARK-prefetchhang')},50)})",
    }),
  },
  {
    id: "iframeHang",
    kind: "must",
    quick: true,
    marker: "MARK-iframehang",
    note: "load 后插入一个 4 s 才返回的 iframe（子框架内容不在主文档快照里，不应阻止提前结束）",
    html: page("慢子框架", {
      script:
        "addEventListener('load',function(){setTimeout(function(){var f=document.createElement('iframe');f.src='/api/frame?d=4000';document.body.appendChild(f);late('MARK-iframehang')},50)})",
    }),
  },
  // ---- 看起来像文档、实为数据/导航的请求：必须等 ----
  {
    id: "ajaxHtml",
    kind: "must",
    marker: "MARK-ajaxhtml",
    note: "jQuery 式 XHR（Accept: text/html, */*）取 800 ms 的 HTML 片段再插入：不能被当作文档而忽略",
    html: page("取片段", {
      script:
        "var x=new XMLHttpRequest();x.open('GET','/api/frag?d=800');x.setRequestHeader('Accept','text/html, */*; q=0.01');x.onload=function(){document.getElementById('late').insertAdjacentHTML('beforeend',x.responseText)};x.send()",
    }),
  },
  {
    id: "jsRedirect",
    kind: "must",
    marker: "MARK-jsredirect",
    note: "脚本在 150 ms 后跳转到响应 700 ms 的新页面：导航期间沉降轮询会被顺延，必须采到新页面正文（回归哨兵）",
    html: page("脚本跳转", { script: "setTimeout(function(){location.replace('/redirected')},150)" }),
  },
  {
    id: "xhrHang",
    kind: "must",
    marker: "MARK-xhrhang",
    note: "load 后发一个 4 s 才返回的 fetch：无法判断与正文无关，B 等满首段（不快速退出，但不超过旧版）",
    html: page("慢请求", {
      script:
        "addEventListener('load',function(){setTimeout(function(){fetch('/api/x?d=4000&t=a').catch(function(){});late('MARK-xhrhang')},50)})",
    }),
  },
  {
    id: "timer700",
    kind: "limit",
    marker: "MARK-timer700",
    note: "纯定时器：700 ms 后才写入，之前既无请求也无 DOM 变化（预期 B 可能丢失）",
    html: page("长定时", { script: "setTimeout(function(){late('MARK-timer700')},700)" }),
  },
  {
    id: "shadowTimer500",
    kind: "limit",
    marker: "MARK-shadowtimer",
    note: "shadow root 内 500 ms 定时渲染：DOM 观察器与可见文字都看不到（预期 B 可能丢失）",
    html: page("组件定时", {
      body: "<x-card></x-card>",
      script: card("setTimeout(function(){root.innerHTML='<p>'+'MARK-shadowtimer'+'</p>'},500)"),
    }),
  },
];

// 带延迟返回的辅助路径：[内容类型, 内容, 默认延迟 ms]；查询参数 d 可覆盖延迟。
const DELAYED = {
  "/api/img": ["image/png", PIXEL, 0],
  "/api/css": ["text/css", "p{}", 0],
  "/api/frame": ["text/html; charset=utf-8", "<p>子框架内容</p>", 0],
  "/api/frag": ["text/html; charset=utf-8", `<p>${sentence("MARK-ajaxhtml")}</p>`, 0],
  "/redirected": ["text/html; charset=utf-8", page("跳转目标", { body: `<p>${sentence("MARK-jsredirect")}</p>` }), 700],
};

const sec = (ms) => (ms == null ? "—" : `${(ms / 1000).toFixed(2)}s`);
const median = (values) => {
  const sorted = values.filter(Number.isFinite).sort((a, b) => a - b);
  if (!sorted.length) return null;
  const mid = sorted.length >> 1;
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
};

export default async function ({ win, app, outDir, mainEntry }) {
  const reps = Math.max(1, Number.parseInt(process.env.GUIZHI_FIXTURE_REPS ?? "", 10) || 2);
  // 只跑指定夹具（逗号分隔的 id），用于改动后的快速复验；服务端仍提供全部夹具页面。
  const only = (process.env.GUIZHI_FIXTURE_ONLY ?? "").split(",").map((item) => item.trim()).filter(Boolean);
  const selected = only.length ? FIXTURES.filter((item) => only.includes(item.id)) : FIXTURES;
  if (selected.length !== (only.length || FIXTURES.length)) throw new Error(`GUIZHI_FIXTURE_ONLY 含未知夹具：${only.join(",")}`);
  const out = path.join(outDir, "settle-fixtures");
  fs.mkdirSync(out, { recursive: true });

  const server = http.createServer((request, response) => {
    let url;
    try {
      url = new URL(request.url);
    } catch {
      return response.writeHead(400).end();
    }
    if (url.origin !== ORIGIN) return response.writeHead(403).end();
    // 延迟返回的辅助资源：制造“在途但与正文无关”或“必须等待”的请求。
    const delayed = DELAYED[url.pathname];
    if (delayed) {
      const [type, body, fallbackDelay] = delayed;
      setTimeout(() => {
        response.writeHead(200, { "content-type": type, "cache-control": "no-store" });
        response.end(body);
      }, Number(url.searchParams.get("d") ?? fallbackDelay));
      return;
    }
    if (url.pathname === "/api/x") {
      const delay = Number(url.searchParams.get("d") || 0);
      setTimeout(() => {
        response.writeHead(200, { "content-type": "application/json", "cache-control": "no-store" });
        response.end(JSON.stringify({ text: url.searchParams.get("t") || "" }));
      }, delay);
      return;
    }
    const fixture = FIXTURES.find((item) => `/${item.id}` === url.pathname);
    if (!fixture) return response.writeHead(404).end();
    response.writeHead(200, { "content-type": "text/html; charset=utf-8", "cache-control": "no-store" });
    response.end(fixture.html);
  });
  server.on("connect", (_request, socket) => socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"));
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));

  try {
    ensureWebRuntime(mainEntry);
    await win.evaluate(
      (port) =>
        window.api.settings.set({
          networkProxy: {
            mode: "manual",
            protocol: "http",
            host: "127.0.0.1",
            port,
            username: "",
            password: "",
            bypass: "localhost,127.0.0.1",
          },
        }),
      server.address().port,
    );
    const runtime = await win.evaluate(() => window.api.webCapture.status());
    if (!runtime?.ok || !runtime.data?.available)
      throw new Error(`网页组件不可用：${runtime?.data?.reason ?? runtime?.error ?? "未知原因"}`);

    const importOnce = createImporter({ win, outDir: out, timeoutMs: 120_000, saveContent: false });
    const setEnv = (cond, traceFile) =>
      app.evaluate(
        (_electron, arg) => {
          process.env.GUIZHI_WEB_SETTLE = arg.settle;
          process.env.GUIZHI_WEB_CONNECTION_REUSE = "1";
          // 沉降夹具要验证“在途的图片请求不阻止提前结束”；默认的占位图本地应答下图片根本不出网，
          // 这类夹具会失去意义，所以恢复真实加载（占位图本身由 web-inert-probe-steps.mjs 回归）。
          process.env.GUIZHI_WEB_INERT_RESOURCES = "load";
          process.env.GUIZHI_WEB_TRACE_FILE = arg.traceFile;
        },
        { settle: CONDITIONS[cond].settle, traceFile },
      );

    // 预热：第一次采集会启动提取器进程并创建渲染会话，不计入统计。
    await setEnv("B", "");
    const warm = await importOnce(`${ORIGIN}/ssr?warm=1`, "");
    console.log(`预热 ${warm.status} ${sec(warm.wallMs)}`);
    if (warm.status !== "completed") throw new Error(`预热失败：${warm.error}`);

    const results = [];
    for (let rep = 1; rep <= reps; rep++) {
      const order = rep % 2 === 1 ? ["A", "B"] : ["B", "A"];
      for (const cond of order) {
        const traceFile = path.join(out, `trace-${cond}-r${rep}.jsonl`);
        await setEnv(cond, traceFile);
        console.log(`\n== 第 ${rep} 轮 / ${CONDITIONS[cond].label} ==`);
        for (const fixture of selected) {
          const { content, ...result } = await importOnce(`${ORIGIN}/${fixture.id}?c=${cond}&r=${rep}`, traceFile);
          const row = {
            fixture: fixture.id,
            kind: fixture.kind,
            cond,
            rep,
            status: result.status,
            error: result.error,
            engine: result.trace?.engine ?? null,
            found: content.includes(fixture.marker),
            chars: content.length,
            settle: result.trace?.settle ?? null,
            settleMs: result.trace?.ms?.settle ?? null,
            captureMs: result.trace?.ms?.total ?? null,
          };
          results.push(row);
          console.log(
            `[${cond} r${rep}] ${fixture.id.padEnd(15)} ${row.status.padEnd(9)} ${(row.engine ?? "-").padEnd(8)}` +
              ` 标记 ${row.found ? "采到" : "丢失"} 沉降阶段 ${sec(row.settleMs)}` +
              (row.settle ? ` (${row.settle.reason} 首段 ${sec(row.settle.initialMs)})` : ""),
          );
        }
      }
    }
    await setEnv("B", "");
    fs.writeFileSync(path.join(out, "results.json"), JSON.stringify(results, null, 2));

    // ---- 检查 ----
    const failures = [];
    const rowsOf = (id, cond) => results.filter((row) => row.fixture === id && row.cond === cond);
    for (const row of results) {
      if (row.status !== "completed") failures.push(`${row.fixture}/${row.cond}/r${row.rep} 未完成：${row.error}`);
      else if (row.engine !== "electron") failures.push(`${row.fixture}/${row.cond}/r${row.rep} 没有走渲染路径（engine=${row.engine}），夹具无效`);
    }
    for (const fixture of selected) {
      // 夹具有效性：旧行为必须能采到晚到内容，否则这个夹具说明不了任何问题。
      for (const row of rowsOf(fixture.id, "A"))
        if (!row.found) failures.push(`${fixture.id}/A/r${row.rep}：旧行为也没采到 ${fixture.marker}，夹具无效`);
      if (fixture.kind === "must")
        for (const row of rowsOf(fixture.id, "B"))
          if (!row.found) failures.push(`${fixture.id}/B/r${row.rep}：新行为丢失 ${fixture.marker}（${fixture.note}）`);
    }
    for (const row of results.filter((item) => item.cond === "B"))
      if (row.settleMs != null && row.settleMs > LEGACY_SETTLE_MS + TOLERANCE_MS)
        failures.push(`${row.fixture}/B/r${row.rep}：沉降阶段 ${sec(row.settleMs)} 超过旧版 ${sec(LEGACY_SETTLE_MS)}`);
    for (const row of rowsOf("ssr", "B")) {
      if (row.settle?.reason !== "quiet" || row.settle.initialMs > 450)
        failures.push(`ssr/B/r${row.rep}：静态页应在约 300 ms 判定静止，实际 ${row.settle?.reason}/${row.settle?.initialMs}ms`);
      if (row.settleMs > 900) failures.push(`ssr/B/r${row.rep}：静态页沉降阶段应不超过 0.9 s，实际 ${sec(row.settleMs)}`);
    }
    // 标记了 quick 的夹具：B 应在静止窗口后很快结束，而不是被无关的在途请求拖到上限。
    // 页面在 load 后 50 ms 才改动 DOM，所以静止最早在约 400 ms；上限给 600 ms，远低于等满的 1200 ms。
    for (const fixture of selected.filter((item) => item.quick))
      for (const row of rowsOf(fixture.id, "B"))
        if (row.settle?.reason !== "quiet" || row.settle.initialMs > QUICK_MAX_MS)
          failures.push(`${fixture.id}/B/r${row.rep}：应快速结束，实际 ${row.settle?.reason}/${row.settle?.initialMs}ms（${fixture.note}）`);
    for (const id of ["poll", "animate"])
      for (const row of rowsOf(id, "B"))
        if (row.settle?.reason !== "cap") failures.push(`${id}/B/r${row.rep}：持续活动的页面应等满首段（cap），实际 ${row.settle?.reason}`);
    for (const row of rowsOf("shellSilent", "B"))
      if (row.settle?.initialMs < 1000) failures.push(`shellSilent/B/r${row.rep}：空壳页不应提前结束，首段仅 ${row.settle?.initialMs}ms`);

    console.log("\n================ 汇总（沉降阶段中位数，秒）================");
    console.log("夹具              类别   A 固定    B 事件驱动   节省    B 结束原因              B 采到/轮数");
    for (const fixture of selected) {
      const a = median(rowsOf(fixture.id, "A").map((row) => row.settleMs));
      const bRows = rowsOf(fixture.id, "B");
      const b = median(bRows.map((row) => row.settleMs));
      const reasons = bRows.map((row) => `${row.settle?.reason ?? "-"}/${sec(row.settle?.initialMs)}`).join(",");
      const found = bRows.filter((row) => row.found).length;
      console.log(
        `${fixture.id.padEnd(16)} ${fixture.kind.padEnd(6)} ${sec(a).padEnd(9)} ${sec(b).padEnd(12)} ${sec(a - b).padEnd(7)} ${reasons.padEnd(24)} ${found}/${bRows.length}  ${fixture.note}`,
      );
    }
    const limitLost = selected.filter((fixture) => fixture.kind === "limit").flatMap((fixture) =>
      rowsOf(fixture.id, "B").filter((row) => !row.found).map(() => fixture.id),
    );
    console.log(`\n已知限制场景中 B 丢失标记的次数：${limitLost.length}${limitLost.length ? `（${[...new Set(limitLost)].join("、")}）` : ""}`);
    console.log(`\n产物：${out}`);
    if (failures.length) {
      for (const failure of failures) console.log(`  不通过：${failure}`);
      throw new Error(`夹具验收有 ${failures.length} 项不通过`);
    }
    console.log("夹具验收全部通过（must 场景 B 均采到晚到内容，持续活动页等满首段，静态页约 0.3 s 结束）");
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}
