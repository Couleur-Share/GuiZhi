import { webPause } from "../../../src/main/services/web-capture/web-task-gate";
import type { webNetworkRequest as productionRequest } from "../../../src/main/services/web-capture/web-network";

// 所有网络响应由固定夹具提供，不访问真实网站。
export const calls: string[] = [];
export const webNetworkRequest: typeof productionRequest = async (
  request,
  signal,
) => {
  calls.push(request.url);
  const url = new URL(request.url);
  if (url.pathname === "/redirect")
    return {
      status: 302,
      headers: { location: "https://fixture.example/short" },
      body: "",
    };
  if (url.pathname === "/redirect-outside")
    return {
      status: 302,
      headers: { location: "https://outside.example/short" },
      body: "",
    };
  if (url.pathname === "/frame-root")
    return {
      status: 200,
      headers: { "content-type": "text/html; charset=utf-8" },
      body: Buffer.from(
        '<article>主页面正文</article><iframe src="https://frame.example/short"></iframe><script>fetch("/redirect").then(r=>r.text()).then(t=>document.querySelector("article").textContent="动态请求完成："+t)</script>',
      ).toString("base64"),
    };
  if (url.pathname === "/slow") {
    // 网络替身与正式安全出口一样响应取消，不能让超时掩盖取消结果。
    await webPause(5000, signal);
    throw new Error("受控慢请求");
  }
  const content =
    url.pathname === "/short"
      ? "<title>短文</title><article><p>今天发布了一个小更新。</p></article>"
      : url.pathname === "/denied"
        ? "<title>拒绝访问</title><h1>Forbidden</h1>"
        : '<title>中文技术文档</title><nav>广告导航</nav><main><h1>组件配置</h1><p id="dynamic">加载中</p><table><tr><th>引擎</th><th>并发</th></tr><tr><td>Crawl4AI</td><td>2</td></tr></table><pre><code>print("归知")</code></pre><a href="/reference">参考链接</a></main><script>setTimeout(()=>document.querySelector("#dynamic").textContent="动态正文支持中文知识管理。",100)</script>';
  return {
    status: url.pathname === "/denied" ? 403 : 200,
    headers: { "content-type": "text/html; charset=utf-8" },
    body: Buffer.from(content).toString("base64"),
  };
};
