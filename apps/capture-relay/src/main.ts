import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { createServer } from "./server";
import { DEFAULT_LIMITS } from "./captures";
const origin = process.env.CAPTURE_ORIGIN;
if (!origin || new URL(origin).protocol !== "https:") throw new Error("必须配置 HTTPS CAPTURE_ORIGIN");
const database = resolve(process.env.CAPTURE_DATABASE ?? "data/capture.db");
mkdirSync(dirname(database), { recursive: true, mode: 0o700 });
function limit(name: string, fallback: number) {
  const n = Number(process.env[name] ?? fallback);
  if (!Number.isSafeInteger(n) || n <= 0) throw new Error(`配置无效: ${name}`);
  return n;
}
const { app } = await createServer({ database, origin, staticRoot: resolve(process.env.CAPTURE_WEB_ROOT ?? "../capture-web/dist"),
  trustProxy: process.env.CAPTURE_TRUST_PROXY?.split(",").map(v => v.trim()).filter(Boolean),
  limits: { dailyItems: limit("CAPTURE_DAILY_ITEMS", DEFAULT_LIMITS.dailyItems), pendingItems: limit("CAPTURE_PENDING_ITEMS", DEFAULT_LIMITS.pendingItems),
    pendingBytes: limit("CAPTURE_PENDING_BYTES", DEFAULT_LIMITS.pendingBytes), retentionDays: limit("CAPTURE_RETENTION_DAYS", DEFAULT_LIMITS.retentionDays) } });
for (const signal of ["SIGTERM", "SIGINT"] as const) process.once(signal, () => { void app.close().then(() => process.exit(0)); });
await app.listen({ host: "0.0.0.0", port: Number(process.env.PORT ?? 8787) });
