import { describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

// douyin → safe-fetch → network-proxy 引用 electron，单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  buildImageNoteEntry,
  OCR_IMAGE_LIMIT,
} from "../../../src/main/services/import/image-note-entry";
import {
  douyinImageNoteSource,
  type DouyinAweme,
} from "../../../src/main/services/import/douyin";
import { xiaohongshuImageNoteSource } from "../../../src/main/services/import/xiaohongshu";

const OCR_CONFIG = {
  apiUrl: "https://api.openai.com",
  apiKey: "sk-test",
  model: "gpt-4o",
};

/** 默认关掉 AI 拟题，避免用例去读真实的 ai-config.json */
const NO_TITLE = { getTitleConfig: () => null };

function buildAweme(imageCount: number): DouyinAweme {
  return {
    awemeId: "7648655441894886691",
    kind: "note",
    title: "我用这套方法做了一个生产级 RAG 系统。",
    description: "我用这套方法做了一个生产级 RAG 系统。\n1. 评测先行。",
    author: "mHe",
    durationSeconds: 0,
    playUrl: null,
    imageMirrors: Array.from({ length: imageCount }, (_, index) => [
      `https://p1.douyinpic.com/${index}.webp`,
      `https://p9.douyinpic.com/${index}.webp`,
    ]),
    webpageUrl: "https://www.douyin.com/note/7648655441894886691",
  };
}

/** 抖音图文素材：本文件的大部分用例与平台无关，用它当代表 */
function buildSource(imageCount: number) {
  return douyinImageNoteSource(buildAweme(imageCount));
}

/** 下载桩：写一个真实临时文件，让资产落盘与清理链路都跑到 */
function fakeDownload(onCall?: (mirrors: string[]) => void) {
  const dirs: string[] = [];
  const download = async (mirrors: string[]) => {
    onCall?.(mirrors);
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-note-test-"));
    dirs.push(dir);
    const filePath = path.join(dir, "image.webp");
    fs.writeFileSync(filePath, "fake-image");
    return { dir, filePath };
  };
  return { download, dirs };
}

