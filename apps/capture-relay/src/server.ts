import Fastify, { type FastifyRequest } from "fastify";
import cookie from "@fastify/cookie";
import staticFiles from "@fastify/static";
import { CaptureInputError } from "@guizhi/shared/utils/capture-submission";
import { CAPTURE_PROTOCOL_VERSION } from "@guizhi/shared/types/mobile-capture";
import { Accounts, type Principal } from "./accounts";
import { Captures, type Limits } from "./captures";
import { RelayDatabase, RelayError, assert, hash, identifier, token } from "./database";
export interface ServerOptions { database: string; origin: string; staticRoot?: string; trustProxy?: string[]; limits?: Limits }
export async function createServer(options: ServerOptions) {
  const origin = new URL(options.origin).origin;
  const app = Fastify({ logger: false, trustProxy: options.trustProxy ?? false, bodyLimit: 200000, requestTimeout: 15000 });
  const db = new RelayDatabase(options.database), accounts = new Accounts(db), captures = new Captures(db, options.limits);
  await app.register(cookie);
  const cookieName = "__Host-guizhi-capture";
  const cookieOptions = { path: "/", httpOnly: true, secure: true, sameSite: "strict" as const, maxAge: 365 * 86400 };
  function auth(req: FastifyRequest, kind?: Principal["kind"], pending = false) {
    const bearer = req.headers.authorization?.match(/^Bearer ([A-Za-z0-9_-]+)$/)?.[1];
    const p = accounts.authenticate(bearer ?? req.cookies[cookieName], pending);
    assert(!kind || p.kind === kind, "forbidden", 403);
    db.rate(`device:${p.id}`, 240);
    return p;
  }
  app.addHook("onRequest", async (req, reply) => {
    reply.header("Cache-Control", "no-store").header("X-Content-Type-Options", "nosniff").header("Referrer-Policy", "no-referrer")
      .header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'");
    if (!req.url.startsWith("/v1/")) return;
    // 不信任任意 X-Forwarded-For；仅运维显式列出的代理可提供客户端 IP。
    db.rate(`ip:${hash(req.ip)}`, 180);
    const source = req.headers.origin;
    if (source) assert(source === origin, "origin_denied", 403);
    if (req.cookies[cookieName] && !["GET", "HEAD"].includes(req.method)) {
      assert(source === origin && req.headers["x-guizhi-csrf"] === "1", "csrf_denied", 403);
    }
    if (!["GET", "HEAD"].includes(req.method)) {
      assert(req.headers["content-type"]?.startsWith("application/json"), "json_required", 415);
      assert(req.headers["x-guizhi-protocol"] === String(CAPTURE_PROTOCOL_VERSION), "protocol_mismatch", 426);
    }
  });
  app.setErrorHandler((error, _req, reply) => {
    // 不序列化请求、完整 URL、堆栈或 SQLite 参数。
    const code = error instanceof RelayError ? error.code : error instanceof CaptureInputError ? error.message : "request_failed";
    const status = error instanceof RelayError ? error.statusCode : error instanceof CaptureInputError ? 400 : ((error as { statusCode?: number }).statusCode === 413 ? 413 : 500);
    if (status === 429) reply.header("Retry-After", "60");
    reply.code(status).send({ success: false, error: code });
  });
  app.get("/healthz", async () => ({ ok: db.get<{ ok: number }>("SELECT 1 AS ok")?.ok === 1, protocol: CAPTURE_PROTOCOL_VERSION }));
  app.get("/v1/meta", async () => ({ protocol: CAPTURE_PROTOCOL_VERSION, serverTime: Date.now() }));
  app.post<{ Body: Record<string, unknown> }>("/v1/mailboxes", async req => {
    db.rate(`create:${hash(req.ip)}`, 5); return accounts.create(req.body);
  });
  app.delete("/v1/mailbox", async req => {
    const p = auth(req, "desktop"); db.run("UPDATE mailboxes SET active=0 WHERE id=?", p.mailboxId); return { success: true };
  });
  app.post<{ Body: { nonce: string } }>("/v1/pairings", async req => {
    const p = auth(req, "desktop"); db.rate(`pair:${p.id}`, 10); return accounts.pair(p.mailboxId, req.body.nonce);
  });
  app.post<{ Body: Record<string, unknown> }>("/v1/pairings/claim", async (req, reply) => {
    assert(req.headers.origin === origin && req.headers["x-guizhi-csrf"] === "1", "csrf_denied", 403);
    db.rate(`claim:${hash(req.ip)}`, 10);
    const result = accounts.claim(req.body);
    reply.setCookie(cookieName, token(req.body.credential), cookieOptions); return result;
  });
  app.get("/v1/pairings", async req => {
    const p = auth(req, "desktop");
    return db.all("SELECT p.id,p.expires_at AS expiresAt,p.device_id AS deviceId,d.name FROM pairings p LEFT JOIN devices d ON d.id=p.device_id WHERE p.mailbox_id=? AND p.confirmed=0 AND p.expires_at>?", p.mailboxId, Date.now());
  });
  app.post<{ Params: { id: string }; Body: { deviceId: string } }>("/v1/pairings/:id/confirm", async req => {
    const p = auth(req, "desktop"); return accounts.confirm(p.mailboxId, identifier(req.params.id), identifier(req.body.deviceId));
  });
  app.get("/v1/session", async req => {
    const p = auth(req, undefined, true); assert(p.kind === "phone", "forbidden", 403);
    return { paired: p.active === 1, deviceId: p.id };
  });
  app.get("/v1/devices", async req => {
    const p = auth(req, "desktop");
    return db.all("SELECT id,name,kind,active,parent_id AS parentId FROM devices WHERE mailbox_id=?", p.mailboxId);
  });
  app.delete<{ Params: { id: string } }>("/v1/devices/:id", async req => accounts.revoke(auth(req, "desktop").mailboxId, identifier(req.params.id)));
  app.post<{ Body: { credential: string } }>("/v1/shortcut", async req => accounts.shortcut(auth(req, "phone"), req.body.credential));
  app.delete("/v1/shortcut", async req => {
    const p = auth(req, "phone"); db.run("UPDATE devices SET active=0 WHERE parent_id=?", p.id); return { success: true };
  });
  app.post("/v1/captures", async (req, reply) => {
    const p = auth(req); assert(p.kind !== "desktop", "forbidden", 403); db.rate(`submit:${p.id}`, 30);
    return reply.code(201).send(captures.submit(p, req.body));
  });
  app.get<{ Querystring: { after?: string } }>("/v1/deliveries", async req => captures.deliveries(auth(req, "desktop").mailboxId, req.query.after ? identifier(req.query.after) : ""));
  app.post<{ Params: { id: string } }>("/v1/deliveries/:id/ack", async req => captures.acknowledge(auth(req, "desktop").mailboxId, identifier(req.params.id)));
  app.put<{ Params: { id: string } }>("/v1/deliveries/:id/progress", async req => captures.progress(auth(req, "desktop").mailboxId, identifier(req.params.id), req.body));
  app.get<{ Querystring: { before?: string } }>("/v1/history", async req => {
    const p = auth(req); assert(p.kind !== "desktop", "forbidden", 403);
    const before = req.query.before ? Number(req.query.before) : Date.now() + 1;
    assert(Number.isSafeInteger(before) && before > 0, "invalid_cursor"); return captures.history(p, before);
  });
  if (options.staticRoot) {
    await app.register(staticFiles, { root: options.staticRoot, index: "index.html" });
    app.setNotFoundHandler((req, reply) => {
      if (req.method === "GET" && req.url.split("?")[0] === "/share") return reply.sendFile("index.html");
      return reply.code(404).send({ error: "not_found" });
    });
  }
  db.cleanup();
  const cleanup = setInterval(() => { try { db.cleanup(); } catch { process.stderr.write("暂存清理失败\n"); } }, 60000);
  cleanup.unref();
  app.addHook("onClose", async () => { clearInterval(cleanup); db.close(); });
  return { app, db };
}
