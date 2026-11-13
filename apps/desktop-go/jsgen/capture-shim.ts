import { hostAsync } from "./pipeline-host";
export async function captureWebPage(request: { url: string; purpose?: string; taskId?: string }, signal?: AbortSignal) {
  return JSON.parse(await hostAsync("capture", request, signal));
}
