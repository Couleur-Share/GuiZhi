// NGA 线上 A/B 探针编排：dry = 离线干跑（只验证 TS 探针与报告本身，请求发往本机假服务器）；
// real = 真实访问 bbs.nga.cn / img.nga.cn（Go 与 TS 各跑一次，请求封顶，间隔 ≥1s，不带真实账号 Cookie）。
//   用法（仓库根目录）：tsx apps/desktop-go/verify/nga-ab/nga-ab.mts <dry|real> <输出目录> [--order go,ts|ts,go]
// real 模式每侧约 24 次请求（第 1 页含 guestJs 握手 2 次、楼主分页最多 19 次、附件图 3 次），合计约 50 次。
import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../../../..");
const [mode, outDir, ...rest] = process.argv.slice(2);
if (!["dry", "real"].includes(mode) || !outDir) throw new Error("用法: nga-ab.mts <dry|real> <输出目录> [--order go,ts]");
const order = (rest[rest.indexOf("--order") + 1] ?? "go,ts").split(",");
fs.mkdirSync(outDir, { recursive: true });
const p = (n: string) => path.join(outDir, n);

// ---- 离线假 NGA ----
function fakeNga(): Promise<http.Server> {
  const store = (o: unknown) => "window.script_muti_get_var_store=" + JSON.stringify(o);
  const post = (lou: number, withImg = false) => ({
    pid: 1000 + lou, lou, authorid: 7, content: `楼层 ${lou} 的正文${withImg ? " [img]./mon_202601/01/x" + lou + ".jpg[/img]" : ""}`,
    postdatetimestamp: 1760000000 + lou,
  });
  const srv = http.createServer((req, res) => {
    const u = new URL(req.url ?? "/", "http://x");
    if (u.pathname === "/read.php") {
      if (!(req.headers.cookie ?? "").includes("guestJs=")) { res.writeHead(403, { "Content-Type": "text/html" }); res.end("<script>document.cookie='guestJs=GJ123; path=/'</script>"); return; }
      const page = Number(u.searchParams.get("page") ?? 1), op = u.searchParams.has("authorid");
      const n = op ? (page <= 2 ? 20 : 5) : 20;
      const posts = Array.from({ length: n }, (_, i) => post((page - 1) * 20 + i, i < 4));
      res.writeHead(200, { "Content-Type": "text/javascript; charset=utf-8" });
      res.end(store({ data: { __T: { tid: 1, fid: 1, subject: "干跑帖子", author: "楼主", authorid: 7, replies: 99, postdate: 1760000000 }, __ROWS: 100, __R: posts, __U: { 7: { username: "楼主" } }, __GLOBAL: { _ATTACH_BASE_VIEW: "img.nga.cn/attachments" } } }));
      return;
    }
    if (u.pathname.endsWith("x3.jpg") || u.pathname.endsWith("x2.jpg")) { res.writeHead(567); res.end(); return; }
    res.writeHead(200, { "Content-Type": "image/png" });
    res.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAHnOcQAAAAABJRU5ErkJggg==", "base64"));
  });
  return new Promise((r) => srv.listen(0, "127.0.0.1", () => r(srv)));
}

// ---- 打包 TS 探针（产品原文件；electron 换桩；干跑时目标校验仍走真实 net-safety，仅做 DNS 解析）----
const bundle = p("nga-probe.cjs");
await build({
  entryPoints: [path.join(here, "nga-probe.ts")], outfile: bundle, bundle: true, platform: "node", format: "cjs", target: "node22",
  alias: { electron: path.join(here, "electron-stub.cjs") }, logLevel: "error",
});

const electron = path.join(repo, "apps/desktop/node_modules/electron/dist/electron.exe");
function runProc(cmd: string, args: string[], env: Record<string, string>, cwd: string | undefined, logFile: string) {
  return new Promise<number>((resolve) => {
    const out = fs.createWriteStream(logFile);
    const c = spawn(cmd, args, { env: { ...process.env, ...env }, cwd });
    c.stdout.pipe(out, { end: false }); c.stderr.pipe(out, { end: false });
    c.on("close", (code) => { out.end(); resolve(code ?? -1); });
  });
}

