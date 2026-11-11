// 网页采集纯逻辑的差分基准：真实的 web-extract-article.ts（结果组装、元数据、错误分类）、web-static-route.ts（静态路由判定）、
// web-error.ts、web-scope.ts（规范化网址）。正文 Markdown 的逐页等价性在 spikes/mygo-readability 的对拍里另行验证（59 页），
// 这里用合成页面覆盖结果字段与分支。
export async function buildWebGolden(): Promise<unknown> {
  const base = "../../apps/desktop/src/main/services/web-capture";
  const art = await import(`${base}/web-extract-article.ts`);
  const route = await import(`${base}/web-static-route.ts`);
  const werr = await import(`${base}/web-error.ts`);
  const scope = await import("../../packages/shared/utils/web-scope.ts");

  const longPara = (n: number) => "归知是本地优先的个人知识库，".repeat(n);
  const article = (body: string, head = "", title = "测试文章") =>
    `<!doctype html><html><head><title>${title}</title>${head}</head><body><header><nav><a href="/">首页</a></nav></header><main><article>${body}</article></main><footer>页脚</footer></body></html>`;
  const pages: { name: string; html: string; url: string; status: number }[] = [
    { name: "plain-article", url: "https://example.com/post/1", status: 200, html: article(`<h1>标题</h1><p>${longPara(20)}</p><p>第二段 <a href="/x?y=1">链接</a> <code>code</code></p>`) },
    { name: "published-z", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-05-01T12:00:00Z"><meta name="author" content="张三"><meta name="author" content="李四">`) },
    { name: "published-offset", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-05-01T12:00:00+0800">`) },
    { name: "published-millis", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-05-01T12:00:00.123456-05:30">`) },
    { name: "published-overflow-day", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-02-30T12:00:00+08:00">`) },
    { name: "published-24h", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-05-01T24:00:00+08:00">`) },
    { name: "published-no-tz", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-05-01T12:00:00">`) },
    { name: "published-date-only", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-05-01">`) },
    { name: "published-bad-minute", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content="2024-05-01T12:60:00+08:00">`) },
    { name: "published-space-sep", url: "https://example.com/p", status: 200, html: article(`<p>${longPara(20)}</p>`, `<meta property="article:published_time" content=" 2024-05-01 12:00:00+08:00 ">`) },
    { name: "empty-body", url: "https://example.com/e", status: 200, html: `<html><head><title>空</title></head><body></body></html>` },
    { name: "http-404", url: "https://example.com/404", status: 404, html: `<html><head><title>Not Found</title></head><body><p>${longPara(5)}</p></body></html>` },
    { name: "http-403", url: "https://example.com/403", status: 403, html: `<html><head><title>Forbidden</title></head><body><p>${longPara(5)}</p></body></html>` },
    { name: "http-401", url: "https://example.com/401", status: 401, html: `<html><head><title>x</title></head><body><p>${longPara(5)}</p></body></html>` },
    { name: "http-500", url: "https://example.com/500", status: 500, html: `<html><head><title>x</title></head><body><p>${longPara(5)}</p></body></html>` },
    { name: "captcha-title", url: "https://example.com/c", status: 200, html: `<html><head><title>Just a moment...</title></head><body><p>${longPara(5)}</p></body></html>` },
    { name: "login-form", url: "https://example.com/l", status: 200, html: `<html><head><title>用户登录</title></head><body><form><input type="password"></form><p>${longPara(5)}</p></body></html>` },
    { name: "truncated", url: "https://example.com/t", status: 200, html: article(`<p>${"a".repeat(200_001)}</p>`) },
    { name: "base-href", url: "https://example.com/dir/page", status: 200, html: article(`<p>${longPara(20)}</p><p><a href="rel">相对</a><img src="img/a.png" alt="图"></p>`, `<base href="https://cdn.example.org/assets/">`) },
    { name: "fragment-no-wrapper", url: "https://example.com/f", status: 200, html: `<p>${longPara(10)}</p>` },
    { name: "spa-shell", url: "https://example.com/spa", status: 200, html: `<html><head><title>App</title><script src="/app.js"></script></head><body><div id="root"></div></body></html>` },
    { name: "script-article", url: "https://example.com/s", status: 200, html: article(`<p>${longPara(60)}</p>`).replace("</head>", `<script>1</script></head>`) },
    { name: "json-script-only", url: "https://example.com/j", status: 200, html: article(`<p>${longPara(60)}</p>`).replace("</head>", `<script type="application/ld+json">{}</script></head>`) },
    { name: "hidden-main", url: "https://example.com/h", status: 200, html: `<html><head><title>x</title></head><body><main hidden><p>${longPara(20)}</p></main></body></html>` },
    { name: "hidden-ancestor-style", url: "https://example.com/h2", status: 200, html: `<html><head><title>x</title></head><body style="display: none"><p>${longPara(20)}</p></body></html>` },
    { name: "meta-refresh", url: "https://example.com/r", status: 200, html: `<html><head><title>x</title><meta http-equiv="Refresh" content="3;url=/n"></head><body><p>${longPara(20)}</p></body></html>` },
    { name: "aria-busy", url: "https://example.com/b", status: 200, html: article(`<div aria-busy="true"></div><p>${longPara(20)}</p>`) },
    { name: "loading-placeholder", url: "https://example.com/lp", status: 200, html: article(`<p>${longPara(20)}</p><div>Loading...</div>`) },
    { name: "loading-in-code", url: "https://example.com/lc", status: 200, html: article(`<p>${longPara(20)}</p><pre><code>Loading</code></pre>`) },
    { name: "custom-element", url: "https://example.com/ce", status: 200, html: article(`<p>${longPara(20)}</p><my-widget></my-widget>`) },
    { name: "pre-empty-anchor", url: "https://example.com/pa", status: 200, html: article(`<p>${longPara(20)}</p><pre><a href="#L1"></a>code</pre>`) },
    { name: "two-articles-scripts", url: "https://example.com/ta", status: 200, html: `<html><head><title>x</title></head><body><article><p>${longPara(30)}</p></article><article><p>${longPara(30)}</p></article><script>1</script></body></html>` },
    { name: "onclick-handler", url: "https://example.com/oc", status: 200, html: article(`<p onclick="x()">${longPara(8)}</p>`) },
    { name: "nbsp-whitespace", url: "https://example.com/nb", status: 200, html: article(`<p>\u00a0\u3000\ufeff</p>`) },
    { name: "wechat", url: "https://mp.weixin.qq.com/s/abc", status: 200, html: `<html><head><title>公众号</title></head><body><h1 id="activity-name"> 微信标题 </h1><div id="js_content"><p>${longPara(20)}</p></div></body></html>` },
    { name: "h1-title-fallback", url: "https://example.com/h1", status: 200, html: `<html><head></head><body><main><h1>仅有 H1</h1><p>${longPara(20)}</p></main></body></html>` },
    { name: "long-title", url: "https://example.com/lt", status: 200, html: article(`<p>${longPara(20)}</p>`, "", "题".repeat(400)) },
  ];

  const extract = pages.map((p) => {
    const a = art.extractWebArticle(p.html, p.url, p.status);
    return { name: p.name, source: a.source, result: a.result };
  });
  const staticDecisions = pages.map((p) => ({ name: p.name, d: route.assessStaticPage(p.html, p.status) }));
  const verification = pages.map((p) => ({ name: p.name, v: route.isWebVerificationPage(p.html) }));
  verification.push({ name: "cf-wrapper", v: route.isWebVerificationPage(`<html><body><div id="cf-wrapper"></div></body></html>`) });
  verification.push({ name: "chl-script", v: route.isWebVerificationPage(`<html><body><script src="/cdn-cgi/challenge-platform/orchestrate/chl_page/v1"></script></body></html>`) });
  verification.push({ name: "zh-title", v: route.isWebVerificationPage(`<html><head><title>安全验证 - x</title></head><body></body></html>`) });
  const linkPages = [
    { html: `<a href="/a">1</a><a href="b?c=1#d">2</a><a href="mailto:x@y">3</a><a href="javascript:void(0)">4</a><a href="http://[bad">5</a><a href="//cdn.test/x">6</a><a>7</a><a href="HTTPS://UP.test/Q">8</a>`, url: "https://example.com/dir/page" },
    { html: Array.from({ length: 2100 }, (_, i) => `<a href="/p${i}">x</a>`).join(""), url: "https://example.com/" },
  ];
  const links = linkPages.map((p) => ({ n: p.html.length, url: p.url, links: route.staticPageLinks(p.html, p.url) }));

  const errorTexts: [string, string?][] = [
    ["连接超时"], ["Request timeout"], ["已取消"], ["The operation was aborted"], ["不允许访问内网地址"], ["Access to internal network addresses is not allowed"],
    ["重定向超出目录范围"], ["不支持的协议: ftp:"], ["遇到验证码"], ["网站需要网页验证"], ["请先登录"], ["HTTP 403"], ["HTTP 429 Too Many"], ["拒绝访问"],
    ["网页响应超过 10 MiB"], ["正文提取输入超过 16 MiB"], ["预算已用尽"], ["随便一个错误"], ["访问 https://a.test/path/x?token=abc#frag 失败：超时"],
    ["http://user:pw@host.test:8080/a/b?q=1 无法连接"], ["Failed https://[bad 然后"], ["x".repeat(1200)], ["网页需要登录", "captcha"], ["随便", "not-a-code"], ["HTTP 403 但显式", "network"],
    ["超时但登录"], ["验证码与登录"],
  ];
  const errors = errorTexts.map(([text, code]) => {
    const e: Error & { cause?: unknown } = new Error(text);
    if (code) e.cause = { webCaptureCode: code };
    return { text, code: code ?? null, r: werr.webCaptureError(e) };
  });

  const urls = [
    "https://example.com/a#frag", "http://example.com", "HTTP://EXAMPLE.com:80/x?y#z", "https://user@example.com/", "https://user:pw@example.com/", "ftp://example.com/",
    "javascript:alert(1)", "not a url", "", "https://例え.jp/パス?クエリ=値#h", "https://example.com/a b", "https://[::1]:8080/x", "https://example.com:443/", "//example.com/",
  ];
  const canonical = urls.map((u) => {
    try {
      return { u, ok: true, v: scope.canonicalWebUrl(u) };
    } catch (e) {
      return { u, ok: false, v: (e as Error).message };
    }
  });

  const scopeCases: { entry: string; dir?: string }[] = [
    { entry: "https://example.com/docs/guide/intro.html" }, { entry: "https://example.com/" }, { entry: "https://example.com/docs/", dir: "/docs" },
    { entry: "https://example.com/a/b", dir: "/a/b/" }, { entry: "https://example.com/a", dir: "docs" }, { entry: "https://example.com/a", dir: "/x?y" },
    { entry: "https://example.com/a", dir: "/x#y" }, { entry: "https://example.com/a", dir: "/x%2Fy/" }, { entry: "https://example.com/a", dir: "/x%5cy/" },
    { entry: "https://example.com/a", dir: "/x/../y/" }, { entry: "https://example.com/a", dir: "/x/./y/" }, { entry: "https://example.com/a", dir: "/x%2ey/" },
    { entry: "https://example.com/a", dir: "/%E6%96%87%E6%A1%A3/" }, { entry: "https://example.com/a", dir: "/%E6%96%/" }, { entry: "https://example.com/a", dir: "/%ff/" },
    { entry: "https://example.com/a", dir: "/x\\y/" }, { entry: "https://user@example.com/a" }, { entry: "ftp://example.com/a" },
    { entry: "https://example.com:8443/文档/页面.html" }, { entry: "https://EXAMPLE.com/Docs/Page" },
  ];
  const scopes = scopeCases.map((c) => {
    try { return { ...c, ok: true, v: scope.webScope(c.entry, c.dir) }; } catch (e) { return { ...c, ok: false, v: (e as Error).message }; }
  });
  const inScopeUrls = [
    "https://example.com/docs/guide/x", "https://example.com/docs/guide", "https://example.com/docs/guide/", "https://example.com/docs/guidebook",
    "https://example.com/docs", "https://example.com/other", "http://example.com/docs/guide/x", "https://example.com:8443/docs/guide/x",
    "https://other.com/docs/guide/x", "https://example.com/docs/guide/%2e%2e/x", "https://example.com/docs/guide/a%2Fb", "https://example.com/docs/guide/%E6%96%87",
    "https://example.com/docs/guide/%zz", "https://example.com/docs/guide/%ff", "https://example.com/docs/guide/x#frag", "https://example.com/docs/guide/x?q=1",
    "https://example.com/docs/guide/../guide/x", "https://example.com/docs/guide/a\\b", "not a url", "https://u:p@example.com/docs/guide/x",
  ];
  const inScope = inScopeUrls.map((u) => ({ u, r: scope.inWebScope(u, { origin: "https://example.com", directory: "/docs/guide/" }) }));
  const pageLinks = [
    "https://x.test/a.html", "https://x.test/a.PDF", "https://x.test/a.zip?x=1", "https://x.test/a.mp4", "https://x.test/a.js", "https://x.test/a.jsx",
    "https://x.test/a.jpeg", "https://x.test/a.jpg/", "https://x.test/", "https://x.test/img.svg#x", "nope", "https://x.test/a.woff2", "https://x.test/a.xml", "https://x.test/a.xmlx",
  ].map((u) => ({ u, r: scope.isWebPageLink(u) }));

  return { scopes, inScope, pageLinks, pages: pages.map((p) => ({ name: p.name, html: p.html, url: p.url, status: p.status })), extract, staticDecisions, verification, linkPages, links, errors, canonical };
}
