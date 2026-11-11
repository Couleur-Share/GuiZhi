// 差分测试桩：公众号采集的出站抓取改读 globalThis.__WX 里的夹具（真实实现带 SSRF 防护与网络，不适合离线录制）。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { randomUUID } from "node:crypto";

type Fixture = { pages: Record<string, { html: string; finalUrl?: string }>; images: Record<string, { b64?: string; status?: number }> };
const fx = () => (globalThis as unknown as { __WX: Fixture }).__WX;

export async function fetchHtml(url: string): Promise<{ finalUrl: string; html: string; contentType: string }> {
  const page = fx().pages[url];
  if (!page) throw new Error("HTTP 404");
  return { finalUrl: page.finalUrl ?? url, html: page.html, contentType: "text/html" };
}

export async function downloadToTempFile(url: string, options: { fileName: string }): Promise<{ dir: string; filePath: string }> {
  const img = fx().images[url];
  if (!img || img.status || img.b64 === undefined) throw new Error(`HTTP ${img?.status ?? 404}`);
  const dir = path.join(os.tmpdir(), `gz-wx-${randomUUID().slice(0, 8)}`);
  fs.mkdirSync(dir, { recursive: true });
  const filePath = path.join(dir, options.fileName);
  fs.writeFileSync(filePath, Buffer.from(img.b64, "base64"));
  return { dir, filePath };
}
