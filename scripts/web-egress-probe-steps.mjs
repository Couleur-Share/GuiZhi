/**
 * pnpm shot 专用：网页采集渲染窗口的“出口”实测——页面脚本能不能绕过 protocol.handle 的安全出口，
 * 把连接打到本机或非 HTTP 协议上。不依赖公网与本机代理。
 * Egress probe for the hidden web-capture window: can page script bypass the protocol.handle egress?
 *
 * 做法：本机起四个“证人”——
 *   fixture  HTTP 服务，当作应用的手动代理，以虚构站点 guard.probe.example 提供探测页并收集页面自报结果；
 *   L1       监听 127.0.0.1（及 ::1）的 TCP 端口，能完成 WebSocket 握手：任何连到它的流量都是真实泄漏；
 *   L2       监听 127.0.0.1:9，渲染会话的黑洞代理地址：落到它身上的流量说明“被代理规则兜住”；
 *   U1       UDP 端口，用来观察 WebRTC 的 STUN 探测。
 * 探测页在 load 阶段被一张慢图片撑住，等脚本全部跑完再放行；“证人”看到的才是裁决依据，页面自报只作参考。
 *
 * 场景：
 *   A  非 HTTP 协议：ws / wss / WebRTC / file iframe（页面脚本逐项尝试）
 *   B  连接提示：<link rel=preconnect|dns-prefetch>（绕过 URL 加载器，直接由网络栈建连）
 *   C  HTTP 类到本机的各条路径（每条独占一次采集，因为第一次被拦截会使整次采集失败）
 * 变体：
 *   V0  按现状（onBeforeRequest 防护在位）
 *   V2  把防护替换成只记录不拦截的监听，既观察它是否被触发，也观察没有它时的真实走向
 *
 * 用法：pnpm shot --steps scripts/web-egress-probe-steps.mjs --out <新目录>
 * 产物：<out>/egress-probe/results.json；控制台输出逐项裁决表。
 */
import crypto from "node:crypto";
import dgram from "node:dgram";
import fs from "node:fs";
import http from "node:http";
import net from "node:net";
import path from "node:path";
import { createImporter } from "./lib/web-import.mjs";

const HOST = "guard.probe.example";
const ORIGIN = `http://${HOST}`;
const ELSEWHERE = "elsewhere.probe.example";
const WS_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";
// 1x1 透明 PNG，用作被“撑住”的慢图片的响应体。
const PIXEL = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);
const FILLER = "归知网页采集出口探测正文，用于保证可见文字超过空壳判定阈值。".repeat(12);

