package main

import (
	"fmt"
	"strings"
)

// 探针页面。JS 里不能出现反引号（Go 的原始字符串），所以一律用字符串拼接。
// 页面通过 window.viva.postMessage(JSON) 把结果交给 Go；Go 用 PostMessage 给页面发指令。

// pageShim 是各页共用的脚本头：post/sleep/onGo。
const pageShim = `
function post(o) { try { window.viva.postMessage(JSON.stringify(o)); } catch (e) {} }
function sleep(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
function onGo(fn) {
  window.viva.onmessage = function (e) { fn(typeof e === 'string' ? e : (e && e.data)); };
}
`

// probeCSP 是 inline 组经 island.Config.CSP 覆盖的策略：脚本只允许内联的探针自身，其余一律不放行，网络与内嵌框架全部关闭。
// 生产页面用岛的默认策略（island.DefaultCSP：脚本只许自有 scheme 的文件）。
const probeCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; frame-src about:; connect-src 'none'; form-action 'none'; base-uri 'none'"

// securityBody 是页面骨架：一个同源 srcdoc 内层框架，和一个带内联样式属性的元素（默认策略下的正向对照用）。
const securityBody = `<iframe id="f" srcdoc="&lt;p&gt;inner&lt;/p&gt;"></iframe><p id="sty" style="color: rgb(1, 2, 3)">style</p>`

// securityHTML 是敌意页面（内联脚本）：inline 组用它（策略经 Config.CSP 放行内联脚本）。
const securityHTML = `<!doctype html><html><head><meta charset="utf-8"><title>island-security-probe</title></head><body>` +
	securityBody + `<script>` + securityJS + `</script></body></html>`

// securityShellHTML 是同一份敌意脚本改成外链（prod 组：岛的默认策略 script-src 'self' 不允许内联脚本）。
const securityShellHTML = `<!doctype html><html><head><meta charset="utf-8"><title>island-security-probe</title></head><body>` +
	securityBody + `<script src="probe.js"></script></body></html>`

