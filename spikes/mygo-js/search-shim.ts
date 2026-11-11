import { hostAsync } from "./pipeline-host";
export async function searchReadingWeb(query: string, signal: AbortSignal, scenario = "themedReading") {
  return JSON.parse(await hostAsync("search", { query, scenario }, signal));
}
