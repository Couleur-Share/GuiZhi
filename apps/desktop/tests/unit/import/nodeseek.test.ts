// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  fetchNodeseekThread,
  parseNodeseekPage,
} from "../../../src/main/services/import/nodeseek";
import {
  detectForumPlatform,
  nodeseekVerificationTarget,
} from "@guizhi/shared/utils/forum-platforms";
import { isAllowedPlatformUrl } from "@guizhi/shared/utils/platform-capture";
import { isAllowedBrowserResourceUrl } from "../../../src/main/services/platform-capture/browser-capture-policy";
import { sourceIdentity } from "../../../src/main/services/import/source-identity";
import { getAuthenticatedRetryPlatform } from "../../../src/renderer/components/imports/import-task-meta";
import type { ImportTask } from "@guizhi/shared/types";

// 元素结构取自 2026-09-27 用户指定帖的真实 DOM，正文采用合成数据。
const post = (floor: number) =>
  `<div class="content-item" id="${floor}"><a class="author-name">作者${floor}</a><span class="date-created"><time datetime="2026-09-20T10:35:55.000Z"></time></span><article class="post-content"><p>正文${floor}</p><a href="/post-123-1#2">引用</a><img src="/image.png"><script>bad()</script></article></div>`;
const page = (floors: number[], pages = 2) =>
  `<h1><a class="post-title-link" href="/post-123-1">测试帖子</a></h1>${floors.map(post).join("")}<div aria-label="pagination"><a href="/post-123-${pages}">末页</a><a href="https://evil.test/post-123-999">伪造</a><a href="/post-456-999">别的帖子</a></div>`;

describe("NodeSeek 验证后论坛采集", () => {
  it("识别实站中文验证页，并将验证定位到同帖受阻的分页", () => {
    expect(() =>
      parseNodeseekPage(
        "<html><head><title>请稍候…</title></head><body><h1>www.nodeseek.com</h1>正在进行安全验证</body></html>",
        "123",
        3,
      ),
    ).toThrow("nodeseek_verification_required");
    const source = "https://www.nodeseek.com/post-123-1";
    expect(
      nodeseekVerificationTarget(
        source,
        "[nodeseek_verification_required]（https://www.nodeseek.com/post-123-3）",
      ),
    ).toBe("https://www.nodeseek.com/post-123-3");
    expect(
      nodeseekVerificationTarget(
        source,
        "[nodeseek_verification_required] https://www.nodeseek.com/post-999-3",
      ),
    ).toBe(source);
  });
  it("识别分页链接并将来源归一到主帖", () => {
    expect(
      detectForumPlatform("https://www.nodeseek.com/post-123-8#70"),
    ).toEqual({ platform: "nodeseek", topicId: "123" });
    expect(sourceIdentity("https://nodeseek.com/post-123-8#70")).toBe(
      "https://www.nodeseek.com/post-123-1",
    );
    expect(
      detectForumPlatform("https://nodeseek.com.evil.test/post-123-1"),
    ).toBeNull();
  });
  it("只允许官方 HTTPS 和验证域，不允许内网、凭证、端口和任意子域", () => {
    for (const url of [
      "http://www.nodeseek.com/post-123-1",
      "https://127.0.0.1/",
      "https://a:b@www.nodeseek.com/",
      "https://www.nodeseek.com:8443/",
      "https://evil.nodeseek.com/",
    ]) {
      expect(isAllowedPlatformUrl("nodeseek", url)).toBe(false);
      expect(isAllowedBrowserResourceUrl("nodeseek", url)).toBe(false);
    }
    expect(
      isAllowedBrowserResourceUrl(
        "nodeseek",
        "https://challenges.cloudflare.com/turnstile/v0/api.js",
      ),
    ).toBe(true);
    expect(
      isAllowedBrowserResourceUrl(
        "nodeseek",
        "https://evil.challenges.cloudflare.com/",
      ),
    ).toBe(false);
  });
  it("保留主楼、作者与真实时间，转换相对链接并移除脚本", () => {
    const parsed = parseNodeseekPage(page([0, 1]), "123");
    expect(parsed.pageCount).toBe(2);
    expect(parsed.posts[0]).toMatchObject({
      floor: 0,
      author: "作者0",
      createdAt: Date.parse("2026-09-20T10:35:55.000Z"),
    });
    expect(parsed.posts[0].content).toContain(
      "https://www.nodeseek.com/post-123-1#2",
    );
    expect(parsed.posts[0].content).not.toContain("bad()");
  });
  it("自动翻页并按楼层去重，置顶回复不会重复入库", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(page([0, 19, 1]))
      .mockResolvedValueOnce(page([2, 19]));
    const thread = await fetchNodeseekThread("123", fetch);
    expect(fetch.mock.calls.map((call) => call[0])).toEqual([
      "https://www.nodeseek.com/post-123-1",
      "https://www.nodeseek.com/post-123-2",
    ]);
    expect(thread.replies.map((reply) => reply.floor)).toEqual([1, 2, 19]);
    expect(thread.replyCount).toBe(3);
    expect(thread.content).toContain("正文0");
  });
  it("验证页和空白页不能被识别为主楼或成功结果", () => {
    expect(() =>
      parseNodeseekPage("<title>Just a moment...</title>", "123"),
    ).toThrow("nodeseek_verification_required");
    expect(() => parseNodeseekPage(page([]), "123")).toThrow("未加载");
  });
  it("中途验证失效会失败，不能将首页当成完整结果", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(page([0, 1]))
      .mockResolvedValueOnce("<title>Just a moment...</title>");
    await expect(fetchNodeseekThread("123", fetch)).rejects.toThrow(
      "nodeseek_verification_required",
    );
  });
  it("取消后不再请求下一页", async () => {
    const controller = new AbortController();
    const fetch = vi.fn(async () => {
      controller.abort();
      return page([0, 1]);
    });
    await expect(
      fetchNodeseekThread("123", fetch, controller.signal),
    ).rejects.toThrow();
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("旧版本 403 和已验证但过期的任务都可再次验证", () => {
    const task = {
      sourceKind: "url",
      sourceInput: "https://www.nodeseek.com/post-123-1",
      status: "failed",
      error: "网站拒绝访问或限制请求频率",
    } as ImportTask;
    expect(getAuthenticatedRetryPlatform(task)).toBe("nodeseek");
    expect(
      getAuthenticatedRetryPlatform({
        ...task,
        captureStrategy: "authenticated",
        error: "[nodeseek_verification_required]",
      }),
    ).toBe("nodeseek");
    expect(
      getAuthenticatedRetryPlatform({ ...task, error: "无法解析域名" }),
    ).toBeNull();
  });
});
