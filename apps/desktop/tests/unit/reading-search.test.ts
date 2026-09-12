import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { EventEmitter } from "node:events";
import { configureRuntimePaths, resetRuntimePaths } from "@guizhi/core/runtime-paths";
const mock = vi.hoisted(() => ({ secure: true, status: 200, response: {} as any, body: "", headers: {} as any, url: "", calls: 0 }));
const native = vi.hoisted(() => ({ availability: vi.fn(), search: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/native-search", () => ({ nativeSearchAvailability: native.availability, searchNativeWeb: native.search, NATIVE_SEARCH_TIMEOUT_MS: 180000 }));
vi.mock("electron", () => ({ safeStorage: { isEncryptionAvailable: () => mock.secure, getSelectedStorageBackend: () => "gnome_libsecret", encryptString: (s: string) => Buffer.from("encrypted:" + s), decryptString: (b: Buffer) => b.toString().slice(10) } }));
vi.mock("../../src/main/services/network-proxy", () => ({ getHttpRequestAgent: () => undefined }));
vi.mock("node:https", () => ({ default: { request: (_url: URL, options: any, receive: (res: any) => void) => {
  mock.headers = options.headers; mock.url = _url.href; mock.calls++;
  const req: any = new EventEmitter(); req.destroy = (error: Error) => { if (error) req.emit("error", error); req.emit("close"); };
  req.end = (body: string) => { mock.body = body; queueMicrotask(() => {
    const res: any = new EventEmitter(); res.statusCode = mock.status; res.resume = () => {};
    receive(res); res.emit("data", Buffer.from(JSON.stringify(mock.response))); res.emit("end"); req.emit("close");
  }); }; return req;
} } }));
import { configureReadingSearch, readingSearchStatus, saveReadingSearchKey, searchReadingWeb } from "../../src/main/services/themed-reading/search-service";

describe("联网搜索服务与密钥边界", () => {
  let directory: string;
  beforeEach(async () => { directory = await fs.mkdtemp(path.join(os.tmpdir(), "guizhi-search-test-")); configureRuntimePaths({ userDataPath: directory }); mock.secure = true; mock.status = 200; mock.response = { results: [] }; mock.calls = 0; native.availability.mockReset().mockReturnValue({}); native.search.mockReset().mockResolvedValue([{ title: "文档", url: "https://example.org" }]); });
  afterEach(async () => { resetRuntimePaths(); await fs.rm(directory, { recursive: true, force: true }); });
  it("新旧配置默认关闭，开关保存后保留密钥且不发送搜索请求", async () => {
    expect((await readingSearchStatus()).defaultEnabled).toBe(false);
    await saveReadingSearchKey("existing-secret");
    const target = path.join(directory, ".machine/reading-search.json");
    const old = JSON.parse(await fs.readFile(target, "utf8")); delete old.defaultEnabled;
    await fs.writeFile(target, JSON.stringify(old));
    expect((await readingSearchStatus()).defaultEnabled).toBe(false);
    await configureReadingSearch({ defaultEnabled: true });
    expect(await readingSearchStatus()).toMatchObject({ defaultEnabled: true, configured: true });
    expect(JSON.parse(await fs.readFile(target, "utf8")).defaultEnabled).toBe(true);
    await configureReadingSearch({ defaultEnabled: false });
    expect(await readingSearchStatus()).toMatchObject({ defaultEnabled: false, configured: true });
    expect(mock.calls).toBe(0);
  });
  it("读取配置只返回状态，安全存储不可用不落明文", async () => {
    mock.secure = false; await saveReadingSearchKey("test-secret"); expect(await readingSearchStatus()).toMatchObject({ configured: true, persistent: false, provider: "tavily" });
    await expect(fs.readFile(path.join(directory, ".machine/reading-search.json"))).rejects.toThrow();
  });
  it("固定搜索预算，仅提取正文，不能把搜索摘要视为正文", async () => {
    await saveReadingSearchKey("test-secret");
    mock.response = { results: [{ title: "只有摘要", url: "https://example.org/1", content: "搜索摘要" }, { title: "正文", url: "https://example.org/2", raw_content: "实际取得正文" }] };
    const results = await searchReadingWeb("主题关键词", new AbortController().signal);
    expect(results[0].text).toBeUndefined(); expect(results[1].text).toBe("实际取得正文");
    expect(JSON.parse(mock.body)).toMatchObject({ max_results: 5, search_depth: "basic", auto_parameters: false, include_answer: false, include_raw_content: "markdown" });
    expect(mock.body).not.toContain("test-secret"); expect(mock.headers.Authorization).toBe("Bearer test-secret");
    expect(await readingSearchStatus()).not.toHaveProperty("apiKey");
  });
  it.each([401, 429, 502])("服务失败 %s 不回显上游内容或密钥", async status => {
    await saveReadingSearchKey("test-secret"); mock.status = status; mock.response = { error: "echo test-secret" };
    await expect(searchReadingWeb("主题", new AbortController().signal)).rejects.toThrow(`HTTP ${status}`);
  });
  it("未配置和取消时不执行搜索", async () => {
    await expect(searchReadingWeb("主题", new AbortController().signal)).rejects.toThrow("配置");
    await saveReadingSearchKey("test-secret"); const controller = new AbortController(); controller.abort();
    await expect(searchReadingWeb("主题", controller.signal)).rejects.toThrow();
    expect(mock.calls).toBe(0);
  });
  it("AnySearch 使用官方端点、鉴权和正文，不把 snippet 当正文", async () => {
    await saveReadingSearchKey("any-secret", "anysearch");
    mock.response = { code: 0, data: { results: [null, { url: "http://127.0.0.1/private" }, { url: "https://example.org/1", title: "", snippet: "搜索摘要" }, { url: "https://example.org/2", title: "正文", content: "# 实际正文" }] } };
    const results = await searchReadingWeb("  主题关键词  ", new AbortController().signal);
    expect(mock.url).toBe("https://api.anysearch.com/v1/search");
    expect(JSON.parse(mock.body)).toEqual({ query: "主题关键词", max_results: 5, format: "markdown" });
    expect(mock.body).not.toContain("any-secret"); expect(mock.headers.Authorization).toBe("Bearer any-secret");
    expect(results).toHaveLength(2); expect(results[0].text).toBeUndefined(); expect(results[0].title).toBe("参考网页"); expect(results[1].text).toBe("# 实际正文");
    expect(await readingSearchStatus()).toEqual({ defaultEnabled: false, provider: "anysearch", configured: true, persistent: true, configuredProviders: ["anysearch"] });
  });
  it("旧 Tavily 配置自动迁移；切换、清除 AnySearch 不丢失 Tavily 密钥", async () => {
    const target = path.join(directory, ".machine/reading-search.json");
    await fs.mkdir(path.dirname(target), { recursive: true });
    await fs.writeFile(target, JSON.stringify({ encrypted: Buffer.from("encrypted:legacy-secret").toString("base64") }));
    expect(await readingSearchStatus()).toMatchObject({ provider: "tavily", configured: true });
    await saveReadingSearchKey("any-secret", "anysearch");
    const stored = await fs.readFile(target, "utf8");
    expect(stored).not.toContain("legacy-secret"); expect(stored).not.toContain("any-secret");
    expect(JSON.parse(stored)).toMatchObject({ version: 2, provider: "anysearch" });
    expect((await readingSearchStatus()).configuredProviders).toEqual(["tavily", "anysearch"]);
    await configureReadingSearch({ provider: "tavily" });
    await searchReadingWeb("主题", new AbortController().signal);
    expect(mock.url).toBe("https://api.tavily.com/search"); expect(mock.headers.Authorization).toBe("Bearer legacy-secret");
    await configureReadingSearch({ provider: "anysearch", apiKey: "" });
    expect(await readingSearchStatus()).toMatchObject({ provider: "tavily", configured: true, configuredProviders: ["tavily"] });
  });
  it("候选 AnySearch 测试失败不覆盖已保存配置，业务失败不回显响应", async () => {
    await saveReadingSearchKey("working-secret");
    mock.response = { code: -1, message: "echo any-secret", data: { results: [] } };
    await expect(configureReadingSearch({ provider: "anysearch", apiKey: "any-secret", test: true })).rejects.toThrow("AnySearch 搜索未成功");
    expect(await readingSearchStatus()).toMatchObject({ provider: "tavily", configuredProviders: ["tavily"] });
    mock.response = { code: 0, data: { results: [] } };
    await expect(configureReadingSearch({ provider: "anysearch", apiKey: "any-secret", test: true })).resolves.toMatchObject({ provider: "anysearch", configured: true });
  });
  it.each([301, 401, 402, 403, 429, 502])("AnySearch HTTP %s 失败不回显内容或匿名回退", async status => {
    await saveReadingSearchKey("any-secret", "anysearch"); mock.status = status; mock.response = { message: "echo any-secret" };
    const error = await searchReadingWeb("主题", new AbortController().signal).catch(e => e);
    expect(error.message).toContain(`HTTP ${status}`); expect(error.message).not.toContain("any-secret"); expect(mock.calls).toBe(1);
  });
  it.each([null, {}, { code: 0 }, { code: 0, data: { results: {} } }])("拒绝 AnySearch 格式不完整的响应 %j", async response => {
    await saveReadingSearchKey("any-secret", "anysearch"); mock.response = response;
    await expect(searchReadingWeb("主题", new AbortController().signal)).rejects.toThrow();
  });
  it("安全存储不可用时两个服务均仅在内存，切换仍保留各自密钥", async () => {
    mock.secure = false;
    await saveReadingSearchKey("tavily-secret"); await saveReadingSearchKey("any-secret", "anysearch");
    await configureReadingSearch({ provider: "tavily" });
    expect(await readingSearchStatus()).toMatchObject({ configured: true, persistent: false, configuredProviders: ["tavily", "anysearch"] });
    await expect(fs.readFile(path.join(directory, ".machine/reading-search.json"))).rejects.toThrow();
    await configureReadingSearch({ apiKey: "" }); expect((await readingSearchStatus()).configured).toBe(false);
  });
  it("非法服务、密钥和 IPC 输入不写入配置", async () => {
    for (const input of [null, [], { provider: "unknown" }, { apiKey: 123 }, { apiKey: "secret\nheader" }, { test: "yes" }, { defaultEnabled: "yes" }]) {
      await expect(configureReadingSearch(input as any)).rejects.toThrow();
    }
    expect((await readingSearchStatus()).configured).toBe(false); expect(mock.calls).toBe(0);
  });
  it("并行保存两个服务不丢失配置", async () => {
    await Promise.all([saveReadingSearchKey("one", "tavily"), saveReadingSearchKey("two", "anysearch")]);
    expect((await readingSearchStatus()).configuredProviders).toEqual(["tavily", "anysearch"]);
  });
  it("原生搜索测试后切换，复用模型配置且保留两家搜索密钥与默认开关", async () => {
    await saveReadingSearchKey("one", "tavily"); await saveReadingSearchKey("two", "anysearch");
    await configureReadingSearch({ defaultEnabled: true });
    native.availability.mockReturnValue({ model: "gpt-6-astra" });
    expect(await configureReadingSearch({ provider: "native", test: true })).toMatchObject({ configured: true, nativeModel: "gpt-6-astra", defaultEnabled: true, configuredProviders: ["tavily", "anysearch", "native"] });
    const stored = JSON.parse(await fs.readFile(path.join(directory, ".machine/reading-search.json"), "utf8"));
    expect(stored.provider).toBe("native"); expect(Object.keys(stored.encryptedKeys)).toEqual(["tavily", "anysearch"]);
    expect(JSON.stringify(stored)).not.toContain("gpt-6-astra");
    await searchReadingWeb("研究关键词", new AbortController().signal);
    expect(native.search).toHaveBeenLastCalledWith("研究关键词", expect.any(AbortSignal), "themedReading");
    await searchReadingWeb("围绕本文提问", new AbortController().signal, "qa");
    expect(native.search).toHaveBeenLastCalledWith("围绕本文提问", expect.any(AbortSignal), "qa");
    expect(mock.calls).toBe(0);
    await configureReadingSearch({ provider: "tavily" });
    await searchReadingWeb("主题", new AbortController().signal);
    expect(mock.headers.Authorization).toBe("Bearer one");
  });
  it("原生搜索失败保留旧服务，禁止在搜索配置保存或清除模型密钥", async () => {
    await saveReadingSearchKey("working-secret");
    native.search.mockRejectedValue(new Error("模型服务未完成联网搜索"));
    await expect(configureReadingSearch({ provider: "native", test: true })).rejects.toThrow("未完成联网搜索");
    expect((await readingSearchStatus()).provider).toBe("tavily");
    for (const apiKey of ["model-secret", ""]) await expect(configureReadingSearch({ provider: "native", apiKey })).rejects.toThrow("复用主文本模型凭证");
    expect((await readingSearchStatus()).provider).toBe("tavily");
  });
  it("主文本模型不可用时不报告原生搜索已配置", async () => {
    native.availability.mockReturnValue({ error: "请先配置主文本模型" });
    await configureReadingSearch({ provider: "native" });
    expect(await readingSearchStatus()).toMatchObject({ provider: "native", configured: false, nativeUnavailableReason: "请先配置主文本模型" });
    expect(native.search).not.toHaveBeenCalled();
  });
});
