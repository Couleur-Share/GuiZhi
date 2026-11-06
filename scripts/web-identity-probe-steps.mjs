/**
 * pnpm shot 专用：网页采集的“请求身份”实测——静态抓取与渲染窗口对同一站点发出的请求头是否一致，
 * 以及按客户端特征拒绝请求的站点（缺少 User-Agent 就返回 403，如 Wikipedia）能否采集。
 * 不依赖公网与本机代理。
 * Request-identity probe for web capture: do the static fetch and the render window present the
 * same identity, and can a site that rejects UA-less clients (403, like Wikipedia) be captured?
 *
 * 做法：本机 HTTP 服务当作应用的手动代理，以虚构站点 identity.probe.example 提供三类页面，并逐条记录
 * 它收到的请求头（只记录与身份有关的头）：
 *   static  无脚本的短文，只走静态路径；
 *   render  含加载占位的页面，静态阶段取一次主文档，渲染窗口再取一次，再加载脚本；
 *   gated   没有 User-Agent 时返回 403，有则返回正文（Wikipedia 的行为）。
 *
 * 用法：pnpm shot --steps scripts/web-identity-probe-steps.mjs --out <新目录>
 *       GUIZHI_WEB_IDENTITY=none 同样运行一次，得到“静态请求不带身份”的基线对照。
 * 产物：<out>/identity-probe/results.json；控制台输出逐条请求头与裁决。
 */
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { createImporter } from "./lib/web-import.mjs";
import { ensureWebRuntime } from "./lib/web-runtime.mjs";

const HOST = "identity.probe.example";
const ORIGIN = `http://${HOST}`;
const FILLER = "归知网页采集请求身份探测正文，用于保证静态正文超过判定阈值。".repeat(24);
const SHOWN = ["user-agent", "accept", "accept-language", "upgrade-insecure-requests", "accept-encoding", "referer", "origin"];

const html = (title, body) =>
  `<!doctype html><html><head><meta charset="utf-8"><title>${title}</title></head><body>${body}</body></html>`;
const article = (name) => html(name, `<article><h1>${name}</h1><p>${FILLER}</p></article>`);
const RENDER_SCRIPT = `document.querySelector('main').innerHTML='<h1>渲染后的正文</h1><p>${FILLER}</p>'`;

const listen = (server) =>
  new Promise((resolve) => {
    server.once("error", (error) => resolve(error));
    server.listen(0, "127.0.0.1", () => resolve(null));
  });