// securityJS 是敌意脚本：逐项尝试越出岛的办法，把每一项的结果记在 out 里，最后一并交给 Go。
// 请求是否真的出了岛，由 Go 一侧的回环服务计数判定，不依赖页面自己的说法。
const securityJS = pageShim + `
(async function () {
  var LOOP = '__LOOP__';
  var SVG = '<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="red"/></svg>';
  var out = {};
  async function guard(key, fn, ms) {
    ms = ms || 4000;
    try {
      var r = await Promise.race([Promise.resolve().then(fn), new Promise(function (_, rej) { setTimeout(function () { rej(new Error('timeout')); }, ms); })]);
      out[key] = (r === undefined ? 'ok' : String(r));
    } catch (e) {
      out[key] = 'error:' + ((e && e.name) || '') + ':' + String((e && e.message) || '').slice(0, 80);
    }
  }
  post({ t: 'ready' });

  // 1. 页面能摸到哪些通往 Go 的入口
  out.viva = typeof window.viva;
  out.vivaKeys = Object.keys(window.viva || {}).sort().join(',');
  out.chromeWebview = typeof (window.chrome && window.chrome.webview);
  try {
    var w = window.chrome && window.chrome.webview;
    out.webviewKeys = w ? Object.getOwnPropertyNames(Object.getPrototypeOf(w)).concat(Object.keys(w)).sort().join(',') : '';
    out.hostObjects = w ? typeof w.hostObjects : 'n/a';
  } catch (e) { out.webviewKeys = 'error:' + e.message; }
  out.origin = location.origin;
  out.isSecureContext = String(window.isSecureContext);
  await sleep(500);
  try { out.iframeViva = typeof document.getElementById('f').contentWindow.viva; } catch (e) { out.iframeViva = 'error:' + e.name; }

  // 1b. 内层框架里的桥：同源 srcdoc 与 sandbox（不透明源）各试一次，看框架里有没有 chrome.webview、消息能不能到 Go。
  // 框架里的脚本若被策略挡住，页面就收不到框架的回报，Go 也不会收到 from-iframe。
  window.addEventListener('message', function (ev) { if (ev.data && ev.data.k) { out['frame_' + ev.data.k] = ev.data.v; } });
  function frameScript(kind) {
    return '<scr' + 'ipt>try{var cw=window.chrome&&window.chrome.webview;parent.postMessage({k:"' + kind + '",v:"webview="+typeof cw+",viva="+typeof window.viva},"*");if(cw){cw.postMessage(JSON.stringify({t:"from-iframe",kind:"' + kind + '"}));}}catch(e){parent.postMessage({k:"' + kind + '",v:"error:"+e.message},"*");}</scr' + 'ipt>';
  }
  var fr1 = document.createElement('iframe'); fr1.srcdoc = frameScript('srcdoc'); document.body.appendChild(fr1);
  var fr2 = document.createElement('iframe'); fr2.setAttribute('sandbox', 'allow-scripts'); fr2.srcdoc = frameScript('sandboxed'); document.body.appendChild(fr2);
  await sleep(1200);

  // 1c. 正向对照：默认策略下界面真正要用的东西必须能用（自有 scheme 的 fetch 与图片、data 与 blob 图片、内联样式属性）。
  function imgTest(src) { return new Promise(function (res, rej) { var i = new Image(); i.onload = function () { res('load'); }; i.onerror = function () { rej(new Error('img-error')); }; i.src = src; }); }
  await guard('ownFetch', function () { return fetch('data.json').then(function (r) { return r.text(); }).then(function (t) { return t.slice(0, 12); }); });
  await guard('ownImage', function () { return imgTest('pic.svg'); });
  await guard('dataImage', function () { return imgTest('data:image/svg+xml;utf8,' + encodeURIComponent(SVG)); });
  await guard('blobImage', function () { return imgTest(URL.createObjectURL(new Blob([SVG], { type: 'image/svg+xml' }))); });
  out.inlineStyle = getComputedStyle(document.getElementById('sty')).color;

  // 2. 出站请求（回环服务计数判定是否出了岛）
  await guard('fetchNoCors', function () { return fetch(LOOP + '/fetch-no-cors', { mode: 'no-cors' }).then(function (r) { return r.type; }); });
  await guard('fetchCors', function () { return fetch(LOOP + '/fetch-cors').then(function (r) { return r.status; }); });
  await guard('xhr', function () { return new Promise(function (res, rej) { var x = new XMLHttpRequest(); x.open('GET', LOOP + '/xhr'); x.onload = function () { res(x.status); }; x.onerror = function () { rej(new Error('xhr-error')); }; x.send(); }); });
  await guard('beacon', function () { return navigator.sendBeacon(LOOP + '/beacon', 'x'); });
  await guard('image', function () { return new Promise(function (res, rej) { var i = new Image(); i.onload = function () { res('load'); }; i.onerror = function () { rej(new Error('img-error')); }; i.src = LOOP + '/image.png'; }); });
  await guard('stylesheet', function () { return new Promise(function (res, rej) { var l = document.createElement('link'); l.rel = 'stylesheet'; l.href = LOOP + '/style.css'; l.onload = function () { res('load'); }; l.onerror = function () { rej(new Error('css-error')); }; document.head.appendChild(l); }); });
  await guard('script', function () { return new Promise(function (res, rej) { var s = document.createElement('script'); s.src = LOOP + '/script.js'; s.onload = function () { res('load'); }; s.onerror = function () { rej(new Error('script-error')); }; document.head.appendChild(s); }); });
  await guard('websocket', function () { return new Promise(function (res, rej) { try { var s = new WebSocket('ws://' + LOOP.slice(7) + '/ws'); s.onopen = function () { res('open'); }; s.onerror = function () { rej(new Error('ws-error')); }; s.onclose = function () { rej(new Error('ws-close')); }; } catch (e) { rej(e); } }); });
  await guard('iframeExternal', function () { return new Promise(function (res) { var i = document.createElement('iframe'); i.src = LOOP + '/iframe'; i.onload = function () { res('load-event'); }; document.body.appendChild(i); setTimeout(function () { res('no-load-event'); }, 2500); }); });

  // 3. 弹窗、下载
  await guard('windowOpen', function () { var w2 = window.open(LOOP + '/window-open'); return w2 === null ? 'null' : 'window'; });
  await guard('download', function () { var a = document.createElement('a'); a.href = URL.createObjectURL(new Blob(['probe'])); a.download = 'gz-island-probe-download.txt'; document.body.appendChild(a); a.click(); return 'clicked'; });
  await sleep(1500);

  // 4. 权限（只查状态与请求无害的几项；摄像头与麦克风只查状态，不发请求）
  await guard('permQueryGeolocation', function () { return navigator.permissions.query({ name: 'geolocation' }).then(function (s) { return s.state; }); });
  await guard('permQueryCamera', function () { return navigator.permissions.query({ name: 'camera' }).then(function (s) { return s.state; }); });
  await guard('permQueryNotifications', function () { return navigator.permissions.query({ name: 'notifications' }).then(function (s) { return s.state; }); });
  await guard('geolocationRequest', function () { return new Promise(function (res, rej) { navigator.geolocation.getCurrentPosition(function () { res('granted'); }, function (e) { rej(new Error('code' + e.code)); }, { timeout: 3000 }); }); }, 5000);
  await guard('notificationRequest', function () { return Notification.requestPermission(); }, 4000);
  await guard('clipboardRead', function () { return navigator.clipboard.readText().then(function () { return 'read'; }); }, 4000);

  // 5. 导航：每一项之后看页面是不是还在原地（导航成功会换掉整个页面，脚本随之消失，Go 一侧收不到 done）
  var before = location.href;
  function where() { return location.href === before ? 'stayed' : 'moved:' + location.href.slice(0, 60); }
  location.href = LOOP + '/nav-http'; await sleep(1200); out.afterHttpNav = where();
  location.href = 'file:///C:/Windows/win.ini'; await sleep(1200); out.afterFileNav = where();
  location.replace('data:text/html,<h1>x</h1>'); await sleep(1200); out.afterDataNav = where();
  var fm = document.createElement('form'); fm.method = 'POST'; fm.action = LOOP + '/form'; document.body.appendChild(fm); fm.submit(); await sleep(1200); out.afterFormSubmit = where();
  var m = document.createElement('meta'); m.httpEquiv = 'refresh'; m.content = '0;url=' + LOOP + '/meta'; document.head.appendChild(m); await sleep(1500); out.afterMetaRefresh = where();
  var a2 = document.createElement('a'); a2.href = LOOP + '/link'; document.body.appendChild(a2); a2.click(); await sleep(1200); out.afterLinkClick = where();
  post({ t: 'done', out: out });
})();`