// 探测页脚本。P1/P2 由服务端替换；每项结果立即上报，采集被中途中止也不丢。
const PAGE_SCRIPT = `
var P1=__P1__,P2=__P2__,SCENARIO='__SCENARIO__',VECTOR='__VECTOR__';
var wait=function(ms){return new Promise(function(r){setTimeout(r,ms)})};
function report(id,value){
  var line=id+': '+value;
  document.getElementById('out').textContent+=line+'\\n';
  return fetch('/report?id='+encodeURIComponent(id)+'&r='+encodeURIComponent(value)).catch(function(){});
}
function socket(url,limit){
  return new Promise(function(resolve){
    var t0=performance.now(),done=false;
    function finish(state){if(done)return;done=true;resolve(state+' '+Math.round(performance.now()-t0)+'ms')}
    try{
      var w=new WebSocket(url);
      w.onopen=function(){finish('open');try{w.close()}catch(e){}};
      w.onerror=function(){finish('error')};
      w.onclose=function(e){finish('close:'+e.code)};
      setTimeout(function(){finish('timeout')},limit||1500);
    }catch(e){finish('throw:'+e.name)}
  });
}
async function rtc(){
  try{
    var pc=new RTCPeerConnection({iceServers:[{urls:'stun:127.0.0.1:'+P2}]});
    pc.createDataChannel('x');
    await pc.setLocalDescription(await pc.createOffer());
    await wait(1200);
    var sdp=(pc.localDescription&&pc.localDescription.sdp)||'';
    var kinds=(sdp.match(/a=candidate:.*/g)||[]).map(function(l){return l.split(' ')[7]});
    pc.close();
    return 'candidates='+kinds.length+(kinds.length?' ['+kinds.join(',')+']':'');
  }catch(e){return 'throw:'+e.name}
}
function frame(src){
  return new Promise(function(resolve){
    var f=document.createElement('iframe'),done=false;
    function finish(s){if(done)return;done=true;resolve(s)}
    f.onload=function(){finish('load')};f.onerror=function(){finish('error')};
    f.src=src;document.body.appendChild(f);setTimeout(function(){finish('timeout')},700);
  });
}
async function main(){
  for(;;){
    var go=await fetch('/go',{cache:'no-store'}).then(function(r){return r.text()}).catch(function(){return ''});
    if(go==='1')break;
    await wait(100);
  }
  await report('start',SCENARIO+(VECTOR?'/'+VECTOR:''));
  if(SCENARIO==='A'){
    await report('ws-loopback',await socket('ws://127.0.0.1:'+P1+'/ws'));
    await report('ws-localhost',await socket('ws://localhost:'+P1+'/ws'));
    await report('wss-loopback',await socket('wss://127.0.0.1:'+P1+'/ws'));
    await report('ws-fixture-host',await socket('ws://${HOST}/ws'));
    await report('webrtc-stun',await rtc());
    await report('iframe-file',await frame('file:///C:/Windows/win.ini'));
  }else if(SCENARIO==='B'){
    ['http://127.0.0.1:'+P1,'http://${HOST}:'+P1].forEach(function(href){
      ['preconnect','dns-prefetch'].forEach(function(rel){
        var l=document.createElement('link');l.rel=rel;l.href=href;document.head.appendChild(l);
      });
    });
    await wait(1500);
    await report('hints','sent');
  }else if(SCENARIO==='C'){
    var target='http://127.0.0.1:'+P1+'/c-'+VECTOR;
    if(VECTOR==='fetch')await fetch(target).catch(function(){});
    else if(VECTOR==='img'){var i=new Image();i.src=target;await wait(800)}
    else if(VECTOR==='beacon'){navigator.sendBeacon(target,'x');await wait(800)}
    else if(VECTOR==='eventsource'){try{new EventSource(target)}catch(e){}await wait(800)}
    else if(VECTOR==='prefetch'){var p=document.createElement('link');p.rel='prefetch';p.href=target;document.head.appendChild(p);await wait(1200)}
    else if(VECTOR==='nav'){location.href=target;await wait(1500)}
    else if(VECTOR==='navftp'){location.href='ftp://127.0.0.1:'+P1+'/f';await wait(1500)}
    else if(VECTOR==='open'){var w=window.open(target);await report('open-result',w===null?'null（被拒绝）':'得到窗口');await wait(800)}
    await report('vector-done',VECTOR);
  }
  await report('done','1');
}
main();
`;

const page = (scenario, vector, p1, p2) =>
  `<!doctype html><html><head><meta charset="utf-8"><title>出口探测 ${scenario}</title></head><body>` +
  `<article><h1>出口探测 ${scenario}</h1><p>${FILLER}</p><x-note></x-note><pre id="out"></pre></article>` +
  // 慢图片撑住 load：脚本全部跑完后由 /hold.png 的释放放行。
  `<img src="/hold.png" width="1" height="1" alt="">` +
  `<script>${PAGE_SCRIPT.replace("__P1__", p1).replace("__P2__", p2).replace("__SCENARIO__", scenario).replace("__VECTOR__", vector)}</script>` +
  `</body></html>`;