describe("buildImageNoteEntry", () => {
  it("配图入资产库并嵌进正文，条目类型为 image", async () => {
    const { download, dirs } = fakeDownload();
    const saved: string[] = [];
    const entry = await buildImageNoteEntry(buildSource(2), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async (filePath) => {
        expect(fs.existsSync(filePath)).toBe(true);
        const name = `import-asset${saved.length}.webp`;
        saved.push(name);
        return name;
      },
      getOcrConfig: () => null,
    });

    expect(entry.itemType).toBe("image");
    expect(entry.sourceUri).toBe(
      "https://www.douyin.com/note/7648655441894886691",
    );
    expect(entry.content).toContain("图文 2 张");
    expect(entry.content).toContain("![图 1](local-image://import-asset0.webp)");
    expect(entry.content).toContain("![图 2](local-image://import-asset1.webp)");
    // 文案照常保留，且行首序号不被 Markdown 吃掉
    expect(entry.content).toContain("1\\. 评测先行。");
    // 临时目录用完即清
    expect(dirs.filter((dir) => fs.existsSync(dir))).toEqual([]);
  });

  it("配了视觉模型 → 逐张识别，结果按图号分节写进正文", async () => {
    const { download } = fakeDownload();
    const recognized: string[] = [];
    const entry = await buildImageNoteEntry(buildSource(2), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => `asset-${recognized.length}.webp`,
      getOcrConfig: () => OCR_CONFIG,
      recognize: async (filePath) => {
        recognized.push(filePath);
        return `第 ${recognized.length} 张图里的文字`;
      },
    });

    expect(recognized).toHaveLength(2);
    expect(entry.content).toContain("## 图中文字");
    expect(entry.content).toContain("### 图 1");
    expect(entry.content).toContain("第 1 张图里的文字");
    expect(entry.content).toContain("### 图 2");
    expect(entry.content).toContain("第 2 张图里的文字");
  });

  it("单张图时不再拆图号小标题", async () => {
    const { download } = fakeDownload();
    const entry = await buildImageNoteEntry(buildSource(1), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => OCR_CONFIG,
      recognize: async () => "图里的文字",
    });
    expect(entry.content).toContain("## 图中文字");
    expect(entry.content).not.toContain("### 图 1");
    expect(entry.content).toContain("图里的文字");
  });

  it("未配置视觉模型 → 图片照常入库，正文给出可读提示", async () => {
    const { download } = fakeDownload();
    const entry = await buildImageNoteEntry(buildSource(1), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => null,
      recognize: async () => {
        throw new Error("未配置时不应发起识别");
      },
    });
    expect(entry.content).toContain("![图 1](local-image://asset.webp)");
    expect(entry.content).toContain("未配置「视觉模型」");
    expect(entry.content).not.toContain("## 图中文字");
  });

  it("超过张数上限 → 全部入库，只识别前 N 张并注明", async () => {
    const { download } = fakeDownload();
    let index = 0;
    const entry = await buildImageNoteEntry(buildSource(OCR_IMAGE_LIMIT + 2), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => `asset-${index++}.webp`,
      getOcrConfig: () => OCR_CONFIG,
      recognize: async () => "文字",
    });

    const embedded = entry.content.match(/!\[图 \d+\]/g) ?? [];
    expect(embedded).toHaveLength(OCR_IMAGE_LIMIT + 2);
    expect(entry.content).toContain(`仅识别了前 ${OCR_IMAGE_LIMIT} 张`);
    expect(entry.content.match(/### 图 \d+/g) ?? []).toHaveLength(
      OCR_IMAGE_LIMIT,
    );
  });

  it("单张图下载失败 → 其余图片与文案照常入库，失败原因写进正文", async () => {
    const { download } = fakeDownload();
    let call = 0;
    const entry = await buildImageNoteEntry(buildSource(2), {
      ...NO_TITLE,
      downloadImage: async (mirrors) => {
        call += 1;
        if (call === 1) {
          throw new Error("HTTP 403");
        }
        return download(mirrors);
      },
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => null,
    });

    expect(entry.content).toContain("第 1 张图下载失败：HTTP 403");
    expect(entry.content).toContain("![图 2](local-image://asset.webp)");
    expect(entry.content).toContain("生产级 RAG 系统");
  });

  it("单张图识别失败 → 其余结果照常保留，正文注明失败张数", async () => {
    const { download } = fakeDownload();
    let index = 0;
    const entry = await buildImageNoteEntry(buildSource(2), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => `asset-${index++}.webp`,
      getOcrConfig: () => OCR_CONFIG,
      recognize: async (filePath) => {
        if (filePath.includes("asset-0")) {
          throw new Error("HTTP 429");
        }
        return "第二张的文字";
      },
    });

    expect(entry.content).toContain("### 图 2");
    expect(entry.content).toContain("第二张的文字");
    expect(entry.content).not.toContain("### 图 1");
    expect(entry.content).toContain("有 1 张图的文字识别失败（HTTP 429）");
  });

  it("识别全军覆没 → 正文如实交代原因，不静默留白", async () => {
    const { download } = fakeDownload();
    const entry = await buildImageNoteEntry(buildSource(2), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => OCR_CONFIG,
      recognize: async () => {
        throw new Error("OCR 请求失败 (HTTP 429): insufficient_user_quota");
      },
    });

    expect(entry.content).toContain(
      "图中文字识别失败：OCR 请求失败 (HTTP 429): insufficient_user_quota",
    );
    expect(entry.content).toContain("可在详情页点「识别图中文字」重试");
    expect(entry.content).not.toContain("## 图中文字");
    // 图片与文案照常入库
    expect(entry.content).toContain("![图 1](local-image://asset.webp)");
    expect(entry.content).toContain("生产级 RAG 系统");
  });

  it("取消会中断整条采集，不留下半成品条目", async () => {
    const controller = new AbortController();
    await expect(
      buildImageNoteEntry(
        buildSource(2),
        {
          downloadImage: async () => {
            controller.abort();
            throw new Error("已取消");
          },
          getOcrConfig: () => null,
        },
        controller.signal,
      ),
    ).rejects.toThrow("已取消");
  });

  it("配了文本模型 → AI 拟题替换文案首行，素材含图中文字", async () => {
    const { download } = fakeDownload();
    let material = "";
    const entry = await buildImageNoteEntry(buildSource(1), {
      downloadImage: download,
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => OCR_CONFIG,
      recognize: async () => "图里的指标表：Recall@5 0.175 → 0.600",
      getTitleConfig: () => ({
        provider: "openai",
        apiProtocol: "openai",
        apiKey: "sk-test",
        apiUrl: "https://api.openai.com",
        model: "main-model",
      }),
      generateTitle: async (source) => {
        material = source;
        return "手写混合检索 RAG 系统的工程实践与七条原则";
      },
    });

    expect(entry.title).toBe("手写混合检索 RAG 系统的工程实践与七条原则");
    expect(material).toContain("生产级 RAG 系统");
    // 图中文字也是拟题素材，否则标题只能反映文案
    expect(material).toContain("Recall@5");
  });

  it("拟题失败或模型没给出标题 → 退回文案首行，不阻断导入", async () => {
    const { download } = fakeDownload();
    const titleConfig = {
      provider: "openai",
      apiProtocol: "openai" as const,
      apiKey: "sk-test",
      apiUrl: "https://api.openai.com",
      model: "main-model",
    };
    const base = {
      ...{ downloadImage: download },
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => null,
      getTitleConfig: () => titleConfig,
    };

    const failed = await buildImageNoteEntry(buildSource(1), {
      ...base,
      generateTitle: async () => {
        throw new Error("HTTP 500");
      },
    });
    expect(failed.title).toBe("我用这套方法做了一个生产级 RAG 系统。");

    const empty = await buildImageNoteEntry(buildSource(1), {
      ...base,
      generateTitle: async () => null,
    });
    expect(empty.title).toBe("我用这套方法做了一个生产级 RAG 系统。");
  });

  it("未配置文本模型 → 不发拟题请求", async () => {
    const { download } = fakeDownload();
    const entry = await buildImageNoteEntry(buildSource(1), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => null,
      generateTitle: async () => {
        throw new Error("未配置时不应拟题");
      },
    });
    expect(entry.title).toBe("我用这套方法做了一个生产级 RAG 系统。");
  });

  it("按序上报子阶段", async () => {
    const { download } = fakeDownload();
    const stages: string[] = [];
    await buildImageNoteEntry(buildSource(1), {
      ...NO_TITLE,
      downloadImage: download,
      saveAsset: async () => "asset.webp",
      getOcrConfig: () => OCR_CONFIG,
      recognize: async () => "文字",
      onStage: (stage) => stages.push(stage),
    });
    expect(stages).toEqual(["image-download", "image-ocr"]);
  });

  it("小红书笔记有作者写的标题 → 照用，不去 AI 重拟", async () => {
    const { download } = fakeDownload();
    const entry = await buildImageNoteEntry(
      xiaohongshuImageNoteSource({
        noteId: "6a59e7f3000000000301fc49",
        kind: "note",
        title: "AI漫剧培训实战课程丨12天独立出片",
        authoredTitle: true,
        description: "司晨视觉AI漫剧实战班，带你用AI工具快速产出高质量剧情漫剧。",
        author: "司晨视觉",
        durationSeconds: null,
        playUrls: [],
        imageMirrors: [["https://sns-webpic-qc.xhscdn.com/a"]],
        webpageUrl:
          "https://www.xiaohongshu.com/explore/6a59e7f3000000000301fc49",
      }),
      {
        downloadImage: download,
        saveAsset: async () => "asset.jpg",
        getOcrConfig: () => null,
        getTitleConfig: () => {
          throw new Error("原标题是人写的，不该再去解析拟题模型");
        },
      },
    );

    expect(entry.title).toBe("AI漫剧培训实战课程丨12天独立出片");
    expect(entry.content).toContain("平台：小红书 · 作者：司晨视觉 · 图文 1 张");
    expect(entry.sourceUri).toBe(
      "https://www.xiaohongshu.com/explore/6a59e7f3000000000301fc49",
    );
  });
});
