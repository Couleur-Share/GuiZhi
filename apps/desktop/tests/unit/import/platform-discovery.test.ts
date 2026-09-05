import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";

const { createContext } = vi.hoisted(() => ({ createContext: vi.fn() }));
vi.mock("../../../src/main/services/platform-capture/electron-capture-runtime", () => ({ createElectronCaptureContext: createContext }));
import { BrowserCaptureService, scanPlatformDiscoveryPayloads } from "../../../src/main/services/platform-capture/browser-capture";
import { readDiscoveryPage, submitXhsSearch, waitForXhsSearch } from "../../../src/main/services/platform-capture/discovery-page";

const dirs: string[] = [];
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  for (const dir of dirs.splice(0)) fs.rmSync(dir, { recursive: true, force: true });
  document.body.innerHTML = "";
  document.title = "";
});
function fixture(platform: "douyin" | "xiaohongshu" = "douyin") {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-discovery-test-"));
  dirs.push(dir);
  fs.mkdirSync(path.join(dir, "browser-capture"));
  fs.writeFileSync(path.join(dir, "browser-capture/session-status.json"), JSON.stringify({ version: 3, [platform]: true }));
  const payloads: unknown[] = [];
  const page = {
    setDefaultNavigationTimeout: vi.fn(), setDefaultTimeout: vi.fn(), goto: vi.fn().mockResolvedValue(undefined),
    url: () => "https://www.douyin.com/search/test", isClosed: () => false,
    startJsonCapture: vi.fn(() => payloads), stopJsonCapture: vi.fn(), reload: vi.fn(), scrollBy: vi.fn(),
    waitForTimeout: (ms: number) => new Promise((resolve) => setTimeout(resolve, ms)),
    evaluate: vi.fn().mockResolvedValue({ cards: [], payloads: [], empty: false, verification: false, loginRequired: false }),
  };
  const close = vi.fn().mockResolvedValue(undefined);
  createContext.mockResolvedValue({ page, close, cookies: async () => [{ name: platform === "douyin" ? "sessionid" : "web_session", value: "fixture", domain: `.${platform}.com`, path: "/" }] });
  return { service: new BrowserCaptureService({ userDataPath: dir }), page, close, payloads };
}
describe("平台发现的首屏等待与空结果", () => {
  it("首批搜索响应晚于五秒仍会收集，忽略 DOM 与 SSR 推荐流", async () => {
    vi.useFakeTimers();
    const { service, page, payloads } = fixture();
    const started = Date.now();
    page.evaluate.mockImplementation(async () => {
      if (Date.now() - started >= 8000 && payloads.length === 0) payloads.push({ data: [1, 2].map((id) => ({ aweme_info: { aweme_id: String(id), desc: `test ${id}`, video: {}, author: { aweme_id: "recommended", desc: "作者推荐" } } })), recommend: [{ aweme_id: "unrelated", desc: "无关推荐" }] });
      return { cards: [{ href: "https://www.douyin.com/search/test?modal_id=feed", title: "推荐流" }], payloads: [{ aweme_id: "ssr-feed", desc: "缓存推荐" }], empty: false, verification: false };
    });
    const progress = vi.fn();
    const resolved = vi.fn();
    const pending = service.search({ platform: "douyin", keyword: "test", limit: 1 }, undefined, progress).then((value) => { resolved(); return value; });
    await vi.advanceTimersByTimeAsync(6000);
    expect(resolved).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(3000);
    expect((await pending).items[0]).toMatchObject({ externalId: "1", url: "https://www.douyin.com/video/1" });
    expect(progress).toHaveBeenCalledWith("正在等待平台返回搜索结果");
    expect(page.startJsonCapture).toHaveBeenCalledWith("douyin", { keyword: "test" });
  });
  it("一直空白不能标成成功；搜索响应明确为空才返回零结果", async () => {
    vi.useFakeTimers();
    const { service, payloads } = fixture();
    const pending = expect(service.search({ platform: "douyin", keyword: "test" })).rejects.toMatchObject({ code: "platform_changed" });
    await vi.advanceTimersByTimeAsync(91_000);
    await pending;
    payloads.push({ status_code: 0, data: [] });
    await expect(service.search({ platform: "douyin", keyword: "test" })).resolves.toEqual({ items: [], cursor: null, hasMore: false });
  });
  it("小红书从首页搜索框提交，失效登录明确提示而不导航搜索深链", async () => {
    vi.useFakeTimers();
    const { service, page, payloads } = fixture("xiaohongshu");
    page.url = () => "https://www.xiaohongshu.com/explore";
    page.evaluate.mockImplementation(async (fn, arg) => {
      if (fn === submitXhsSearch) {
        expect(arg).toBe("GPT-6");
        payloads.push({ success: true, data: { items: [] } });
        return "submitted";
      }
      return { cards: [], payloads: [], empty: false, verification: false };
    });
    await expect(service.search({ platform: "xiaohongshu", keyword: "GPT-6" })).resolves.toMatchObject({ items: [] });
    expect(page.goto).toHaveBeenCalledWith("https://www.xiaohongshu.com/explore", expect.anything());
    page.evaluate.mockResolvedValue("login_required");
    const rejected = expect(service.search({ platform: "xiaohongshu", keyword: "GPT-6" })).rejects.toMatchObject({ code: "login_required" });
    await vi.advanceTimersByTimeAsync(3000);
    await rejected;
  });
  it("小红书恢复账号期间的访客占位不会被误判为登录失效", async () => {
    vi.useFakeTimers();
    const { page } = fixture("xiaohongshu");
    const started = Date.now();
    page.evaluate.mockImplementation(async () => Date.now() - started < 1500 ? "login_required" : "submitted");
    const result = waitForXhsSearch(page as never, "GPT-6", () => {});
    await vi.advanceTimersByTimeAsync(2000);
    await expect(result).resolves.toBe("submitted");
  });
  it("通过原生输入事件与官方搜索按钮提交关键词", () => {
    document.body.innerHTML = '<div><input id="search-input" placeholder="搜索小红书"><div class="search-icon"></div></div>';
    const input = document.querySelector<HTMLInputElement>("input")!;
    const onInput = vi.fn();
    const onClick = vi.fn(() => expect(input.value).toBe("GPT-6"));
    input.addEventListener("input", onInput);
    document.querySelector(".search-icon")!.addEventListener("click", onClick);
    expect(submitXhsSearch("GPT-6")).toBe("submitted");
    expect(onInput).toHaveBeenCalledOnce();
    expect(onClick).toHaveBeenCalledOnce();
    input.placeholder = "登录探索更多内容";
    expect(submitXhsSearch("GPT-6")).toBe("login_required");
    expect(onClick).toHaveBeenCalledOnce();
  });
  it("安全验证与导航超时分别反馈，不归为零结果", async () => {
    const { service, page } = fixture();
    page.evaluate.mockResolvedValue({ cards: [], payloads: [], empty: false, verification: true });
    await expect(service.search({ platform: "douyin", keyword: "test" })).rejects.toMatchObject({ code: "verification_required" });
    page.goto.mockRejectedValue(new Error("页面导航超时"));
    await expect(service.search({ platform: "douyin", keyword: "test" })).rejects.toMatchObject({ code: "navigation_timeout" });
  });
  it("正文空白的验证码中间页立即识别，不等待九十秒", async () => {
    const { service, page } = fixture();
    document.title = "验证码中间页";
    const snapshot = readDiscoveryPage({ searchOnly: true });
    expect(snapshot.verification).toBe(true);
    page.evaluate.mockResolvedValue(snapshot);
    await expect(service.search({ platform: "douyin", keyword: "GPT-6" })).rejects.toMatchObject({ code: "verification_required" });
    expect(page.scrollBy).not.toHaveBeenCalled();
    document.title = "讨论验证码的视频 - 抖音";
    expect(readDiscoveryPage({ searchOnly: true }).verification).toBe(false);
  });
  it("已有登录 Cookie 时搜索验证仍保持窗口，直到匹配关键词的结果返回", async () => {
    vi.useFakeTimers();
    const { service, page, payloads, close } = fixture();
    const snapshot = { cards: [], payloads: [], empty: false, verification: true, loginRequired: false };
    page.evaluate.mockResolvedValue(snapshot);
    const completed = vi.fn();
    const result = service.login("douyin", false, undefined, "GPT-6").then((value) => { completed(); return value; });
    await vi.advanceTimersByTimeAsync(3000);
    expect(completed).not.toHaveBeenCalled();
    expect(close).not.toHaveBeenCalled();
    expect(page.goto).toHaveBeenCalledWith("https://www.douyin.com/search/GPT-6", expect.anything());
    expect(page.goto).toHaveBeenCalledTimes(1);
    expect(page.startJsonCapture).not.toHaveBeenCalled();
    snapshot.verification = false;
    payloads.push({ status_code: 10001, data: [] });
    await vi.advanceTimersByTimeAsync(2000);
    expect(completed).not.toHaveBeenCalled();
    expect(page.startJsonCapture).toHaveBeenCalledWith("douyin", { keyword: "GPT-6" });
    expect(page.reload).toHaveBeenCalledOnce();
    snapshot.verification = true;
    await vi.advanceTimersByTimeAsync(1000);
    expect(page.stopJsonCapture).toHaveBeenCalledOnce();
    expect(completed).not.toHaveBeenCalled();
    snapshot.verification = false;
    await vi.advanceTimersByTimeAsync(1000);
    payloads.push({ status_code: 0, data: [] });
    await vi.advanceTimersByTimeAsync(1000);
    await expect(result).resolves.toMatchObject({ loggedIn: true });
    expect(close).toHaveBeenCalledOnce();
    expect(createContext).toHaveBeenCalledWith(expect.objectContaining({ visible: true }));
  });
  it("读取 SSR 数据，并合并小红书搜索卡片外层 ID 与访问令牌", () => {
    document.body.innerHTML = '<script id="RENDER_DATA" type="application/json"></script>';
    document.getElementById("RENDER_DATA")!.textContent = encodeURIComponent(JSON.stringify({ aweme: { awemeId: "123", desc: "SSR 视频", video: {}, createTime: 1720000000 } }));
    expect(scanPlatformDiscoveryPayloads("douyin", readDiscoveryPage().payloads)[0]).toMatchObject({ externalId: "123", publishedAt: 1720000000000 });
    const items = scanPlatformDiscoveryPayloads("xiaohongshu", [{ data: { items: [{ id: "note-1", xsec_token: "test-token", note_card: { display_title: "搜索笔记", user: { nickname: "作者" }, interact_info: { liked_count: "88" } } }] } }]);
    expect(items[0]).toMatchObject({ externalId: "note-1", title: "搜索笔记", author: "作者", engagement: { likes: 88 } });
    expect(new URL(items[0].url).searchParams.get("xsec_token")).toBe("test-token");
  });
});
