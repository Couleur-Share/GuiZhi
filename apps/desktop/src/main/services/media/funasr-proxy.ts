/** 本地转写引擎经 Python（pip、modelscope）联网；其 requests 缺少 PySocks，无法使用 SOCKS 代理。 */
export const FUNASR_SOCKS_PROXY_HINT =
  "本地转写引擎经 Python 下载依赖和模型，暂不支持 SOCKS5 代理。请在「设置 → 网络设置」把代理协议改为 HTTP（Clash 等客户端的混合端口同时支持 HTTP）后重试。";

const SOCKS_SUPPORT_MISSING = /Missing dependencies for SOCKS support/i;

/** Python 报缺少 SOCKS 支持时换成可操作的说明，并保留原始原因供复制。 */
export function explainFunasrProxyFailure(message: string): string {
  return SOCKS_SUPPORT_MISSING.test(message)
    ? `${FUNASR_SOCKS_PROXY_HINT}\n原始错误：${message}`
    : message;
}
