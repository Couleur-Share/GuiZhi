/**
 * 渲染窗口的子框架（iframe）加载策略：不加载跨源子框架，同源子框架照常加载。
 *
 * 采集只用主框架的 DOM。子框架内容不进快照（快照清洗会移除 iframe），跨源子框架的 DOM
 * 也读不到。跨源子框架（视频嵌入、广告、统计、评论小组件）却有两项代价：
 *  - 主框架的 load 要等它们全部载完，页面越嵌越慢；
 *  - 渲染窗口走自定义协议、没有跨框架缓存，每个子框架各自重复下载同一批脚本与样式，
 *    迅速耗尽请求与字节预算。实测 developer.chrome.com/docs/devtools 嵌入 9 个 YouTube
 *    播放器，约 180 个请求、30 MB，同一个 1.6 MB 的播放器脚本被下载 9 次，
 *    页面因“网络预算已用尽”采集失败。
 * 同源子框架可能是站点自己的内容框架或脚本依赖，保持加载。
 *
 * 设置环境变量 GUIZHI_WEB_SUBFRAMES=allow 恢复加载全部子框架（排查某个站点或做 A/B 对照）。
 */
export const WEB_SUBFRAMES_ENV = "GUIZHI_WEB_SUBFRAMES";

const isHttp = (url: URL) => url.protocol === "http:" || url.protocol === "https:";

/**
 * 是否取消这次子框架导航。只判断 http(s) 的跨源子框架；无法判断（顶层地址为空、不是 http(s)
 * 或不是合法网址）或目标不产生网络请求（about:blank、blob:、data: 等）一律不拦截，保持原行为。
 */
export function shouldBlockSubframe(
  frameUrl: string,
  topUrl: string,
  env: Record<string, string | undefined> = process.env,
): boolean {
  if (env[WEB_SUBFRAMES_ENV] === "allow") return false;
  try {
    const frame = new URL(frameUrl);
    const top = new URL(topUrl);
    return isHttp(frame) && isHttp(top) && frame.origin !== top.origin;
  } catch {
    return false;
  }
}
