// 以 stdio 方式驱动一个 MCP server 进程：逐条发送 JSON-RPC 行，收集每条请求的响应（按 id 对应）。
// 用法：tsx mcp-probe.mts <data-dir> <requests.json>   —— 起 TS 版 server 并打印响应数组
import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));

export type Rpc = Record<string, unknown> | string;

/** 发送 requests（对象会 JSON.stringify，字符串原样作为一行），等到所有带 id 的请求都收到响应后返回全部输出行。 */
export async function probe(command: string, args: string[], env: Record<string, string>, requests: Rpc[], cwd?: string): Promise<{ lines: unknown[]; stderr: string }> {
  const child = spawn(command, args, { env: { ...process.env, ...env }, cwd, stdio: ["pipe", "pipe", "pipe"], windowsHide: true });
  let stderr = "";
  child.stderr.on("data", (d) => (stderr += d.toString()));
  const lines: unknown[] = [];
  let buf = "";
  const waitingIds = new Set<unknown>();
  for (const r of requests) if (typeof r === "object" && r && "id" in r) waitingIds.add((r as { id: unknown }).id);
  const done = new Promise<void>((resolve) => {
    const check = () => { if (waitingIds.size === 0) resolve(); };
    child.stdout.on("data", (d) => {
      buf += d.toString();
      let nl;
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl).replace(/\r$/, "");
        buf = buf.slice(nl + 1);
        if (!line) continue;
        let msg: unknown;
        try { msg = JSON.parse(line); } catch { msg = { __raw: line }; }
        lines.push(msg);
        if (msg && typeof msg === "object" && "id" in (msg as object)) waitingIds.delete((msg as { id: unknown }).id);
        check();
      }
    });
    child.on("exit", () => resolve());
    setTimeout(resolve, 30000);
  });
  for (const r of requests) {
    child.stdin.write((typeof r === "string" ? r : JSON.stringify(r)) + "\n");
    await new Promise((r2) => setTimeout(r2, 30));
  }
  await done;
  await new Promise((r) => setTimeout(r, 200)); // 给迟到的输出一点时间
  child.stdin.end();
  child.kill();
  return { lines, stderr };
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  const [dataDir, requestsFile, outFile] = process.argv.slice(2);
  const requests = JSON.parse(fs.readFileSync(requestsFile, "utf8")) as Rpc[];
  const serverTs = path.resolve(here, "../../apps/desktop/src/mcp/server.ts");
  const tsx = path.resolve(here, "../../node_modules/.bin/tsx.CMD");
  const result = await probe(process.platform === "win32" ? "cmd.exe" : tsx, process.platform === "win32" ? ["/c", tsx, serverTs] : [serverTs], { GUIZHI_DATA_DIR: dataDir }, requests);
  fs.writeFileSync(outFile, JSON.stringify(result, null, 1), "utf8");
}