// ephemeralWriteHTML 写入 localStorage 与 IndexedDB；ephemeralReadHTML 在另一个岛里读它们。
const ephemeralWriteHTML = `<!doctype html><meta charset="utf-8"><script>` + pageShim + `
(async function () {
  var out = {};
  try { localStorage.setItem('gz-probe', 'persisted'); out.localStorage = 'written'; } catch (e) { out.localStorage = 'error:' + e.name; }
  try {
    await new Promise(function (res, rej) {
      var r = indexedDB.open('gz-probe-db', 1);
      r.onupgradeneeded = function () { r.result.createObjectStore('s'); };
      r.onsuccess = function () { var tx = r.result.transaction('s', 'readwrite'); tx.objectStore('s').put('persisted', 'k'); tx.oncomplete = function () { r.result.close(); res(); }; };
      r.onerror = function () { rej(r.error); };
    });
    out.indexedDB = 'written';
  } catch (e) { out.indexedDB = 'error:' + e.name; }
  post({ t: 'done', out: out });
})();
</script>`

const ephemeralReadHTML = `<!doctype html><meta charset="utf-8"><script>` + pageShim + `
(async function () {
  var out = {};
  try { out.localStorage = localStorage.getItem('gz-probe'); } catch (e) { out.localStorage = 'error:' + e.name; }
  try {
    out.indexedDB = await new Promise(function (res, rej) {
      var r = indexedDB.open('gz-probe-db', 1);
      var existed = true;
      r.onupgradeneeded = function () { existed = false; r.result.createObjectStore('s'); };
      r.onsuccess = function () {
        if (!existed) { r.result.close(); res('absent'); return; }
        var g = r.result.transaction('s').objectStore('s').get('k');
        g.onsuccess = function () { r.result.close(); res(String(g.result)); };
        g.onerror = function () { r.result.close(); rej(g.error); };
      };
      r.onerror = function () { rej(r.error); };
    });
  } catch (e) { out.indexedDB = 'error:' + e.name; }
  post({ t: 'done', out: out });
})();
</script>`

