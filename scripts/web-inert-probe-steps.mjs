/**
 * pnpm shot 专用：渲染窗口“惰性图片”（img、picture、CSS 背景图）以占位图本地应答的实测。
 * 同一批页面在 GUIZHI_WEB_INERT_RESOURCES=load（真实加载）与 stub（占位图）下各采集一次并对照。
 * 不依赖公网与本机代理。
 * Probe for inert images in the render window: the same pages are captured once with real image
 * loading and once with local placeholder answers, then compared.
 *
 * 做法：本机 HTTP 服务当作应用的手动代理，以虚构站点 inert.probe.example 提供页面、
 * cdn.probe.example 提供图片，逐条记录收到的请求（区分渲染窗口发出的与导入流程另行下载的）：
 *   slow     6 张图片各自被服务端扣住 2.5 s：真实加载下 load 被拖到 2.5 s 之后，占位图下不应受影响；
 *   onerror  图片 404，onerror 处理器把 src 改写成占位图：占位图下 src 应保持原样（真实加载下被改写）；
 *   cors     <img crossorigin>：占位图应答必须带 CORS 头，否则页面会收到 onerror；
 *   coep     页面开启 COEP require-corp：占位图应答必须带 CORP 头；
 *   lazy     data-src 懒加载与 loading="lazy"；
 *   media    picture/srcset 与 CSS 背景图；
 *   many     300 张图片：真实加载会超出 200 个请求的页面预算而失败，占位图下应成功；
 *   scroll   无限滚动（哨兵进入视口就加载下一批，每批加载完重新观察哨兵；图片未标注尺寸，每批带文字）；
 *   scrollbare  同上，但每批只有图片、没有文字——占位图是 1×1，页面塌缩，哨兵更久地留在视口内，
 *            会比真实加载多取几批。这是已知差异：两种模式都必须采集成功，批次数只打印、不作为通过条件，
 *            用来衡量差异有多大（批次以服务端收到的 /api/more 请求数计，/api 每次应答延迟 80 ms，
 *            模拟真实网络往返；提取结果会丢弃短段落，数不出条目）。
 *
 * 用法：pnpm shot --steps scripts/web-inert-probe-steps.mjs --out <新目录>
 * 产物：<out>/inert-probe/results.json、content/*.md；控制台输出逐场景对照与裁决。
 */
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { createImporter } from "./lib/web-import.mjs";

const PAGE_HOST = "inert.probe.example";
const CDN_HOST = "cdn.probe.example";
const CDN = `http://${CDN_HOST}`;
const HOLD_MS = 2500;
const FILLER = "归知网页采集惰性图片探测正文，用于保证渲染后的正文超过判定阈值。".repeat(24);
// 1×1 PNG。
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);

const html = (title, body) =>
  `<!doctype html><html><head><meta charset="utf-8"><title>${title}</title></head><body>${body}</body></html>`;
// 页面载入后写入正文；onload 之后再写，保证能读到图片的最终状态与最终 src。
const script = (name) => `
window.addEventListener('load', function () {
  setTimeout(function () {
    var img = document.images[0];
    var state = document.body.getAttribute('data-img') || 'none';
    var src = img ? img.getAttribute('src') : 'none';
    document.querySelector('main').innerHTML =
      '<h1>${name}</h1><p>img-state=' + state + '</p><p>src-final=' + src + '</p><p>${FILLER}</p>';
  }, 50);
});`;

const imgs = (urls, attrs = "") => urls.map((url) => `<img ${attrs} src="${url}" alt="">`).join("");
const numbered = (prefix, count) => Array.from({ length: count }, (_, i) => `${CDN}/${prefix}/${i}.png`);

// 无限滚动页：哨兵进入视口就加载下一批（每批 5 张未标注尺寸的 600×400 图片，最多 40 批），
// 每批加载完重新观察哨兵——页面若没把它推出视口就继续加载（常见写法）。
// 真实图片把页面撑高、哨兵很快离开视口；占位图是 1×1，页面塌缩，哨兵更久地留在视口内。
const feedBody = (appendItem, name) => `<main><p>Loading...</p></main><div id="list"></div><div id="sentinel" style="height:1px"></div>
  <script>
    var page = 0, busy = false;
    var observer = new IntersectionObserver(function (entries) { if (entries[0].isIntersecting) more(); });
    function more() {
      if (busy || page >= 40) return;
      busy = true;
      var n = ++page;
      fetch('/api/more?page=' + n).then(function (r) { return r.json(); }).then(function (items) {
        var list = document.getElementById('list');
        items.forEach(function (src, i) { ${appendItem} });
        busy = false;
        var sentinel = document.getElementById('sentinel');
        observer.unobserve(sentinel);
        observer.observe(sentinel);
      });
    }
    observer.observe(document.getElementById('sentinel'));
  </script>
  <script src="/app.js?n=${name}"></script>`;

