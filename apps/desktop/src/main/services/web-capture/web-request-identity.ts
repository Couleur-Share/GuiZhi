/**
 * 非浏览器发出的网页请求（静态抓取、robots.txt、sitemap）的请求身份。
 *
 * 这些请求原先只带 host 与 accept-encoding，既没有 User-Agent 也没有 Accept；渲染窗口里 Chromium
 * 发出的请求则带完整的浏览器头。同一次采集的两个阶段身份不一致有两个后果：
 *  - 按客户端特征拒绝请求的站点（缺少 User-Agent 就返回 403，如 Wikipedia）在静态阶段被拒，而同一
 *    站点用渲染窗口或浏览器能正常访问；robots.txt 同样被拒时，文档站批次会因“规则读取失败”暂停；
 *  - 静态与渲染可能取到不同版本的页面。
 * 这里让它们沿用渲染窗口的身份：Chromium 自带的 User-Agent（含 `GuiZhi/<版本>` 记号，是自报家门，
 * 没有冒用任何爬虫身份；robots.txt 里针对 GuiZhi 的规则也由此可被站点识别）与文档导航的 Accept。
 *
 * 不带 Accept-Language 与 Upgrade-Insecure-Requests：渲染窗口经 protocol.handle 转发时本来就没有
 * Accept-Language（Electron 33 实测），不补它，静态与渲染才不会因语言协商得到不同版本；
 * Upgrade-Insecure-Requests 是能力声明，不是身份，发出去只会让 http 站点多一次重定向。
 *
 * 遇到 HTTP 错误仍保持失败：这里只是让第一次请求不再缺少身份，不是被拒之后换一种方式重试。
 *
 * 设置环境变量 GUIZHI_WEB_IDENTITY=none 恢复原先不带身份的请求（排查与 A/B 对照）。
 */
export const WEB_IDENTITY_ENV = "GUIZHI_WEB_IDENTITY";

/** Chromium 130 对文档导航发出的 Accept，取自渲染窗口主文档请求的实测值。 */
export const DOCUMENT_ACCEPT =
  "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7";

type Env = Record<string, string | undefined>;

/** Electron 全局默认的 User-Agent，与渲染会话（未单独设置 UA）发出的一致；取不到则不带身份。 */
async function chromiumUserAgent(): Promise<string | undefined> {
  try {
    const { app } = await import("electron");
    return app.userAgentFallback || undefined;
  } catch {
    return undefined;
  }
}

/** 静态抓取主文档的请求头：User-Agent 与 Accept，与渲染窗口的主文档请求一致。 */
export async function webDocumentHeaders(
  env: Env = process.env,
): Promise<Record<string, string>> {
  if (env[WEB_IDENTITY_ENV] === "none") return {};
  const userAgent = await chromiumUserAgent();
  return userAgent ? { "user-agent": userAgent, accept: DOCUMENT_ACCEPT } : {};
}

/** robots.txt、sitemap 等文本请求的请求头：只带 User-Agent。 */
export async function webTextHeaders(
  env: Env = process.env,
): Promise<Record<string, string>> {
  if (env[WEB_IDENTITY_ENV] === "none") return {};
  const userAgent = await chromiumUserAgent();
  return userAgent ? { "user-agent": userAgent } : {};
}