// throttleHTML 在收到 go 之后计时：每 50 ms 一个 setInterval，外加 requestAnimationFrame 计数，
// 并按 5 秒一档记下 tick 数，用来看节流是从第几秒开始的。
const throttleHTML = `<!doctype html><meta charset="utf-8"><body><script>` + pageShim + `
post({ t: 'ready', visibility: document.visibilityState });
onGo(function (arg) {
  var duration = parseInt(arg, 10) || 30000, t0 = performance.now(), last = t0;
  var ticks = 0, raf = 0, maxGap = 0, bins = [];
  var iv = setInterval(function () {
    var n = performance.now(); ticks++; maxGap = Math.max(maxGap, n - last); last = n;
    var b = Math.floor((n - t0) / 5000); bins[b] = (bins[b] || 0) + 1;
  }, 50);
  (function frame() { raf++; requestAnimationFrame(frame); })();
  var vis = [document.visibilityState];
  document.addEventListener('visibilitychange', function () { vis.push(document.visibilityState); });
  setTimeout(function () {
    clearInterval(iv);
    var el = performance.now() - t0;
    post({ t: 'done', ticks: ticks, expectedTicks: Math.round(el / 50), raf: raf, maxGapMs: Math.round(maxGap), elapsedMs: Math.round(el), bins: bins, visibility: vis });
  }, duration);
});
</script>`

// msgHTML 是消息大小探针：收到 send:<n> 就回一条 n 个字符的消息；收到别的就回 recv:<长度>。
const msgHTML = `<!doctype html><meta charset="utf-8"><script>` + pageShim + `
post({ t: 'ready' });
onGo(function (text) {
  if (typeof text === 'string' && text.indexOf('send:') === 0) {
    var n = parseInt(text.slice(5), 10);
    window.viva.postMessage('big:' + n + ':' + 'y'.repeat(n));
  } else {
    post({ t: 'recv', n: (text || '').length });
  }
});
</script>`