/** 各场景的页面与额外响应头。 */
const PAGES = {
  slow: { body: `<main><p>Loading...</p></main>${imgs(numbered("slow", 6))}<script src="/app.js?n=slow"></script>` },
  onerror: {
    body: `<main><p>Loading...</p></main>${imgs(numbered("missing", 2), `onerror="this.onerror=null;this.src='/fallback.png'"`)}<script src="/app.js?n=onerror"></script>`,
  },
  cors: {
    body: `<main><p>Loading...</p></main><img crossorigin="anonymous" src="${CDN}/c.png" alt="" onload="document.body.setAttribute('data-img','loaded')" onerror="document.body.setAttribute('data-img','failed')"><script src="/app.js?n=cors"></script>`,
  },
  coep: {
    headers: { "cross-origin-embedder-policy": "require-corp", "cross-origin-opener-policy": "same-origin" },
    body: `<main><p>Loading...</p></main><img src="${CDN}/c.png" alt="" onload="document.body.setAttribute('data-img','loaded')" onerror="document.body.setAttribute('data-img','failed')"><script src="/app.js?n=coep"></script>`,
  },
  lazy: {
    body: `<main><p>Loading...</p></main>
      <img data-src="${CDN}/lazy/a.png" alt="" class="lazy"><img loading="lazy" src="${CDN}/lazy/b.png" alt="">
      <script>new IntersectionObserver(function (es) { es.forEach(function (e) { if (e.isIntersecting) { e.target.src = e.target.dataset.src; } }); }).observe(document.querySelector('.lazy'));</script>
      <script src="/app.js?n=lazy"></script>`,
  },
  media: {
    body: `<main><p>Loading...</p></main>
      <style>.hero{width:40px;height:40px;background:url(${CDN}/media/bg.png)}</style><div class="hero"></div>
      <picture><source srcset="${CDN}/media/p.webp" type="image/webp"><img src="${CDN}/media/p.png" srcset="${CDN}/media/p.png 1x, ${CDN}/media/p2.png 2x" alt=""></picture>
      <script src="/app.js?n=media"></script>`,
  },
  many: { body: `<main><p>Loading...</p></main>${imgs(numbered("many", 300))}<script src="/app.js?n=many"></script>` },
  scroll: {
    body: feedBody(
      "var img = document.createElement('img'); img.src = src; list.appendChild(img);" +
        "var p = document.createElement('p'); p.textContent = 'ITEM-' + n + '-' + i; list.appendChild(p);",
      "scroll",
    ),
  },
  scrollbare: {
    body: feedBody("var img = document.createElement('img'); img.src = src; list.appendChild(img);", "scrollbare"),
  },
};

const SVG = Buffer.from(
  '<svg xmlns="http://www.w3.org/2000/svg" width="600" height="400"><rect width="600" height="400" fill="#ddd"/></svg>',
);

const listen = (server) =>
  new Promise((resolve) => {
    server.once("error", (error) => resolve(error));
    server.listen(0, "127.0.0.1", () => resolve(null));
  });

