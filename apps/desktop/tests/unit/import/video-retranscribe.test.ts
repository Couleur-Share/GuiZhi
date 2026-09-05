import { describe, expect, it, vi } from "vitest";
import fs from "fs";
import path from "path";

vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  downloadVideoAudio,
  type VideoUrlDeps,
} from "../../../src/main/services/import/video-url";
import type { XiaohongshuNote } from "../../../src/main/services/import/xiaohongshu";

const sourceUrl =
  "https://www.xiaohongshu.com/explore/123?xsec_token=test-token";
const videoNote: XiaohongshuNote = {
  noteId: "123",
  kind: "video",
  title: "中文视频",
  authoredTitle: true,
  description: "视频简介",
  author: "作者",
  durationSeconds: 60,
  playUrls: [
    "https://sns-video-bd.xhscdn.com/main.mp4",
    "https://sns-video-hw.xhscdn.com/backup.mp4",
  ],
  imageMirrors: [],
  webpageUrl: "https://www.xiaohongshu.com/explore/123",
};

function xhsDeps(note = videoNote) {
  return {
    // 重转写小红书不应查询 yt-dlp 安装状态，更不能调用它。
    getYtDlpPath: vi.fn(() => {
      throw new Error("不应读取 yt-dlp 配置");
    }),
    run: vi.fn(async () => {
      throw new Error("不应调用 yt-dlp");
    }),
    fetchXiaohongshu: vi.fn(async () => note),
    downloadXiaohongshu: vi.fn(async () => ({
      dir: "test-media",
      filePath: "test-media/xiaohongshu.mp4",
    })),
  } satisfies VideoUrlDeps;
}

describe("已有视频重新下载音轨", () => {
  it("小红书复用专用解析与主备源下载，并保留分享令牌和取消信号", async () => {
    const deps = xhsDeps();
    const signal = new AbortController().signal;
    const audio = await downloadVideoAudio(
      sourceUrl,
      "xiaohongshu",
      deps,
      signal,
    );
    expect(deps.fetchXiaohongshu).toHaveBeenCalledWith(sourceUrl, signal);
    expect(deps.downloadXiaohongshu).toHaveBeenCalledWith(
      videoNote.playUrls,
      signal,
    );
    expect(audio.filePath).toBe("test-media/xiaohongshu.mp4");
    expect(deps.getYtDlpPath).not.toHaveBeenCalled();
    expect(deps.run).not.toHaveBeenCalled();
  });

  it("图文笔记明确拒绝转写，不下载图片或执行 OCR", async () => {
    const deps = xhsDeps({
      ...videoNote,
      kind: "note",
      playUrls: [],
      imageMirrors: [["https://example.com/image.jpg"]],
    });
    const downloadImage = vi.fn(async () => {
      throw new Error("不应下载图片");
    });
    await expect(
      downloadVideoAudio(sourceUrl, "xiaohongshu", {
        ...deps,
        imageNote: { downloadImage },
      }),
    ).rejects.toThrow("图文笔记没有音轨");
    expect(downloadImage).not.toHaveBeenCalled();
    expect(deps.downloadXiaohongshu).not.toHaveBeenCalled();
    expect(deps.run).not.toHaveBeenCalled();
  });

  it("视频没有播放地址时保留明确原因，不回退到 yt-dlp", async () => {
    const deps = xhsDeps({ ...videoNote, playUrls: [] });
    await expect(
      downloadVideoAudio(sourceUrl, "xiaohongshu", deps),
    ).rejects.toThrow("未能取到视频播放地址");
    expect(deps.downloadXiaohongshu).not.toHaveBeenCalled();
    expect(deps.run).not.toHaveBeenCalled();
  });

  it("解析失败保留原始原因，不使用另一解析器掩盖错误", async () => {
    const deps = xhsDeps();
    deps.fetchXiaohongshu.mockRejectedValue(
      new Error("小红书拒绝访问（HTTP 403）"),
    );
    await expect(
      downloadVideoAudio(sourceUrl, "xiaohongshu", deps),
    ).rejects.toThrow("小红书拒绝访问（HTTP 403）");
    expect(deps.downloadXiaohongshu).not.toHaveBeenCalled();
    expect(deps.run).not.toHaveBeenCalled();
  });

  it("下载失败照常上报，不回退到 yt-dlp", async () => {
    const deps = xhsDeps();
    deps.downloadXiaohongshu.mockRejectedValue(new Error("视频主备源均不可用"));
    await expect(
      downloadVideoAudio(sourceUrl, "xiaohongshu", deps),
    ).rejects.toThrow("视频主备源均不可用");
    expect(deps.run).not.toHaveBeenCalled();
  });

  it("抖音仍从分享页解析无水印视频地址", async () => {
    const downloadDouyin = vi.fn(async () => ({
      dir: "test-media",
      filePath: "test-media/douyin.mp4",
    }));
    await downloadVideoAudio("https://www.douyin.com/video/123", "douyin", {
      ...xhsDeps(),
      fetchDouyin: async () => ({
        awemeId: "123",
        kind: "video",
        title: "视频",
        description: "",
        author: "作者",
        durationSeconds: 60,
        imageMirrors: [],
        playUrl: "https://example.com/douyin.mp4",
        webpageUrl: "https://www.douyin.com/video/123",
      }),
      downloadDouyin,
    });
    expect(downloadDouyin).toHaveBeenCalledWith(
      "https://example.com/douyin.mp4",
      undefined,
    );
  });

  it.each([
    ["bilibili", "https://www.bilibili.com/video/BV1xx411c7mD"],
    ["youtube", "https://www.youtube.com/watch?v=example"],
  ] as const)(
    "%s 仍只调用一次 yt-dlp 下载，不重复请求元数据",
    async (platform, url) => {
      const run = vi.fn(async (_executable: string, args: string[]) => {
        const template = args[args.indexOf("-o") + 1];
        const output = template.replace(".%(ext)s", ".m4a");
        fs.mkdirSync(path.dirname(output), { recursive: true });
        fs.writeFileSync(output, "fake-audio");
        return { stdout: "" };
      });
      const audio = await downloadVideoAudio(url, platform, {
        getYtDlpPath: () => "test-yt-dlp",
        run,
      });
      try {
        expect(run).toHaveBeenCalledTimes(1);
        expect(run.mock.calls[0][1]).toContain(url);
        expect(run.mock.calls[0][1]).not.toContain("--dump-json");
        expect(fs.readFileSync(audio.filePath, "utf8")).toBe("fake-audio");
      } finally {
        fs.rmSync(audio.dir, { recursive: true, force: true });
      }
    },
  );
});