// readingHTML 的脚本在收到 go 之后用 requestAnimationFrame 逐帧程序化滚动，记下帧间隔分布。
func readingHTML(article string) string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>
body{font:16px/1.75 "Microsoft YaHei UI",system-ui,sans-serif;margin:0;padding:24px 48px;max-width:860px;color:#222;background:#fff}
h1,h2,h3{line-height:1.3}pre{background:#f5f5f5;padding:12px;overflow:auto;border-radius:6px}code{font-family:Consolas,monospace}
table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:4px 10px}blockquote{margin:0;padding-left:12px;border-left:3px solid #ccc;color:#555}
</style></head><body>` + article + `<script>` + pageShim + `
post({ t: 'ready', height: document.documentElement.scrollHeight, nodes: document.getElementsByTagName('*').length });
onGo(function () {
  var deltas = [], prev = performance.now(), start = prev, y = 0, maxY = document.documentElement.scrollHeight - innerHeight, dir = 1;
  function step() {
    var now = performance.now(); deltas.push(now - prev); prev = now;
    y += 40 * dir; if (y >= maxY) { y = maxY; dir = -1; } else if (y <= 0) { y = 0; dir = 1; }
    window.scrollTo(0, y);
    if (now - start < 6000) { requestAnimationFrame(step); return; }
    deltas.shift(); deltas.sort(function (a, b) { return a - b; });
    function q(p) { return deltas.length ? +deltas[Math.min(deltas.length - 1, Math.floor(p * deltas.length))].toFixed(2) : 0; }
    post({ t: 'done', frames: deltas.length, p50: q(0.5), p95: q(0.95), p99: q(0.99), max: +(deltas[deltas.length - 1] || 0).toFixed(2),
      over20: deltas.filter(function (d) { return d > 20; }).length, over34: deltas.filter(function (d) { return d > 34; }).length });
  }
  requestAnimationFrame(step);
});
</script></body></html>`
}

// article 生成约 size 字节的中文长文 HTML：标题、段落、列表、引用、代码块与表格交替出现。
func article(size int) string {
	sentences := []string{
		"知识库的价值不在于收集了多少内容，而在于需要的时候能不能在三秒之内找回它，并且还记得当初为什么要保存它。",
		"采集到的网页往往带着导航栏、广告和评论区，阅读视图要做的第一件事是把正文从这些噪声里分离出来，再按统一的排版呈现。",
		"检索质量取决于切块的粒度：块太大，向量会被无关内容稀释；块太小，上下文又断得支离破碎，所以通常按语义边界切并保留少量重叠。",
		"本地优先意味着数据首先属于用户的磁盘，网络只是可选的增强；备份、迁移和导出都应该是一条命令就能完成的普通操作。",
		"当模型的回答引用了某条笔记，界面必须能一键跳回原文并高亮被引用的段落，否则用户没有办法判断答案是否可信。",
		"长文阅读最容易暴露性能问题：几千行的文本、几十个代码块、再加上表格和图片，滚动时每一帧都要在十六毫秒之内完成。",
		"错误处理要让用户看得懂、能重试：失败的原因要写清楚，可以复制，批量操作要逐条记账，而不是笼统地提示完成。",
		"配置里凡是离开数据目录的机密字段都必须加密保存，日志里不允许出现密钥与口令，导出的备份也不携带这些内容。",
	}
	var b strings.Builder
	b.WriteString("<h1>长文阅读性能探针</h1>")
	for i := 0; b.Len() < size; i++ {
		switch i % 7 {
		case 0:
			fmt.Fprintf(&b, "<h2>第 %d 节 · %s</h2>", i/7+1, sentences[i%len(sentences)][:18])
		case 3:
			b.WriteString("<ul>")
			for j := 0; j < 4; j++ {
				fmt.Fprintf(&b, "<li>%s</li>", sentences[(i+j)%len(sentences)])
			}
			b.WriteString("</ul>")
		case 4:
			fmt.Fprintf(&b, "<blockquote>%s</blockquote>", sentences[(i*3)%len(sentences)])
		case 5:
			b.WriteString("<pre><code>")
			for j := 0; j < 22; j++ {
				fmt.Fprintf(&b, "func step%d_%d(ctx context.Context, items []Item) error { // 第 %d 行\n\tfor _, it := range items { if err := handle(ctx, it); err != nil { return err } }\n", i, j, j)
			}
			b.WriteString("</code></pre>")
		case 6:
			b.WriteString("<table><tr><th>指标</th><th>当前</th><th>目标</th><th>备注</th></tr>")
			for j := 0; j < 8; j++ {
				fmt.Fprintf(&b, "<tr><td>指标 %d-%d</td><td>%d ms</td><td>%d ms</td><td>%s</td></tr>", i, j, 10+j*3, 8+j*2, sentences[(i+j)%len(sentences)][:24])
			}
			b.WriteString("</table>")
		default:
			fmt.Fprintf(&b, "<p>%s%s</p>", sentences[i%len(sentences)], sentences[(i+3)%len(sentences)])
		}
	}
	return b.String()
}
