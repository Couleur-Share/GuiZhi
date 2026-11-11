// 差分测试桩：论坛抓取器的出站请求全部由测试注入，默认实现不应被调用。
export async function fetchJson(): Promise<never> {
  throw new Error("forum golden: 未注入 fetchJson");
}
export async function fetchHtml(): Promise<never> {
  throw new Error("forum golden: 未注入 fetchHtml");
}

export async function downloadToTempFile(): Promise<never> {
  throw new Error("forum golden: 未注入 downloadToTempFile");
}
export async function fetchRawText(): Promise<never> {
  throw new Error("forum golden: 未注入 fetchRawText");
}

