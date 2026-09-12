import fs from "node:fs/promises";
import path from "node:path";
import https from "node:https";
import { randomUUID } from "node:crypto";
import { safeStorage } from "electron";
import { getUserDataPath } from "@guizhi/core/runtime-paths";
import type { ReadingSearchStatus, ReadingSearchProvider, ReadingSearchConfigInput, ReadingQuery } from "@guizhi/shared/types/reading-reconstruction";
import type { AIUsageScenarioId } from "@guizhi/shared/types/ai-usage";
import { getHttpRequestAgent } from "../network-proxy";
import { isPublicSearchUrl } from "./search-result-url";
import { nativeSearchAvailability, searchNativeWeb, NATIVE_SEARCH_TIMEOUT_MS } from "./native-search";

const providers = ["tavily", "anysearch"] as const;
const names = { tavily: "Tavily", anysearch: "AnySearch", native: "模型服务原生搜索" };
interface SearchConfig { defaultEnabled?: boolean; provider: ReadingSearchProvider; keys: Partial<Record<ReadingSearchProvider, string>>; }
let transient: { directory: string; config: SearchConfig } | undefined;
let saving: Promise<unknown> = Promise.resolve();
const persistent = () => safeStorage.isEncryptionAvailable() && (process.platform !== "linux" || safeStorage.getSelectedStorageBackend() !== "basic_text");
const file = () => path.join(getUserDataPath(), ".machine", "reading-search.json");
function parseProvider(value: unknown): ReadingSearchProvider {
  if (value !== "tavily" && value !== "anysearch" && value !== "native") throw new Error("搜索服务无效");
  return value;
}
async function config(): Promise<SearchConfig> {
  if (transient?.directory === getUserDataPath()) return structuredClone(transient.config);
  if (!persistent()) return { provider: "tavily", keys: {} };
  try {
    const value = JSON.parse(await fs.readFile(file(), "utf8"));
    // 旧版只存 Tavily 密文；首次修改时升级，同时保留它的密钥。
    if (typeof value.encrypted === "string") return { provider: "tavily", keys: { tavily: safeStorage.decryptString(Buffer.from(value.encrypted, "base64")) } };
    if (value.version !== 2 || !value.encryptedKeys || typeof value.encryptedKeys !== "object") throw new Error("配置格式无效");
    const keys: SearchConfig["keys"] = {};
    for (const provider of providers) {
      const encrypted = value.encryptedKeys[provider];
      if (encrypted !== undefined) {
        if (typeof encrypted !== "string") throw new Error("密钥格式无效");
        keys[provider] = safeStorage.decryptString(Buffer.from(encrypted, "base64"));
      }
    }
    return { provider: parseProvider(value.provider), keys, defaultEnabled: value.defaultEnabled === true };
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return { provider: "tavily", keys: {} };
    throw new Error("联网搜索密钥无法读取，请检查系统安全存储后重试", { cause: error });
  }
}
function status(value: SearchConfig): ReadingSearchStatus {
  const native = nativeSearchAvailability();
  return { defaultEnabled: value.defaultEnabled === true, configured: value.provider === "native" ? Boolean(native.model) : Boolean(value.keys[value.provider]), persistent: persistent(), provider: value.provider,
    configuredProviders: [...providers.filter(provider => Boolean(value.keys[provider])), ...(native.model ? ["native" as const] : [])],
    nativeModel: native.model, nativeUnavailableReason: native.error };
}
export async function readingSearchStatus(): Promise<ReadingSearchStatus> { return status(await config()); }
async function persist(value: SearchConfig): Promise<void> {
  if (!persistent()) { transient = { directory: getUserDataPath(), config: structuredClone(value) }; return; }
  const encryptedKeys: Partial<Record<ReadingSearchProvider, string>> = {};
  for (const provider of providers) if (value.keys[provider]) encryptedKeys[provider] = safeStorage.encryptString(value.keys[provider]!).toString("base64");
  await fs.mkdir(path.dirname(file()), { recursive: true });
  const staging = `${file()}.${randomUUID()}.tmp`;
  try {
    await fs.writeFile(staging, JSON.stringify({ version: 2, provider: value.provider, encryptedKeys, defaultEnabled: value.defaultEnabled === true }), { mode: 0o600, flag: "wx" });
    await fs.rename(staging, file());
    transient = undefined;
  } finally { await fs.rm(staging, { force: true }); }
}
/** 连接测试先验证候选配置；失败时保留之前可用的服务和密钥。 */
export async function configureReadingSearch(input: ReadingSearchConfigInput = {}): Promise<ReadingSearchStatus> {
  if (!input || typeof input !== "object" || Array.isArray(input)) throw new Error("搜索配置格式无效");
  if (input.provider !== undefined) parseProvider(input.provider);
  if (input.defaultEnabled !== undefined && typeof input.defaultEnabled !== "boolean") throw new Error("搜索默认开关无效");
  if (input.test !== undefined && typeof input.test !== "boolean") throw new Error("搜索测试选项无效");
  if (input.apiKey !== undefined && (typeof input.apiKey !== "string" || input.apiKey.length > 1000 || /[\x00-\x1f\x7f]/.test(input.apiKey))) throw new Error("搜索密钥格式无效");
  const update = async () => {
    const value = await config();
    const selected = input.provider ?? value.provider;
    if (selected === "native" && input.apiKey !== undefined) throw new Error("原生搜索复用主文本模型凭证，请在模型服务中管理密钥");
    if (input.defaultEnabled !== undefined) value.defaultEnabled = input.defaultEnabled;
    if (input.apiKey !== "") value.provider = selected;
    if (input.apiKey !== undefined) {
      const key = input.apiKey.trim();
      if (key) value.keys[selected] = key; else delete value.keys[selected];
    }
    if (input.test) await search("OpenAI web search documentation", AbortSignal.timeout(selected === "native" ? NATIVE_SEARCH_TIMEOUT_MS : 45000), value);
    if (input.apiKey !== undefined || input.provider !== undefined || input.defaultEnabled !== undefined) await persist(value);
    return status(value);
  };
  const result = saving.then(update);
  saving = result.catch(() => undefined);
  return result;
}
export async function saveReadingSearchKey(value: string, provider?: ReadingSearchProvider): Promise<void> {
  await configureReadingSearch({ apiKey: value, provider });
}
export async function searchReadingWeb(query: string, signal: AbortSignal, scenario: AIUsageScenarioId = "themedReading"): Promise<ReadingQuery["results"]> {
  return search(query, signal, await config(), scenario);
}
function httpError(provider: ReadingSearchProvider, code?: number): Error {
  const hints: Record<number, string> = { 400: "请求无效，请检查搜索词", 401: "密钥无效，请重新配置", 402: "搜索额度已用尽，请检查账户额度", 403: "密钥已停用、过期或无权访问", 429: "请求过于频繁，请稍后重试", 502: "搜索服务暂时不可用，请稍后重试" };
  return new Error(`${names[provider]} 搜索失败（HTTP ${code}）${hints[code!] ? `：${hints[code!]}` : "，请稍后重试"}`);
}
/** 固定官方端点，沿用应用代理；不跟随重定向，也不回显上游错误正文和请求头。 */
async function search(query: string, signal: AbortSignal, value: SearchConfig, scenario: AIUsageScenarioId = "themedReading"): Promise<ReadingQuery["results"]> {
  if (value.provider === "native") return searchNativeWeb(query, signal, scenario);
  const { provider } = value, secret = value.keys[provider];
  if (!secret) throw new Error(`请先在设置中配置 ${names[provider]} 搜索密钥，或明确选择不联网生成`);
  if (typeof query !== "string" || !query.trim() || query.length > 400) throw new Error("搜索词无效");
  signal.throwIfAborted();
  const endpoint = new URL(provider === "anysearch" ? "https://api.anysearch.com/v1/search" : "https://api.tavily.com/search");
  const body = JSON.stringify(provider === "anysearch"
    ? { query: query.trim(), max_results: 5, format: "markdown" }
    : { query: query.trim(), max_results: 5, search_depth: "basic", auto_parameters: false, include_answer: false, include_images: false, include_raw_content: "markdown" });
  const data = await new Promise<string>((resolve, reject) => {
    const request = https.request(endpoint, { method: "POST", agent: getHttpRequestAgent(endpoint), signal,
      headers: { Authorization: `Bearer ${secret}`, "Content-Type": "application/json", "Content-Length": Buffer.byteLength(body) } }, response => {
      if (response.statusCode !== 200) { response.resume(); reject(httpError(provider, response.statusCode)); return; }
      let bytes = 0; const chunks: Buffer[] = [];
      response.on("data", chunk => {
        bytes += chunk.length;
        if (bytes > 4 * 1024 * 1024) { reject(new Error("搜索响应超过大小上限")); request.destroy(); }
        else chunks.push(chunk);
      });
      response.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
      response.on("error", () => reject(new Error("读取搜索结果失败")));
      response.on("aborted", () => reject(new Error("搜索响应中断，请重试")));
    });
    const timeout = setTimeout(() => request.destroy(new Error("搜索超时")), 45000);
    request.on("close", () => clearTimeout(timeout));
    request.on("error", error => reject(new Error(signal.aborted ? "搜索已取消" : error.message.includes("超时") ? `${names[provider]} 搜索超时` : "搜索连接失败，请检查网络或重试")));
    request.end(body);
  });
  let result: any;
  try { result = JSON.parse(data); } catch { throw new Error("搜索响应格式无效"); }
  if (!result || typeof result !== "object") throw new Error("搜索响应格式无效");
  if (provider === "anysearch" && result.code !== 0) throw new Error("AnySearch 搜索未成功，请检查账户额度或稍后重试");
  const results = provider === "anysearch" ? result.data?.results : result.results;
  if (!Array.isArray(results)) throw new Error("搜索结果缺失");
  return results.filter((r: any) => r && isPublicSearchUrl(r.url)).slice(0, 5).map((r: any) => {
    // AnySearch content 是清洗后的正文；snippet 只是摘要，仍须进入原有正文采集流程。
    const content = provider === "anysearch" ? r.content : r.raw_content;
    return { title: typeof r.title === "string" && r.title.trim() ? r.title.slice(0, 500) : "参考网页", url: r.url,
      text: typeof content === "string" ? content.slice(0, 60000) : undefined,
      publishedAt: typeof r.published_date === "string" ? r.published_date.slice(0, 100) : undefined };
  });
}
