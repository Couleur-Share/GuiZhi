export const DEFAULT_OVERALL_SECONDS = 900;
export const MAX_OVERALL_SECONDS = 86400;

// 整次容器运行的截止秒数。默认 900 秒：暖启动实测约 63 秒，首次还要下载固定镜像与 pnpm，需要留出余量。
// 无效值直接报错，不回退默认值，避免写错的环境变量让限时悄悄失效。
export function overallSeconds(env = process.env) {
  const raw = env.CHECK_LINUX_TIMEOUT;
  if (raw === undefined || raw === '') return DEFAULT_OVERALL_SECONDS;
  if (!/^[1-9]\d*$/.test(raw) || Number(raw) > MAX_OVERALL_SECONDS) {
    throw Error('CHECK_LINUX_TIMEOUT must be an integer from 1 to ' + MAX_OVERALL_SECONDS + ' seconds.');
  }
  return Number(raw);
}