/** 场景 D：目录范围 /docs/ 内的入口页，之后经不同方式试图离开范围；js-in-scope 是留在范围内的对照。 */
const SCOPE_KINDS = {
  "js-same": { script: "setTimeout(function(){location.href='/other/b'},300)" },
  "js-cross": { script: `setTimeout(function(){location.href='http://${ELSEWHERE}/x'},300)` },
  click: {
    body: '<a id="l" href="/other/l">离开范围的链接</a>',
    script: "setTimeout(function(){document.getElementById('l').click()},300)",
  },
  // 0 秒刷新：延迟更长的 meta 刷新会晚于沉降结束，采集已完成，探不到东西。
  meta: { head: '<meta http-equiv="refresh" content="0;url=/other/m">' },
  // 先跳到范围内的 /docs/hop，由服务端 302 到范围外：检验“重定向”这一跳也被拦截。
  "js-redirect": { script: "setTimeout(function(){location.href='/docs/hop'},300)" },
  // 入口自身：静态阶段看到正常页面，渲染窗口再请求时被服务端重定向到范围外。
  flip: {},
  // 子框架：两个跨源（其中一个 6 秒才响应）与一个同源。跨源的不应被取走，也不应拖慢主框架 load。
  frames: {
    body:
      `<iframe src="http://${ELSEWHERE}/frame-x"></iframe><iframe src="/docs/frame-same"></iframe>` +
      `<iframe src="http://${ELSEWHERE}/hold"></iframe>`,
  },
  "js-in-scope": { script: "setTimeout(function(){location.href='/docs/next'},300)" },
};
const scopePage = (kind) => {
  const spec = SCOPE_KINDS[kind] ?? {};
  return (
    `<!doctype html><html><head><meta charset="utf-8">${spec.head ?? ""}<title>范围探测 ${kind}</title></head><body>` +
    `<article><h1>范围探测 ${kind}</h1><p>${FILLER}</p><x-note></x-note>${spec.body ?? ""}</article>` +
    `${spec.script ? `<script>${spec.script}</script>` : ""}</body></html>`
  );
};
const plainPage = (name) =>
  `<!doctype html><html><head><meta charset="utf-8"><title>${name}</title></head><body><article><h1>${name}</h1><p>${FILLER}</p></article></body></html>`;

/** 记录一条入站连接的首行（HTTP 请求行或二进制前缀），并在需要时完成 WebSocket 握手。 */
function witness(list, { handshake = false } = {}) {
  return (socket) => {
    const record = { at: Date.now(), local: `${socket.localAddress}:${socket.localPort}`, firstLine: "", upgraded: false };
    list.push(record);
    socket.once("data", (chunk) => {
      const text = chunk.toString("latin1");
      record.firstLine = /^[\x20-\x7e]{3}/.test(text)
        ? text.split("\r\n")[0].slice(0, 120)
        : `0x${chunk.subarray(0, 3).toString("hex")}（${chunk.length} 字节，非 HTTP）`;
      const key = /sec-websocket-key:\s*(.+)\r\n/i.exec(text)?.[1];
      if (handshake && key) {
        record.upgraded = true;
        const accept = crypto.createHash("sha1").update(key.trim() + WS_GUID).digest("base64");
        socket.write(
          `HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`,
        );
        setTimeout(() => socket.destroy(), 300);
      } else socket.destroy();
    });
    socket.on("error", () => {});
    // 只建连不发数据（preconnect 就是这样）也要有记录，稍后关闭。
    setTimeout(() => socket.destroy(), 2500);
  };
}

const listen = (server, port, host) =>
  new Promise((resolve) => {
    server.once("error", (error) => resolve(error));
    server.listen(port, host, () => resolve(null));
  });

