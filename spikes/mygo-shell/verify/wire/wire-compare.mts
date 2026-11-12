// 离线对比 Go 版与 TS 版抓取层「真正发到线上的字节」：请求头顺序与 Connection、TLS ClientHello（JA3）。
// 所有请求只发往本机 127.0.0.1 的捕获服务器，不访问任何真实站点。
//   用法（仓库根目录）：pnpm dlx tsx spikes/mygo-shell/verify/wire/wire-compare.mts [输出目录]
// TS 客户端分别在 Electron（ELECTRON_RUN_AS_NODE，与产品主进程同一套 Node/TLS）与本机 Node 下各跑 3 次，
// Go 客户端跑产品的 NGA 请求路径（wire_client_test.go）3 次；BoringSSL 会随机化扩展顺序，所以多次采样。
import { spawn, spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../../../..");
const outDir = process.argv[2] ?? path.join(os.tmpdir(), "guizhi-verify", "wire");
fs.mkdirSync(outDir, { recursive: true });

interface Captured { label: string; kind: "http" | "tls"; text?: string; ja3?: Ja3; hex?: string }
interface Ja3 { ja3: string; hash: string; extensions: number[]; sni?: string; alpn?: string[]; ciphers: number }
const captured: Captured[] = [];
let label = "";

const isGrease = (v: number) => (v & 0x0f0f) === 0x0a0a;

function parseClientHello(buf: Buffer): Ja3 | null {
  try {
    if (buf[0] !== 0x16 || buf[5] !== 0x01) return null;
    let p = 5 + 4;
    const version = buf.readUInt16BE(p); p += 2 + 32;
    p += 1 + buf[p];
    const csLen = buf.readUInt16BE(p); p += 2;
    const ciphers: number[] = [];
    for (let i = 0; i < csLen; i += 2) ciphers.push(buf.readUInt16BE(p + i));
    p += csLen;
    p += 1 + buf[p];
    const extTotal = buf.readUInt16BE(p); p += 2;
    const end = p + extTotal;
    const exts: number[] = [];
    let curves: number[] = [], points: number[] = [], sni: string | undefined, alpn: string[] | undefined;
    while (p + 4 <= end) {
      const type = buf.readUInt16BE(p), len = buf.readUInt16BE(p + 2);
      const body = buf.subarray(p + 4, p + 4 + len);
      exts.push(type);
      if (type === 10) { const n = body.readUInt16BE(0); for (let i = 0; i < n; i += 2) curves.push(body.readUInt16BE(2 + i)); }
      if (type === 11) { for (let i = 0; i < body[0]; i++) points.push(body[1 + i]); }
      if (type === 0) sni = body.subarray(5).toString("latin1");
      if (type === 16) { alpn = []; let q = 2; while (q < body.length) { alpn.push(body.subarray(q + 1, q + 1 + body[q]).toString("latin1")); q += 1 + body[q]; } }
      p += 4 + len;
    }
    const f = (a: number[]) => a.filter((x) => !isGrease(x)).join("-");
    const ja3 = [version, f(ciphers), f(exts), f(curves), points.join("-")].join(",");
    return { ja3, hash: crypto.createHash("md5").update(ja3).digest("hex"), extensions: exts.filter((x) => !isGrease(x)), sni, alpn, ciphers: ciphers.length };
  } catch { return null; }
}

function listen(handler: (s: net.Socket) => void): Promise<net.Server> {
  return new Promise((resolve) => { const s = net.createServer(handler); s.listen(0, "127.0.0.1", () => resolve(s)); });
}
const portOf = (s: net.Server) => (s.address() as net.AddressInfo).port;

const plainSrv = await listen((sock) => {
  let acc = Buffer.alloc(0);
  sock.on("data", (d) => {
    acc = Buffer.concat([acc, d]);
    const i = acc.indexOf("\r\n\r\n");
    if (i < 0) return;
    captured.push({ label, kind: "http", text: acc.subarray(0, i).toString("latin1") });
    sock.end("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok");
    acc = Buffer.alloc(0);
  });
  sock.on("error", () => undefined);
});
const tlsSrv = await listen((sock) => {
  sock.once("data", (d) => {
    const h = parseClientHello(d);
    if (h) captured.push({ label, kind: "tls", ja3: h, hex: d.toString("hex") });
    sock.destroy();
  });
  sock.on("error", () => undefined);
});
const plain = portOf(plainSrv), tls = portOf(tlsSrv);

// 打包 TS 客户端（目标校验与代理换成桩）
const bundle = path.join(outDir, "ts-client.cjs");
await build({
  entryPoints: [path.join(here, "ts-client.ts")], outfile: bundle, bundle: true, platform: "node", format: "cjs", target: "node22",
  plugins: [{
    name: "stubs",
    setup(b) {
      b.onResolve({ filter: /\/net-safety$/ }, () => ({ path: path.join(here, "stub-net-safety.ts") }));
      b.onResolve({ filter: /\/network-proxy$/ }, () => ({ path: path.join(here, "stub-network-proxy.ts") }));
    },
  }],
  logLevel: "error",
});

const electron = path.join(repo, "apps/desktop/node_modules/electron/dist/electron.exe");
const RUNS = 3;
// 必须异步启动子进程：捕获服务器与本脚本同一事件循环，同步等待会让服务器无法应答
const run = (cmd: string, args: string[], env: Record<string, string>, cwd?: string) =>
  new Promise<void>((resolve, reject) => {
    const p = spawn(cmd, args, { env: { ...process.env, ...env }, cwd });
    let out = "";
    p.stdout.on("data", (d) => (out += d));
    p.stderr.on("data", (d) => (out += d));
    p.on("close", (code) => (code === 0 ? resolve() : reject(new Error(`${label} 失败（退出码 ${code}）：\n${out}`))));
  });
const electronVer = spawnSync(electron, ["-e", "console.log(process.versions.node, process.versions.electron)"], { env: { ...process.env, ELECTRON_RUN_AS_NODE: "1" }, encoding: "utf8" }).stdout.trim();

for (let i = 0; i < RUNS; i++) {
  label = "go";
  await run("go", ["test", "-count=1", "-run", "^TestWireClient$", "."], { GUIZHI_WIRE_PLAIN: String(plain), GUIZHI_WIRE_TLS: String(tls) }, path.join(repo, "spikes/mygo-shell"));
  label = "ts-electron";
  await run(electron, [bundle, String(plain), String(tls)], { ELECTRON_RUN_AS_NODE: "1" });
  label = "ts-node";
  await run(process.execPath, [bundle, String(plain), String(tls)], {});
}
plainSrv.close(); tlsSrv.close();

// ---- 报告 ----
const lines: string[] = [`Electron 内置 Node/Electron 版本：${electronVer}；本机 Node：${process.versions.node}`, ""];
const headerView = (t: string) => t.split("\r\n").map((l, i) => (i === 0 ? l.replace(/:\d+/g, ":PORT") : l.replace(/Host: ([^:]+):\d+/, "Host: $1:PORT")));
for (const lab of ["go", "ts-electron", "ts-node"]) {
  const https = captured.filter((c) => c.label === lab && c.kind === "http");
  lines.push(`== ${lab}：明文请求头（实际字节，按发送顺序）==`);
  // 每次运行 2 个请求（页面、图片）；只展示第一次运行，后续运行的差异另行标注
  const firstRun = https.slice(0, 2);
  firstRun.forEach((c, i) => { lines.push(`-- 请求 ${i + 1}（${i === 0 ? "页面" : "图片"}）`); headerView(c.text!).forEach((l) => lines.push("  " + l)); });
  const sig = (c: Captured) => headerView(c.text!).join("|");
  const stable = https.every((c, i) => sig(c) === sig(https[i % 2]));
  lines.push(`  各次运行是否一致：${stable ? "是" : "否"}`, "");
}
for (const lab of ["go", "ts-electron", "ts-node"]) {
  const hs = captured.filter((c) => c.label === lab && c.kind === "tls").map((c) => c.ja3!);
  lines.push(`== ${lab}：TLS ClientHello ==`);
  hs.forEach((h, i) => lines.push(`  第 ${i + 1} 次：JA3=${h.hash} 密码套件 ${h.ciphers} 个，扩展 ${h.extensions.length} 个，SNI=${h.sni}，ALPN=${h.alpn?.join(",") ?? "无"}`));
  lines.push(`  JA3 串（第 1 次）：${hs[0]?.ja3 ?? "未捕获"}`);
  lines.push(`  扩展顺序（第 1 次）：${hs[0]?.extensions.join(",") ?? "未捕获"}`);
  lines.push(`  JA3 是否各次相同：${new Set(hs.map((h) => h.hash)).size === 1 ? "是" : "否（扩展顺序随机化）"}`, "");
}
const text = lines.join("\n");
fs.writeFileSync(path.join(outDir, "wire-report.txt"), text);
// 原始 ClientHello（十六进制）供 utls 复刻实验使用：每个客户端各留一份
for (const lab of ["go", "ts-electron", "ts-node"]) {
  const h = captured.find((c) => c.label === lab && c.kind === "tls");
  if (h?.hex) fs.writeFileSync(path.join(outDir, `clienthello-${lab}.hex`), h.hex);
}
fs.writeFileSync(path.join(outDir, "wire-raw.json"), JSON.stringify(captured, null, 1));
console.log(text);
