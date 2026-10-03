/**
 * 真实网络验收 / 截图脚本共用的本机代理地址。
 * Local proxy address shared by live-network validation scripts.
 *
 * 环境变量 GUIZHI_TEST_PROXY 形如 http://127.0.0.1:7897；未设置时使用下面的默认值
 * （与此前各脚本写死的端口一致，未改变默认行为）。
 * 本机代理端口变动时，设置环境变量即可（或改这一处），不必逐个脚本修改。
 * 不支持带账号口令的地址；脚本只在隔离的临时 userData 中使用它，不写日志。
 */
export const DEFAULT_TEST_PROXY = "http://127.0.0.1:7897";

const BYPASS = "<local>,localhost,127.0.0.1,::1";

/** 返回可直接传给 window.api.settings.set({ networkProxy }) 的手动代理设置。 */
export function testProxySettings({ bypass = BYPASS, env = process.env } = {}) {
  const raw = (env.GUIZHI_TEST_PROXY || DEFAULT_TEST_PROXY).trim();
  let url;
  try {
    url = new URL(raw);
  } catch {
    throw new Error(`GUIZHI_TEST_PROXY 不是有效网址：${raw.replace(/\/\/[^@/]*@/, "//***@")}`);
  }
  const protocol = url.protocol.replace(/:$/, "");
  if (!["http", "https", "socks5"].includes(protocol))
    throw new Error(`GUIZHI_TEST_PROXY 协议不受支持：${protocol}（可用 http / https / socks5）`);
  if (!url.hostname || !url.port)
    throw new Error("GUIZHI_TEST_PROXY 必须包含主机和端口，例如 http://127.0.0.1:7897");
  if (url.username || url.password)
    throw new Error("GUIZHI_TEST_PROXY 不支持带账号口令的地址");
  return {
    mode: "manual",
    protocol,
    host: url.hostname,
    port: Number(url.port),
    bypass,
    username: "",
    password: "",
  };
}