let fake: http.Server | undefined;
const extraTs: Record<string, string> = {};
if (mode === "dry") { fake = await fakeNga(); extraTs.GUIZHI_PROBE_DRY_PORT = String((fake.address() as { port: number }).port); }

for (const f of ["trace-go.jsonl", "trace-ts.jsonl", "summary-go.json", "summary-ts.json"]) fs.rmSync(p(f), { force: true });
for (const [i, side] of order.entries()) {
  if (i > 0 && mode === "real") await new Promise((r) => setTimeout(r, 10_000));
  if (side === "go") {
    if (mode === "dry") { console.log("干跑：Go 侧只有真实站点测试，跳过"); continue; }
    const code = await runProc("go", ["test", "-count=1", "-v", "-run", "^TestRealNgaProbe$", "."], {
      GUIZHI_REAL_NET: "1", GUIZHI_REAL_NGA_PROBE: p("summary-go.json"), GUIZHI_FETCH_TRACE: p("trace-go.jsonl"), GUIZHI_FETCH_MIN_INTERVAL_MS: "1000",
    }, path.join(repo, "apps/desktop-go"), p("go.log"));
    console.log("Go 探针退出码", code);
  } else {
    const code = await runProc(electron, [bundle, p("summary-ts.json"), p("trace-ts.jsonl"), mode === "dry" ? "0" : "1000"], { ELECTRON_RUN_AS_NODE: "1", ...extraTs }, undefined, p("ts.log"));
    console.log("TS 探针退出码", code);
  }
}
fake?.close();

// ---- 报告 ----
interface Row { side: string; seq?: number; hop?: number; url: string; request: Record<string, string>; status?: number; location?: string; contentType?: string; elapsedMs?: number; error?: string; time: string }
const read = (n: string): Row[] => (fs.existsSync(p(n)) ? fs.readFileSync(p(n), "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);
const lines: string[] = [];
for (const side of ["go", "ts"]) {
  const rows = read(`trace-${side}.jsonl`).map((r) => ({ ...r, side }));
  const sum = fs.existsSync(p(`summary-${side}.json`)) ? JSON.parse(fs.readFileSync(p(`summary-${side}.json`), "utf8")) : null;
  lines.push(`== ${side}：${rows.length} 次请求（含重定向各跳） ==`);
  const by = new Map<string, Map<string, number>>();
  for (const r of rows) {
    const host = new URL(r.url).host.replace(/:\d+$/, "");
    const key = r.error ? `错误(${r.error})` : String(r.status);
    const m = by.get(host) ?? new Map(); m.set(key, (m.get(key) ?? 0) + 1); by.set(host, m);
  }
  for (const [h, m] of by) lines.push(`  ${h}：` + [...m].map(([k, v]) => `${k} ×${v}`).join("，"));
  const redirects = rows.filter((r) => r.status && r.status >= 300 && r.status < 400);
  if (redirects.length) {
    lines.push(`  重定向 ${redirects.length} 跳（前 8 条）：`);
    redirects.slice(0, 8).forEach((r) => lines.push(`    ${r.status} ${r.url} → ${r.location}`));
  }
  const firstBad = rows.findIndex((r) => r.error || (r.status && r.status >= 400 && r.status !== 403));
  lines.push(firstBad >= 0 ? `  第一次异常在第 ${firstBad + 1} 次请求：${rows[firstBad].url} → ${rows[firstBad].error ?? rows[firstBad].status}` : "  无异常请求");
  lines.push(`  摘要：${sum ? JSON.stringify(sum) : "未取得"}`, "");
}
const text = lines.join("\n");
fs.writeFileSync(p("ab-report.txt"), text);
console.log(text);
