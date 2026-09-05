import { RelayDatabase, hash, secret } from "./database";
const db = new RelayDatabase(process.env.CAPTURE_DATABASE ?? "data/capture.db");
if (process.argv[2] === "stats") {
  const counts = db.all("SELECT state,count(*) AS submissions,sum(item_count) AS items FROM captures GROUP BY state");
  const oldest = db.get<{ oldest: number | null }>("SELECT min(created_at) AS oldest FROM captures WHERE state='accepted'")?.oldest;
  const delays = db.get("SELECT count(*) AS received,avg(received_at-created_at) AS meanDeliveryMs,max(received_at-created_at) AS maxDeliveryMs FROM captures WHERE received_at IS NOT NULL");
  const results = db.all("SELECT json_extract(j.value,'$.status') AS status,count(*) AS items FROM captures c,json_each(c.progress,'$.items') j GROUP BY status");
  process.stdout.write(JSON.stringify({ time: Date.now(), counts, delays, results, oldestPendingMs: oldest ? Date.now() - oldest : 0 }) + "\n");
} else if (process.argv[2] === "invite") {
  const invite = secret(); db.run("INSERT INTO invites(hash) VALUES(?)", hash(invite));
  // 仅此显式管理员命令输出一次邀请码；HTTP 服务不记录秘密。
  process.stdout.write(invite + "\n");
} else throw new Error("用法: node dist/admin.js invite|stats");
db.close();
