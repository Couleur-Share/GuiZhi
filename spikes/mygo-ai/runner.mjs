// 对拍运行器：TS 原版与 Go 移植共用。通过真实 preload 的 aiApi（requestStream / request / cancel）发请求，
// 记录与平台无关的结果；块数、耗时只作参考，不参与比较。
// 在 Node 里 import 使用，也被原样注入到 MyGo 页面里执行（因此不要用 Node 专有 API）。
export async function runAll(scenarios, ai, base) {
  const fnv = (s) => {
    let h = 0x811c9dc5;
    for (let i = 0; i < s.length; i++) {
      h ^= s.charCodeAt(i);
      h = Math.imul(h, 0x01000193) >>> 0;
    }
    return h.toString(16);
  };
  const out = {};
  const runOne = async (sc) => {
    const url = sc.url ? sc.url.replace("{PORT}", new URL(base).port) : base + sc.path;
    const requestId = ai.createRequestId();
    const req = { requestId, method: sc.method, url, headers: sc.headers, body: sc.body, timeoutMs: sc.timeoutMs };
    const t0 = Date.now();
    let text = "";
    let chunks = 0;
    const errors = [];
    if (sc.cancelAfterMs) setTimeout(() => ai.cancel(requestId), sc.cancelAfterMs);
    let resp;
    try {
      resp =
        sc.kind === "stream"
          ? await ai.requestStream(req, { onChunk: (c) => { text += c; chunks++; }, onError: (e) => errors.push(e) })
          : await ai.request(req);
    } catch (e) {
      resp = { thrown: String(e && e.message) };
    }
    const headers = {};
    for (const k of ["content-type", "x-test"]) if (resp.headers && resp.headers[k] !== undefined) headers[k] = resp.headers[k];
    const body = typeof resp.body === "string" ? resp.body : "";
    out[sc.name] = {
      ok: resp.ok, status: resp.status, statusText: resp.statusText, error: resp.error, thrown: resp.thrown, headers,
      body: body.length <= 400 ? body : undefined, bodyLen: body.length, bodyHash: body.length > 400 ? fnv(body) : undefined,
      streamText: text.length <= 400 ? text : undefined, streamLen: text.length, streamHash: text.length > 400 ? fnv(text) : undefined,
      streamErrors: errors,
      info: { chunks, ms: Date.now() - t0 },
    };
  };
  // 同一 group 的并发运行，其余按顺序
  const groups = new Map();
  const order = [];
  for (const sc of scenarios) {
    if (sc.group) {
      if (!groups.has(sc.group)) { groups.set(sc.group, []); order.push(sc.group); }
      groups.get(sc.group).push(sc);
    } else order.push(sc);
  }
  for (const item of order) {
    if (typeof item === "string") await Promise.all(groups.get(item).map(runOne));
    else await runOne(item);
  }
  return out;
}