const imageUrlsIn = (content) =>
  [...new Set(content.match(/http:\/\/cdn\.probe\.example\/[^\s)"'\]]+/g) ?? [])].sort();
const field = (content, name) => content.match(new RegExp(`${name}=([^\\s]+)`))?.[1] ?? "-";

export default async function ({ win, app, outDir }) {
  const out = path.join(outDir, "inert-probe");
  fs.mkdirSync(out, { recursive: true });
  /** @type {{ host: string, path: string, render: boolean, accept: string }[]} */
  const log = [];

  const server = http.createServer((request, response) => {
    let url;
    try {
      url = new URL(request.url);
    } catch {
      return response.writeHead(400).end();
    }
    const render = /Electron\//.test(String(request.headers["user-agent"] ?? ""));
    log.push({ host: url.host, path: url.pathname, render, accept: String(request.headers.accept ?? "") });
    const send = (status, type, body, extra = {}) => {
      response.writeHead(status, { "content-type": type, "cache-control": "no-store", ...extra });
      response.end(body);
    };
    if (url.host === CDN_HOST) {
      const cors = { "access-control-allow-origin": "*", "cross-origin-resource-policy": "cross-origin" };
      if (url.pathname.startsWith("/missing/")) return send(404, "text/plain", "missing");
      if (url.pathname.startsWith("/slow/")) return void setTimeout(() => send(200, "image/png", PNG, cors), HOLD_MS);
      if (url.pathname.startsWith("/feed/")) return send(200, "image/svg+xml", SVG, cors);
      return send(200, "image/png", PNG, cors);
    }
    if (url.host !== PAGE_HOST) return send(403, "text/plain", "forbidden");
    if (url.pathname === "/api/more") {
      const batch = url.searchParams.get("page") ?? "0";
      const items = JSON.stringify(Array.from({ length: 5 }, (_, i) => `${CDN}/feed/${batch}-${i}.svg`));
      // 延迟 80 ms，模拟真实网络往返；本机零延迟会让占位图下的“跑批”显得比真实更快。
      return void setTimeout(() => send(200, "application/json", items), 80);
    }
    if (url.pathname === "/app.js") return send(200, "text/javascript; charset=utf-8", script(url.searchParams.get("n") ?? ""));
    if (url.pathname === "/fallback.png") return send(200, "image/png", PNG);
    const page = PAGES[url.pathname.slice(1)];
    if (!page) return send(404, "text/plain", "not found");
    return send(200, "text/html; charset=utf-8", html(url.pathname, page.body), page.headers ?? {});
  });
  const listenError = await listen(server);
  if (listenError) throw new Error(`fixture 监听失败：${listenError.message}`);

  const results = [];
  try {
    await win.evaluate(
      (port) =>
        window.api.settings.set({
          networkProxy: { mode: "manual", protocol: "http", host: "127.0.0.1", port, username: "", password: "", bypass: "localhost,127.0.0.1" },
        }),
      server.address().port,
    );
    const traceFile = path.join(out, "trace.jsonl");
    const importOnce = createImporter({ win, outDir: out, timeoutMs: 120_000, saveContent: true });
    const setMode = (mode) =>
      app.evaluate(
        (_electron, arg) => {
          process.env.GUIZHI_WEB_INERT_RESOURCES = arg.mode;
          process.env.GUIZHI_WEB_TRACE_FILE = arg.traceFile;
        },
        { mode, traceFile },
      );

    // 预热：第一次采集要启动 Python 提取进程（实测约 14 s），与图片无关，不计入对照。
    await setMode("stub");
    await importOnce(`http://${PAGE_HOST}/media?run=warmup`, traceFile, "warmup");

    let nonce = 0;
    for (const scenario of Object.keys(PAGES)) {
      for (const mode of ["load", "stub"]) {
        await setMode(mode);
        const run = String(++nonce);
        const begin = log.length;
        const task = await importOnce(`http://${PAGE_HOST}/${scenario}?run=${run}`, traceFile, `${scenario}-${mode}`);
        const seen = log.slice(begin);
        const cdn = seen.filter((entry) => entry.host === CDN_HOST);
        const row = {
          scenario,
          mode,
          status: task.status,
          error: task.error,
          wallMs: task.wallMs,
          loadMs: task.trace?.ms?.load ?? null,
          totalMs: task.trace?.ms?.total ?? null,
          inertStubs: task.trace?.inertStubs ?? 0,
          cdnFromRender: cdn.filter((entry) => entry.render).length,
          cdnFromOthers: cdn.filter((entry) => !entry.render).length,
          imageUrls: imageUrlsIn(task.content),
          imgState: field(task.content, "img-state"),
          srcFinal: field(task.content, "src-final"),
          batches: seen.filter((entry) => entry.host === PAGE_HOST && entry.path === "/api/more").length,
        };
        results.push(row);
        console.log(
          `[${scenario.padEnd(7)} ${mode.padEnd(4)}] ${task.status.padEnd(9)} load ${row.loadMs ?? "-"}ms 总 ${row.totalMs ?? "-"}ms 占位应答 ${row.inertStubs} ` +
            `CDN请求(渲染窗口 ${row.cdnFromRender} / 其他 ${row.cdnFromOthers}) 图片网址 ${row.imageUrls.length} img-state=${row.imgState} src-final=${row.srcFinal}` +
            (scenario.startsWith("scroll") ? ` 已取批次 ${row.batches}` : "") +
            (task.error ? `\n    错误：${String(task.error).slice(0, 160)}` : ""),
        );
      }
    }
    fs.writeFileSync(path.join(out, "results.json"), JSON.stringify(results, null, 2));

    // ---- 裁决 ----
    const failures = [];
    const expect = (ok, message) => {
      if (!ok) failures.push(message);
    };
    const get = (scenario, mode) => results.find((row) => row.scenario === scenario && row.mode === mode);
    for (const scenario of Object.keys(PAGES)) {
      const stub = get(scenario, "stub");
      expect(stub.status === "completed", `${scenario}（占位图）：应采集成功，实际 ${stub.status} ${stub.error ?? ""}`);
      expect(stub.cdnFromRender === 0, `${scenario}（占位图）：渲染窗口不应向图片站发请求，实际 ${stub.cdnFromRender}`);
      // lazy 的图片是否被请求取决于视口与观察器，不对数量作要求；其余场景的图片都是立即加载的。
      const eager = scenario !== "lazy";
      expect(!eager || stub.inertStubs > 0, `${scenario}（占位图）：应有占位应答被记录`);
      if (scenario !== "many") {
        const real = get(scenario, "load");
        expect(real.status === "completed", `${scenario}（真实加载）：对照组应采集成功，实际 ${real.status} ${real.error ?? ""}`);
        expect(real.inertStubs === 0, `${scenario}（真实加载）：不应有占位应答`);
        expect(!eager || real.cdnFromRender > 0, `${scenario}（真实加载）：渲染窗口应向图片站发请求`);
      }
    }
    const slowReal = get("slow", "load");
    const slowStub = get("slow", "stub");
    expect(slowReal.loadMs >= HOLD_MS, `slow（真实加载）：load 应被图片拖到 ${HOLD_MS}ms 之后，实际 ${slowReal.loadMs}ms`);
    expect(slowStub.loadMs <= slowReal.loadMs - 1500, `slow：占位图应让 load 至少提前 1.5 s，真实 ${slowReal.loadMs}ms，占位 ${slowStub.loadMs}ms`);
    expect(
      JSON.stringify(slowStub.imageUrls) === JSON.stringify(slowReal.imageUrls),
      `slow：两种模式采集到的图片网址应一致：真实 ${slowReal.imageUrls.length} 个，占位 ${slowStub.imageUrls.length} 个`,
    );
    expect(get("onerror", "stub").srcFinal.includes("missing"), `onerror（占位图）：src 应保持原样，实际 ${get("onerror", "stub").srcFinal}`);
    expect(get("onerror", "load").srcFinal.includes("fallback"), `onerror（真实加载）：对照组 src 应被改写成占位图，实际 ${get("onerror", "load").srcFinal}`);
    for (const scenario of ["cors", "coep"]) {
      expect(get(scenario, "load").imgState === "loaded", `${scenario}（真实加载）：对照组图片应加载成功，实际 ${get(scenario, "load").imgState}`);
      expect(get(scenario, "stub").imgState === "loaded", `${scenario}（占位图）：图片应触发 onload 而不是 onerror，实际 ${get(scenario, "stub").imgState}`);
    }
    expect(get("many", "load").status === "failed" && /预算/.test(String(get("many", "load").error)), `many（真实加载）：对照组应因请求预算失败，实际 ${get("many", "load").status} ${get("many", "load").error ?? ""}`);
    expect(get("many", "stub").inertStubs >= 300, `many（占位图）：应应答全部 300 个图片请求，实际 ${get("many", "stub").inertStubs}`);

    console.log(`\n产物：${out}`);
    if (failures.length) {
      for (const failure of failures) console.log(`  不通过：${failure}`);
      throw new Error(`惰性图片探测有 ${failures.length} 项不通过`);
    }
    console.log("惰性图片探测全部通过：占位图让 load 不再被图片拖住，CORS/COEP/onerror 行为符合预期，超出请求预算的图片页可采集");
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(() => resolve()));
  }
}
