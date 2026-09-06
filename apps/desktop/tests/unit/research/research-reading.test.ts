import fs from "node:fs";
import path from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { researchFixture } from "../../helpers/research";
import { createResearchReader } from "../../../src/main/services/research/read-research";
import { downloadPlatformCaptions } from "../../../src/main/services/import/video-captions";
import type { BrowserCaptureService } from "../../../src/main/services/platform-capture/browser-capture";

const mock = vi.hoisted(() => ({ note: vi.fn(), run: vi.fn(), executable: vi.fn(), forbidden: vi.fn() }));
vi.mock("../../../src/main/services/net-safety", () => ({ resolvePublicAddress: async () => ({ address: "1.1.1.1", family: 4 }) }));
vi.mock("../../../src/main/services/platform-capture/authenticated-platforms", () => ({ fetchAuthenticatedDouyin: mock.note, fetchAuthenticatedXiaohongshu: mock.note }));
vi.mock("../../../src/main/services/import/video-url", () => ({ runCommand: mock.run, parseYtDlpMetadata: () => ({ title: "视频", description: "简介" }), extractVideoUrl: mock.forbidden }));
vi.mock("../../../src/main/services/media/ytdlp-manager", () => ({ resolveYtDlpExecutable: mock.executable, ensureYtDlp: mock.forbidden }));
vi.mock("../../../src/main/services/media/transcribe", () => ({ transcribeAudio: mock.forbidden }));

beforeEach(() => { vi.clearAllMocks(); mock.executable.mockReturnValue("installed-ytdlp"); });
describe("只读取文字材料", () => {
  it.each(["douyin", "xiaohongshu"] as const)("%s 默认及显式关闭时不请求评论、不产生评论缺失警告", async (source) => {
    mock.note.mockResolvedValue({ title: "图文", kind: "image", author: "作者", description: "原文" });
    const captureComments = vi.fn().mockRejectedValue(new Error("不应访问评论接口"));
    const read = createResearchReader({ captureComments } as unknown as BrowserCaptureService, () => null);
    const candidate = { ...researchFixture().candidates[1], source, url: source === "douyin" ? "https://www.douyin.com/video/123" : "https://www.xiaohongshu.com/explore/123" };
    for (const options of [undefined, { includeComments: false }]) {
      const doc = await read(candidate, new AbortController().signal, options);
      expect(doc.passages.some(p => p.kind === "comment")).toBe(false);
      expect(doc.warning).not.toContain("评论");
      expect(doc.passages.some(p => p.text === "原文")).toBe(true);
    }
    expect(captureComments).not.toHaveBeenCalled();
  });
  it("文案不能冒充口播；评论排序去重并限制作者，保存截断有标记", async () => {
    mock.note.mockResolvedValue({ title: "视频标题", kind: "video", author: "发布者", description: "正文".repeat(110000) });
    const captureComments = vi.fn(async () => [{ content: "观点甲", authorName: "同一作者", likeCount: 1 }, { content: "观点乙", authorName: "同一作者", likeCount: 10 }]);
    const read = createResearchReader({ captureComments } as unknown as BrowserCaptureService, () => null);
    const candidate = researchFixture().candidates[1]; candidate.url = "https://www.douyin.com/video/123";
    const doc = await read(candidate, new AbortController().signal, { includeComments: true });
    expect(doc.truncated).toBe(true);
    expect(doc.passages.reduce((n, p) => n + p.text.length, 0)).toBe(200000);
    expect(doc.passages.every((p) => p.kind === "description")).toBe(true);
    expect(doc.warning).toContain("未读取口播");
    expect(captureComments.mock.calls[0]).toHaveLength(4);
    expect(mock.run).not.toHaveBeenCalled(); expect(mock.forbidden).not.toHaveBeenCalled();
    mock.note.mockResolvedValue({ title: "图文", kind: "image", author: "作者", description: "正文" });
    const short = await read(candidate, new AbortController().signal, { includeComments: true });
    expect(short.passages.filter((p) => p.kind === "comment").map((p) => p.text)).toEqual(["观点乙"]);
  });
  it("评论失败保留已经取得的正文", async () => {
    mock.note.mockResolvedValue({ title: "图文", kind: "image", author: "作者", description: "原文" });
    const read = createResearchReader({ captureComments: async () => { throw new Error("timeout"); } } as unknown as BrowserCaptureService, () => null);
    const candidate = researchFixture().candidates[1];
    const doc = await read(candidate, new AbortController().signal, { includeComments: true });
    expect(doc.status).toBe("partial"); expect(doc.warning).toContain("评论未完整"); expect(doc.passages.some((p) => p.text === "原文")).toBe(true);
  });
  it("缺失依赖不自动安装，也不下载或转写音频", async () => {
    mock.executable.mockImplementationOnce(() => { throw new Error("依赖不可用"); });
    const read = createResearchReader({} as BrowserCaptureService, () => null);
    const candidate = researchFixture().candidates[0]; candidate.url = "https://www.bilibili.com/video/BV123456";
    const result = await read(candidate, new AbortController().signal, { includeComments: true });
    expect(result.status).toBe("failed"); expect(result.error).toContain("依赖"); expect(mock.forbidden).not.toHaveBeenCalled(); expect(mock.run).not.toHaveBeenCalled();
  });
  it("自动字幕等待完成再清理临时目录，禁止媒体下载参数", async () => {
    const directories: string[] = [];
    const run = vi.fn(async (_executable: string, args: string[]) => {
      expect(args).toContain("--skip-download"); expect(args).toContain("--ignore-config");
      expect(args).not.toContain("--extract-audio");
      const dir = path.dirname(args[args.indexOf("-o") + 1]); directories.push(dir);
      if (args.includes("--write-auto-subs")) {
        await new Promise((resolve) => setTimeout(resolve, 5));
        expect(fs.existsSync(dir)).toBe(true);
        fs.writeFileSync(path.join(dir, "caption.zh.vtt"), "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n字幕原文");
      }
    });
    const result = await downloadPlatformCaptions("installed", "https://www.bilibili.com/video/BV123", run, new AbortController().signal);
    expect(result?.source).toBe("platform-ai-captions"); expect(result?.cues?.[0].startMs).toBe(1000);
    expect(run).toHaveBeenCalledTimes(2); expect(directories.every((dir) => !fs.existsSync(dir))).toBe(true);
  });
  it("字幕取消将信号传给子进程且清理目录，不再请求自动字幕", async () => {
    const controller = new AbortController(); let directory = "";
    const run = vi.fn(async (_executable: string, args: string[], options: { signal?: AbortSignal }) => {
      directory = path.dirname(args[args.indexOf("-o") + 1]);
      expect(options.signal).toBe(controller.signal);
      controller.abort(); throw new Error("aborted child");
    });
    await expect(downloadPlatformCaptions("installed", "https://www.bilibili.com/video/BV123", run, controller.signal)).rejects.toThrow();
    expect(run).toHaveBeenCalledTimes(1); expect(fs.existsSync(directory)).toBe(false);
  });
});