export default async function ({ win, app, outDir }) {
  const out = path.join(outDir, "egress-probe");
  fs.mkdirSync(out, { recursive: true });
  const state = { go: false, polls: 0, reports: [], proxyLog: [], held: [], l1: [], l2: [], udp: 0, scenario: "A", vector: "", hits: new Map(), timers: new Set() };

  const l1 = net.createServer(witness(state.l1, { handshake: true }));
  const l1Error = await listen(l1, 0, "127.0.0.1");
  if (l1Error) throw new Error(`L1 监听失败：${l1Error.message}`);
  const P1 = l1.address().port;
  const l1v6 = net.createServer(witness(state.l1, { handshake: true }));
  const l1v6Error = await listen(l1v6, P1, "::1");
  const l2 = net.createServer(witness(state.l2));
  const l2Error = await listen(l2, 9, "127.0.0.1");
  const u1 = dgram.createSocket("udp4");
  u1.on("message", () => state.udp++);
  await new Promise((resolve) => u1.bind(0, "127.0.0.1", resolve));
  const P2 = u1.address().port;
  console.log(
    `证人：L1 127.0.0.1:${P1}${l1v6Error ? "（::1 未监听）" : " 与 ::1"}；L2 127.0.0.1:9 ${l2Error ? `监听失败（${l2Error.code}），黑洞代理流量无法观察` : "已监听"}；U1 udp:${P2}`,
  );

  const releaseHeld = () => {
    clearTimeout(state.holdTimer);
    for (const response of state.held.splice(0)) {
      try {
        response.writeHead(200, { "content-type": "image/png", "cache-control": "no-store" });
        response.end(PIXEL);
      } catch {
        // 采集已中止、连接已关闭：无需再响应。
      }
    }
  };
  const server = http.createServer((request, response) => {
    let url;
    try {
      url = new URL(request.url);
    } catch {
      return response.writeHead(400).end();
    }
    if (url.host !== HOST && url.host !== ELSEWHERE) return response.writeHead(403).end();
    if (url.pathname === "/go") {
      state.polls++;
      return response.writeHead(200, { "content-type": "text/plain", "cache-control": "no-store" }).end(state.go ? "1" : "0");
    }
    // 记录 host + 路径：场景 D 靠它判断“范围外的页面有没有被窗口真正取走”。
    state.proxyLog.push(`${url.host}${url.pathname}${url.search}`.slice(0, 140));
    const html = (body) => {
      response.writeHead(200, { "content-type": "text/html; charset=utf-8", "cache-control": "no-store" });
      response.end(body);
    };
    if (url.host === HOST && url.pathname === "/docs/d-flip") {
      // 同一地址第二次请求（渲染阶段）才重定向到范围外：静态阶段的重定向检查拦不住这种服务端行为。
      const hits = (state.hits.get(url.search) ?? 0) + 1;
      state.hits.set(url.search, hits);
      if (hits >= 2) {
        response.writeHead(302, { location: "/other/f", "cache-control": "no-store" });
        return response.end();
      }
    }
    if (url.host === HOST && url.pathname === "/docs/hop") {
      response.writeHead(302, { location: "/other/h", "cache-control": "no-store" });
      return response.end();
    }
    if (url.host === HOST && url.pathname.startsWith("/docs/d-")) return html(scopePage(url.pathname.slice("/docs/d-".length)));
    if (url.host === ELSEWHERE && url.pathname === "/hold") {
      // 6 秒后才响应的子框架：放行时主框架 load 要等它，取消时不应受影响。
      state.timers.add(setTimeout(() => html(plainPage("hold")), 6000));
      return;
    }
    if (
      url.host === ELSEWHERE ||
      url.pathname === "/docs/next" ||
      url.pathname === "/docs/frame-same" ||
      url.pathname.startsWith("/other/")
    )
      return html(plainPage(`${url.host}${url.pathname}`));
    if (url.pathname === "/report") {
      state.reports.push({ id: url.searchParams.get("id"), r: url.searchParams.get("r") });
      if (url.searchParams.get("id") === "done") setTimeout(releaseHeld, 100);
      return response.writeHead(204).end();
    }
    if (url.pathname === "/hold.png") {
      state.held.push(response);
      // 兜底：脚本异常没有上报 done 时，12 s 后也放行；每次新的占用重置计时，避免上一轮的定时器误放行。
      clearTimeout(state.holdTimer);
      state.holdTimer = setTimeout(releaseHeld, 12_000);
      return;
    }
    if (url.pathname === "/page") {
      response.writeHead(200, { "content-type": "text/html; charset=utf-8", "cache-control": "no-store" });
      return response.end(page(state.scenario, state.vector, String(P1), String(P2)));
    }
    response.writeHead(404).end();
  });
  server.on("connect", (_request, socket) => socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"));
  await listen(server, 0, "127.0.0.1");

  const applyVariant = (variant) =>
    app.evaluate(
      ({ webContents }, arg) => {
        globalThis.__egressHookLog = [];
        const targets = webContents.getAllWebContents().filter((wc) => wc.getURL().startsWith(arg.origin));
        for (const wc of targets) {
          if (arg.variant === "log-only")
            wc.session.webRequest.onBeforeRequest((details, callback) => {
              globalThis.__egressHookLog.push(`${details.resourceType} ${details.url}`.slice(0, 160));
              callback({});
            });
        }
        return targets.length;
      },
      { variant, origin: ORIGIN },
    );
  const readHookLog = () => app.evaluate(() => globalThis.__egressHookLog ?? []);

  const results = [];
  try {
    await win.evaluate(
      (port) =>
        window.api.settings.set({
          networkProxy: { mode: "manual", protocol: "http", host: "127.0.0.1", port, username: "", password: "", bypass: "localhost,127.0.0.1" },
        }),
      server.address().port,
    );
    const importOnce = createImporter({ win, outDir: out, timeoutMs: 90_000 });
    // 本探测检验各类真实请求的出口行为（含 img 向量），并用 /hold.png 这张真实的慢图片撑住 load；
    // 默认的占位图本地应答下图片根本不出网，探测会失去意义，所以恢复真实加载。
    await app.evaluate(() => {
      process.env.GUIZHI_WEB_INERT_RESOURCES = "load";
    });

    const plan = [
      ...["V0", "V2"].map((variant) => ({ scenario: "A", vector: "", variant })),
      ...["V0", "V2"].map((variant) => ({ scenario: "B", vector: "", variant })),
      ...["fetch", "img", "beacon", "eventsource", "prefetch", "nav", "open", "navftp"].map((vector) => ({ scenario: "C", vector, variant: "V0" })),
      { scenario: "C", vector: "navftp", variant: "V2" },
    ];
    let nonce = 0;
    for (const item of plan) {
      Object.assign(state, { go: false, polls: 0, reports: [], proxyLog: [], held: [], scenario: item.scenario, vector: item.vector, udp: 0 });
      state.l1.length = 0;
      state.l2.length = 0;
      const url = `${ORIGIN}/page?run=${++nonce}`;
      const task = importOnce(url, "", `${item.scenario}-${item.vector}-${item.variant}`);
      // 等探测页开始轮询（说明已进入渲染窗口），再施加变体、放行脚本。
      const startedAt = Date.now();
      while (state.polls === 0) {
        if (Date.now() - startedAt > 60_000) throw new Error(`${item.scenario}/${item.variant}：60 s 内探测页没有开始运行（可能没有走渲染路径）`);
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      const hooked = await applyVariant(item.variant === "V2" ? "log-only" : "none");
      if (item.variant === "V2" && hooked === 0) throw new Error(`${item.scenario}/V2：没有找到渲染窗口，变体未生效`);
      state.go = true;
      const finished = await task.catch((error) => ({ status: "error", error: String(error?.message ?? error) }));
      await new Promise((resolve) => setTimeout(resolve, 400));
      const hookLog = item.variant === "V2" ? await readHookLog() : null;
      const row = {
        ...item,
        windows: hooked,
        status: finished.status,
        error: finished.error ?? null,
        reports: state.reports.filter((entry) => !["start", "done"].includes(entry.id)),
        l1: state.l1.map(({ at, ...rest }) => rest),
        l2: l2Error ? null : state.l2.map(({ at, ...rest }) => rest),
        udp: state.udp,
        hookNonHttp: hookLog?.filter((line) => !/^(mainFrame|subFrame|script|stylesheet|image|xhr|other|ping|font|media|cspReport)\s+https?:/i.test(line)) ?? null,
        hookCount: hookLog?.length ?? null,
        proxyRequests: state.proxyLog.filter(
          (entry) => !entry.startsWith(`${HOST}/report`) && !entry.startsWith(`${HOST}/page`) && entry !== `${HOST}/hold.png`,
        ),
      };
      results.push(row);
      console.log(`\n== ${item.scenario}${item.vector ? "/" + item.vector : ""} ${item.variant}  采集 ${row.status}${row.error ? "：" + String(row.error).slice(0, 80) : ""}`);
      for (const entry of row.reports) console.log(`   页面自报 ${entry.id.padEnd(16)} ${entry.r}`);
      console.log(`   L1 本机端口连接 ${row.l1.length} 次${row.l1.map((c) => `\n     - ${c.local} ${c.firstLine}${c.upgraded ? "（已握手）" : ""}`).join("")}`);
      console.log(`   L2 黑洞代理连接 ${row.l2 === null ? "不可观察" : row.l2.length + " 次"}${(row.l2 ?? []).map((c) => `\n     - ${c.firstLine || "（只建连，无数据）"}`).join("")}`);
      console.log(`   U1 UDP 数据报 ${row.udp} 个`);
      if (hookLog) console.log(`   V2 监听触发 ${row.hookCount} 次；其中非 http(s) 或 webSocket：${JSON.stringify(row.hookNonHttp)}`);
      if (row.proxyRequests.length) console.log(`   fixture 收到的其他请求：${JSON.stringify(row.proxyRequests)}`);
    }
    // ---- 场景 D：目录范围（只有经抓取批次采集，request.scope 才会生效）----
    const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
    const crawlOnce = async (kind) => {
      state.proxyLog.length = 0;
      const began = Date.now();
      const url = `${ORIGIN}/docs/d-${kind}?run=${++nonce}`;
      const created = await win.evaluate((input) => window.api.webCapture.create(input), {
        purpose: "documents",
        seeds: [{ url, mode: "directory", directory: "/docs/" }],
        maxPages: 1,
        maxDepth: 0,
      });
      if (!created?.ok) throw new Error(`创建抓取批次失败：${created?.error}`);
      for (const end = Date.now() + 90_000; Date.now() < end; ) {
        const got = await win.evaluate((id) => window.api.webCapture.get(id), created.data);
        const pages = got?.data?.pages ?? [];
        if (pages.length && pages.every((entry) => !["pending", "running"].includes(entry.status))) {
          const elapsedMs = Date.now() - began;
          await sleep(400);
          return { status: pages[0].status, error: pages[0].error ?? null, elapsedMs, requests: state.proxyLog.slice() };
        }
        await sleep(300);
      }
      throw new Error(`范围探测 ${kind} 在 90 s 内未结束`);
    };
    for (const kind of Object.keys(SCOPE_KINDS)) {
      const row = { scenario: "D", vector: kind, variant: "V0", ...(await crawlOnce(kind)) };
      results.push(row);
      console.log(`\n== D/${kind}  抓取页面 ${row.status}${row.error ? "：" + String(row.error).slice(0, 80) : ""}  用时 ${(row.elapsedMs / 1000).toFixed(1)}s`);
      console.log(`   fixture 收到的请求：${JSON.stringify(row.requests.map((entry) => entry.replace(/\?run=\d+/, "")))}`);
    }
    fs.writeFileSync(path.join(out, "results.json"), JSON.stringify(results, null, 2));

    // ---- 裁决 ----
    const failures = [];
    const expect = (ok, message) => {
      if (!ok) failures.push(message);
    };
    const find = (scenario, vector, variant) => results.find((row) => row.scenario === scenario && row.vector === vector && row.variant === variant);
    for (const row of results.filter((item) => item.scenario !== "D")) {
      const label = `${row.scenario}${row.vector ? "/" + row.vector : ""}/${row.variant}`;
      expect(row.l1.length === 0, `${label}：L1 本机端口收到 ${row.l1.length} 次连接（真实泄漏）`);
      expect(row.udp === 0, `${label}：U1 收到 ${row.udp} 个 UDP 数据报（WebRTC 泄漏）`);
    }
    const a0 = find("A", "", "V0");
    for (const id of ["ws-loopback", "ws-localhost", "wss-loopback", "ws-fixture-host"])
      expect(a0?.reports.find((entry) => entry.id === id)?.r.startsWith("error"), `A/V0：${id} 应被拒绝`);
    expect(!a0?.l2?.some((entry) => /CONNECT (127\.0\.0\.1|localhost|guard\.probe\.example)/.test(entry.firstLine)), "A/V0：WebSocket 不应到达黑洞代理（应在监听里就被取消）");
    const a2 = find("A", "", "V2");
    expect(a2?.hookNonHttp?.filter((line) => line.startsWith("webSocket ")).length === 4, "A/V2：webRequest 监听应对 4 个 WebSocket 都触发");
    if (!l2Error)
      expect(["127.0.0.1:", "localhost:", `${HOST}:80`].every((target) => a2?.l2?.some((entry) => entry.firstLine.includes(`CONNECT ${target}`) || entry.firstLine.includes(target))), "A/V2：去掉监听后，WebSocket 应被黑洞代理兜住");
    for (const vector of ["fetch", "img", "beacon", "eventsource", "prefetch"])
      expect(find("C", vector, "V0")?.status === "failed", `C/${vector}：访问本机地址应使采集失败`);
    expect(find("C", "open", "V0")?.reports.some((entry) => entry.id === "open-result" && entry.r.startsWith("null")), "C/open：window.open 应被拒绝");
    for (const kind of ["js-same", "js-cross", "click", "meta", "js-redirect", "flip"]) {
      const row = results.find((item) => item.scenario === "D" && item.vector === kind);
      expect(row?.status === "failed", `D/${kind}：试图离开目录范围的页面应采集失败，实际 ${row?.status}`);
      expect(!row?.requests.some((entry) => entry.startsWith(`${ELSEWHERE}/`) || entry.includes("/other/")), `D/${kind}：范围外的页面不应被窗口取走，实际请求 ${JSON.stringify(row?.requests.filter((entry) => entry.startsWith(`${ELSEWHERE}/`) || entry.includes("/other/")))}`);
      expect(/导航超出允许范围/.test(row?.error ?? ""), `D/${kind}：错误应为“导航超出允许范围”，实际 ${row?.error}`);
    }
    // 子框架：默认不取跨源的，也不被它们拖慢；GUIZHI_WEB_SUBFRAMES=allow 时则相反（基线对照）。
    const allowFrames = process.env.GUIZHI_WEB_SUBFRAMES === "allow";
    const frames = results.find((item) => item.scenario === "D" && item.vector === "frames");
    const sawCross = frames?.requests.filter((entry) => entry.startsWith(`${ELSEWHERE}/`)) ?? [];
    expect(frames && !["failed", "canceled", "skipped"].includes(frames.status), `D/frames：页面应采集成功，实际 ${frames?.status} ${frames?.error ?? ""}`);
    expect(frames?.requests.some((entry) => entry.includes("/docs/frame-same")), "D/frames：同源子框架应照常加载");
    if (allowFrames) {
      expect(sawCross.length === 2, `D/frames(allow)：跨源子框架应被取走，实际 ${JSON.stringify(sawCross)}`);
      expect(frames?.elapsedMs >= 5500, `D/frames(allow)：放行时应被 6 秒的子框架拖住，实际 ${frames?.elapsedMs} ms`);
    } else {
      expect(sawCross.length === 0, `D/frames：跨源子框架不应被取走，实际 ${JSON.stringify(sawCross)}`);
      expect(frames?.elapsedMs < 5500, `D/frames：不应被跨源子框架拖慢，实际 ${frames?.elapsedMs} ms`);
    }
    const inScope = results.find((item) => item.scenario === "D" && item.vector === "js-in-scope");
    expect(inScope && !["failed", "canceled", "skipped"].includes(inScope.status), `D/js-in-scope：范围内的跳转应放行，实际 ${inScope?.status} ${inScope?.error ?? ""}`);
    expect(inScope?.requests.some((entry) => entry.includes("/docs/next")), "D/js-in-scope：范围内的跳转应真正取走目标页面");

    console.log(`\n产物：${out}`);
    if (failures.length) {
      for (const failure of failures) console.log(`  不通过：${failure}`);
      throw new Error(`出口探测有 ${failures.length} 项不通过`);
    }
    console.log("出口探测全部通过：没有任何连接绕过安全出口；离开目录范围的导航在取页面之前就被拦截");
  } finally {
    releaseHeld();
    for (const timer of state.timers) clearTimeout(timer);
    server.closeAllConnections();
    for (const closable of [server, l1, l1v6, l2]) await new Promise((resolve) => closable.close(() => resolve()));
    u1.close();
  }
}
