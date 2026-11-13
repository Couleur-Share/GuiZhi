// NGA 线上 A/B 探针（TS 侧，产品原文件 nga.ts / safe-fetch.ts，在 Electron 的 Node 里运行）。
// 参数与 Go 侧 real_nga_probe_test.go 完全一致：只读第 1 页、楼主分页最多 19 页、不补拉父楼、不重试、
// 附件图最多真实请求 3 次。逐跳证据由对 http/https.request 的包装产生（能看到每一次重定向）。
// 用法：ELECTRON_RUN_AS_NODE=1 electron.exe nga-probe.cjs <摘要输出文件> <跟踪 JSONL 文件> <最小间隔毫秒>
import fs from "node:fs";
import http from "node:http";
import https from "node:https";
import { MEDIA_SIZE_LIMITS } from "../../../../apps/desktop/src/main/services/import/media-files";
import { fetchRawText, downloadToTempFile } from "../../../../apps/desktop/src/main/services/import/safe-fetch";
import { fetchNgaThread } from "../../../../apps/desktop/src/main/services/import/nga";

const [summaryPath, tracePath, intervalArg] = process.argv.slice(2);
if (!summaryPath || !tracePath) throw new Error("用法: nga-probe <摘要文件> <跟踪文件> [最小间隔毫秒]");
const minInterval = Math.max(0, Number(intervalArg) || 0);
const TOPIC = "37194262";

const secretKey = /token|key|secret|sig|auth|pass|session|cookie|ticket|code/i;
const maskUrl = (u: string) =>
  u.replace(/\?([^#]*)/, (_m, q: string) => "?" + q.split("&").map((p) => (secretKey.test(p.split("=")[0]) && p.includes("=") ? p.split("=")[0] + "=***" : p)).join("&"));
const maskCookie = (c: string) => "[names: " + c.split(";").map((s) => s.trim().split("=")[0]).filter(Boolean).sort().join(", ") + "]";

let seq = 0;
function trace(entry: Record<string, unknown>) {
  fs.appendFileSync(tracePath, JSON.stringify({ time: new Date().toISOString(), side: "ts", ...entry }) + "\n");
}

// 包装 http(s).request：产品的每一跳都会经过这里
const httpOriginal = http.request.bind(http) as (...a: unknown[]) => http.ClientRequest;
for (const mod of [http, https] as const) {
  const original = mod.request.bind(mod) as (...a: unknown[]) => http.ClientRequest;
  (mod as unknown as { request: unknown }).request = (...args: unknown[]) => {
    const options = args[0] as http.RequestOptions & { protocol?: string };
    const started = Date.now();
    const dryPort = process.env.GUIZHI_PROBE_DRY_PORT;
    const host = String(options.servername ?? options.headers?.Host ?? options.hostname ?? "");
    const url = `${options.protocol ?? "http:"}//${options.headers && (options.headers as Record<string, string>).Host ? (options.headers as Record<string, string>).Host : host}${options.path ?? ""}`;
    if (dryPort) {
      // 离线干跑：把 https 请求改成发往本机假服务器的 http 请求（仅用于验证探针本身，不访问任何真实站点）
      options.protocol = "http:"; options.hostname = "127.0.0.1"; options.port = Number(dryPort);
      options.servername = undefined; options.family = 4; options.agent = undefined;
    }
    const req = (dryPort ? httpOriginal : original)(...args);
    const hdrs: Record<string, string> = {};
    for (const [k, v] of Object.entries(options.headers ?? {})) hdrs[k] = k.toLowerCase() === "cookie" ? maskCookie(String(v)) : String(v);
    const mine = ++seq;
    let done = false;
    const finish = (extra: Record<string, unknown>) => {
      if (done) return;
      done = true;
      trace({ seq: mine, url: maskUrl(url), request: hdrs, keepAlive: true, elapsedMs: Date.now() - started, ...extra });
    };
    req.on("response", (res) =>
      finish({
        status: res.statusCode, location: res.headers.location ? maskUrl(String(res.headers.location)) : undefined, contentType: res.headers["content-type"],
        // 响应侧只记名字不记值（与 Go 侧 fetch_trace.go 一致）
        setCookieNames: (res.headers["set-cookie"] ?? []).map((c) => c.split("=")[0].trim()),
        server: res.headers.server, responseHeaders: Object.keys(res.headers).sort(),
      }),
    );
    req.on("error", (e) => finish({ status: 0, error: e.message }));
    return req;
  };
}

// 最小间隔：与 Go 侧 GUIZHI_FETCH_MIN_INTERVAL_MS 对应（TS 版在每次产品调用前等待；重定向各跳不单独限速）
let nextAt = 0;
async function pace() {
  if (minInterval <= 0) return;
  const now = Date.now();
  const at = Math.max(now, nextAt);
  nextAt = at + minInterval;
  if (at > now) await new Promise((r) => setTimeout(r, at - now));
}

let imageAttempts = 0, imageCapped = 0;

async function main() {
  const started = Date.now();
  const summary: Record<string, unknown> = { side: "ts" };
  try {
    const th = await fetchNgaThread(TOPIC, {
      retryDelaysMs: [],
      maxSummaryPages: 1,
      maxOpPages: 19,
      maxParentFetches: 0,
      imageLimit: 3,
      fetchRawText: async (url, options) => { await pace(); return fetchRawText(url, options); },
      downloadImage: async (url, signal) => {
        if (++imageAttempts > 3) { imageCapped++; throw new Error("探针封顶：不再发起真实图片请求"); }
        await pace();
        // 与产品 defaultDownloadImage 同参数（未导出，这里复刻调用点：UA、Referer、大小上限、文件名）
        const NGA_UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";
        return downloadToTempFile(url, { signal, userAgent: NGA_UA, referer: "https://bbs.nga.cn/", maxBytes: MEDIA_SIZE_LIMITS.image, fileName: "image.bin" });
      },
      saveImageAsset: async () => "probe.jpg",
    });
    Object.assign(summary, {
      title: th.title, author: th.author, replyCount: th.replyCount, contentChars: [...th.content].length,
      opReplies: th.replies.length, summaryReplies: th.summaryReplies?.length ?? 0, warning: th.warningReason ?? "",
    });
  } catch (e) {
    summary.error = e instanceof Error ? e.message : String(e);
  }
  Object.assign(summary, { elapsedMs: Date.now() - started, imageNetworkAttempts: Math.min(imageAttempts, 3), imageCapped });
  fs.writeFileSync(summaryPath, JSON.stringify(summary, null, 1));
}
main().catch((e) => { console.error(e); process.exit(1); });