export default async function ({ win, outDir, mainEntry }) {
  const out = path.join(outDir, "identity-probe");
  fs.mkdirSync(out, { recursive: true });
  const none = process.env.GUIZHI_WEB_IDENTITY === "none";
  /** @type {{ path: string, run: string, headers: Record<string, string>, names: string[] }[]} */
  const log = [];

  const server = http.createServer((request, response) => {
    let url;
    try {
      url = new URL(request.url);
    } catch {
      return response.writeHead(400).end();
    }
    if (url.host !== HOST) return response.writeHead(403).end();
    const headers = {};
    for (const name of SHOWN) if (request.headers[name] !== undefined) headers[name] = String(request.headers[name]);
    log.push({ path: url.pathname, run: url.searchParams.get("run") ?? "", headers, names: Object.keys(request.headers) });
    const send = (status, type, body) => {
      response.writeHead(status, { "content-type": type, "cache-control": "no-store" });
      response.end(body);
    };
    if (url.pathname === "/static") return send(200, "text/html; charset=utf-8", article("静态页"));
    if (url.pathname === "/render")
      return send(200, "text/html; charset=utf-8", html("渲染页", '<main><p>Loading...</p></main><script src="/app.js"></script>'));
    if (url.pathname === "/app.js") return send(200, "text/javascript; charset=utf-8", RENDER_SCRIPT);
    if (url.pathname === "/gated") {
      if (!request.headers["user-agent"]) return send(403, "text/html; charset=utf-8", "<html><body>Please set a user-agent</body></html>");
      return send(200, "text/html; charset=utf-8", article("按客户端特征放行的页面"));
    }
    send(404, "text/plain", "not found");
  });
  const listenError = await listen(server);
  if (listenError) throw new Error(`fixture 监听失败：${listenError.message}`);

  const results = [];
  try {
    ensureWebRuntime(mainEntry);
    await win.evaluate(
      (port) =>
        window.api.settings.set({
          networkProxy: { mode: "manual", protocol: "http", host: "127.0.0.1", port, username: "", password: "", bypass: "localhost,127.0.0.1" },
        }),
      server.address().port,
    );
    const runtime = await win.evaluate(() => window.api.webCapture.status());
    if (!runtime?.ok || !runtime.data?.available)
      throw new Error(`网页组件不可用：${runtime?.data?.reason ?? runtime?.error ?? "未知原因"}`);
    const importOnce = createImporter({ win, outDir: out, timeoutMs: 90_000 });

    let nonce = 0;
    for (const kind of ["static", "render", "gated"]) {
      const run = String(++nonce);
      const begin = log.length;
      const task = await importOnce(`${ORIGIN}/${kind}?run=${run}`, "", `identity-${kind}`);
      const requests = log.slice(begin).filter((entry) => entry.run === run || entry.path === "/app.js");
      results.push({ kind, status: task.status, error: task.error, requests });
      console.log(`\n== ${kind}  采集 ${task.status}${task.error ? "：" + String(task.error).slice(0, 80) : ""}`);
      for (const entry of requests) {
        console.log(`   ${entry.path}`);
        for (const [name, value] of Object.entries(entry.headers)) console.log(`     ${name}: ${value.length > 140 ? value.slice(0, 140) + "…" : value}`);
        console.log(`     （全部头名）${entry.names.join(", ")}`);
      }
    }
    fs.writeFileSync(path.join(out, "results.json"), JSON.stringify(results, null, 2));

    // ---- 裁决 ----
    const failures = [];
    const expect = (ok, message) => {
      if (!ok) failures.push(message);
    };
    const by = (kind) => results.find((row) => row.kind === kind);
    const documents = (kind) => by(kind).requests.filter((entry) => entry.path === `/${kind}`);
    expect(by("static")?.status === "completed", `static：应采集成功，实际 ${by("static")?.status} ${by("static")?.error ?? ""}`);
    expect(by("render")?.status === "completed", `render：应采集成功，实际 ${by("render")?.status} ${by("render")?.error ?? ""}`);
    expect(documents("render").length >= 2, `render：应先静态取一次、再由渲染窗口取一次主文档，实际 ${documents("render").length} 次`);
    const [staticDoc, renderDoc] = documents("render");
    expect(Boolean(renderDoc?.headers["user-agent"]), "render：渲染窗口的主文档请求应带 User-Agent");
    if (none) {
      expect(!documents("static")[0]?.headers["user-agent"], "基线：静态请求不应带 User-Agent");
      expect(by("gated")?.status === "failed", `基线 gated：缺少 User-Agent 应被拒绝并采集失败，实际 ${by("gated")?.status}`);
    } else {
      for (const name of ["user-agent", "accept"]) {
        expect(Boolean(staticDoc?.headers[name]), `静态请求应带 ${name}`);
        expect(staticDoc?.headers[name] === renderDoc?.headers[name], `静态与渲染的 ${name} 应一致：静态 ${staticDoc?.headers[name]}；渲染 ${renderDoc?.headers[name]}`);
      }
      // 渲染窗口经 protocol.handle 转发时本来就没有 Accept-Language（Electron 45 实测）；静态不补，两边才不会因语言协商得到不同版本。
      expect(!staticDoc?.headers["accept-language"] && !renderDoc?.headers["accept-language"], "静态与渲染都不应带 accept-language");
      expect(by("gated")?.status === "completed", `gated：带 User-Agent 后应采集成功，实际 ${by("gated")?.status} ${by("gated")?.error ?? ""}`);
    }

    console.log(`\n产物：${out}`);
    if (failures.length) {
      for (const failure of failures) console.log(`  不通过：${failure}`);
      throw new Error(`请求身份探测有 ${failures.length} 项不通过`);
    }
    console.log(none ? "基线探测符合预期（静态请求不带身份，UA 门禁站点被拒绝）" : "请求身份探测全部通过：静态与渲染的 UA/Accept 一致，UA 门禁站点可采集");
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(() => resolve()));
  }
}
