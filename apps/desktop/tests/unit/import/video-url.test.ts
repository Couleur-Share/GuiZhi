import { describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";

// transcribe 服务链路引用 electron（network-proxy），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  buildVideoContent,
  detectVideoPlatform,
  extractVideoUrl,
  formatDuration,
  parseYtDlpMetadata,
  stripTranscriptionNote,
  YtDlpNotFoundError,
  type RunCommand,
} from "../../../src/main/services/import/video-url";

const SAMPLE_METADATA = {
  title: "测试视频标题",
  uploader: "测试UP主",
  duration: 305,
  description: "这是简介",
  webpage_url: "https://www.bilibili.com/video/BV1xx411c7mD",
};

describe("detectVideoPlatform", () => {
  it("识别主流平台视频链接", () => {
    expect(
      detectVideoPlatform("https://www.bilibili.com/video/BV1xx411c7mD"),
    ).toBe("bilibili");
    expect(detectVideoPlatform("https://b23.tv/abc123")).toBe("bilibili");
    expect(
      detectVideoPlatform("https://www.youtube.com/watch?v=dQw4w9WgXcQ"),
    ).toBe("youtube");
    expect(detectVideoPlatform("https://youtu.be/dQw4w9WgXcQ")).toBe("youtube");
    expect(
      detectVideoPlatform("https://www.youtube.com/shorts/abcdef"),
    ).toBe("youtube");
    expect(detectVideoPlatform("https://v.douyin.com/xyz/")).toBe("douyin");
    expect(
      detectVideoPlatform("https://www.xiaohongshu.com/explore/123"),
    ).toBe("xiaohongshu");
  });

  it("普通网页与非视频路径不误判", () => {
    expect(detectVideoPlatform("https://example.com/article")).toBeNull();
    expect(detectVideoPlatform("https://www.bilibili.com/read/cv123")).toBeNull();
    expect(detectVideoPlatform("https://www.youtube.com/@channel")).toBeNull();
    expect(detectVideoPlatform("not-a-url")).toBeNull();
  });
});

describe("parseYtDlpMetadata / formatDuration", () => {
  it("解析 dump-json 输出（容忍噪声行）", () => {
    const stdout = `WARNING: something\n${JSON.stringify(SAMPLE_METADATA)}\n`;
    const metadata = parseYtDlpMetadata(stdout);
    expect(metadata.title).toBe("测试视频标题");
    expect(metadata.uploader).toBe("测试UP主");
    expect(metadata.durationSeconds).toBe(305);
    expect(metadata.webpageUrl).toContain("bilibili.com");
  });

  it("无 JSON 输出时报错", () => {
    expect(() => parseYtDlpMetadata("no json here")).toThrow("元数据");
  });

  it("时长格式化", () => {
    expect(formatDuration(305)).toBe("5:05");
    expect(formatDuration(3721)).toBe("1:02:01");
  });
});

describe("extractVideoUrl", () => {
  const url = "https://www.bilibili.com/video/BV1xx411c7mD";

  /** 元数据返回样例 JSON；音频下载则按输出模板写一个假音频文件 */
  const buildAudioAwareRun = (): RunCommand => async (_exe, args) => {
    const outputIndex = args.indexOf("-o");
    if (outputIndex >= 0) {
      const template = args[outputIndex + 1];
      const filePath = template.replace(".%(ext)s", ".m4a");
      fs.mkdirSync(path.dirname(filePath), { recursive: true });
      fs.writeFileSync(filePath, "fake-audio");
      return { stdout: "" };
    }
    return { stdout: JSON.stringify(SAMPLE_METADATA) };
  };

  /** 预处理注入：不转码原样透传 */
  const passthroughPrepareAudio = async (filePath: string) => ({
    filePath,
    cleanup: () => {},
  });

  it("yt-dlp 未安装 → 降级并附安装指引，不登记来源 URI", async () => {
    const run: RunCommand = async () => {
      throw new YtDlpNotFoundError();
    };
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toContain("yt-dlp");
    expect(extracted.degradedReason).toContain("重试");
    expect(extracted.itemType).toBe("video");
    // 降级不入库，sourceUri 必须为空，否则该链接会被空壳占住判重
    expect(extracted.sourceUri).toBeNull();
  });

  it("未配置转写模型 → 仅保存元数据并附提示", async () => {
    const run: RunCommand = async () => ({
      stdout: JSON.stringify(SAMPLE_METADATA),
    });
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toBeUndefined();
    expect(extracted.title).toBe("测试视频标题");
    expect(extracted.transcript).toBeNull();
    expect(extracted.content).toContain("测试UP主");
    expect(extracted.content).toContain("5:05");
    expect(extracted.content).toContain("未配置「语音转写」模型");
    expect(extracted.sourceUri).toBe(SAMPLE_METADATA.webpage_url);
  });

  it("配置转写模型 → 下载音频、预处理后生成文字稿", async () => {
    const run: RunCommand = async (_exe, args) => {
      const outputIndex = args.indexOf("-o");
      if (outputIndex >= 0) {
        // 音频下载：按输出模板写一个假音频文件
        const template = args[outputIndex + 1];
        const filePath = template.replace(".%(ext)s", ".m4a");
        fs.mkdirSync(path.dirname(filePath), { recursive: true });
        fs.writeFileSync(filePath, "fake-audio");
        return { stdout: "" };
      }
      return { stdout: JSON.stringify(SAMPLE_METADATA) };
    };

    let cleanedUp = false;
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      getFormatterConfig: () => null,
      // 预处理注入：模拟 ffmpeg 转码产物（转写应收到转码后的路径）
      prepareAudio: async (filePath) => {
        expect(fs.existsSync(filePath)).toBe(true);
        return {
          filePath: `${filePath}.16k.mp3`,
          cleanup: () => {
            cleanedUp = true;
          },
        };
      },
      transcribe: async (filePath) => {
        expect(filePath.endsWith(".16k.mp3")).toBe(true);
        return "这是转写出来的文字稿";
      },
      getSummaryConfig: () => null,
    });
    expect(extracted.transcript).toBe("这是转写出来的文字稿");
    expect(extracted.content).not.toContain("未配置");
    expect(cleanedUp).toBe(true);
  });

  it("转写成功后自动 AI 排版", async () => {
    const run = buildAudioAwareRun();
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: passthroughPrepareAudio,
      transcribe: async () => "原始 转写 无标点",
      getFormatterConfig: () => ({
        provider: "openai",
        apiProtocol: "openai",
        apiKey: "sk-test",
        apiUrl: "https://api.openai.com",
        model: "fast-model",
      }),
      formatTranscript: async (raw) => {
        expect(raw).toBe("原始 转写 无标点");
        return "原始转写，已排版。";
      },
      getSummaryConfig: () => null,
    });
    expect(extracted.transcript).toBe("原始转写，已排版。");
  });

  it("AI 排版失败 → 保留原始转写，不影响导入", async () => {
    const run = buildAudioAwareRun();
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: passthroughPrepareAudio,
      transcribe: async () => "原始 转写 无标点",
      getFormatterConfig: () => ({
        provider: "openai",
        apiProtocol: "openai",
        apiKey: "sk-test",
        apiUrl: "https://api.openai.com",
        model: "fast-model",
      }),
      formatTranscript: async () => {
        throw new Error("HTTP 429");
      },
      getSummaryConfig: () => null,
    });
    expect(extracted.transcript).toBe("原始 转写 无标点");
    expect(extracted.content).not.toContain("文字稿生成失败");
  });

  it("转写成功后自动生成视频总结与 AI 标题：总结入正文，标题替换并留痕", async () => {
    const run = buildAudioAwareRun();
    const summaryConfig = {
      provider: "openai",
      apiProtocol: "openai" as const,
      apiKey: "sk-test",
      apiUrl: "https://api.openai.com",
      model: "main-model",
    };
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: passthroughPrepareAudio,
      transcribe: async () => "这是转写出来的文字稿",
      getFormatterConfig: () => null,
      getSummaryConfig: () => summaryConfig,
      summarize: async (input) => {
        expect(input.title).toBe("测试视频标题");
        expect(input.context).toBe("这是简介");
        expect(input.transcript).toBe("这是转写出来的文字稿");
        return {
          summary: "本视频讲解了核心内容。\n\n**一、要点**\n- 细节",
          title: "AI 拟定的更贴切标题",
        };
      },
    });
    expect(extracted.title).toBe("AI 拟定的更贴切标题");
    expect(extracted.content).toContain("> 原标题：测试视频标题");
    expect(extracted.content).toContain("## 视频总结");
    expect(extracted.content).toContain("本视频讲解了核心内容。");
    // 简介收进元数据引用块，总结小节位于其后；来源不再写入正文
    expect(extracted.content).toContain("> 简介：这是简介");
    expect(extracted.content.indexOf("## 视频总结")).toBeGreaterThan(
      extracted.content.indexOf("> 简介：这是简介"),
    );
    expect(extracted.content).not.toContain("来源：<");
  });

  it("模型未产出标题（title 为 null）→ 保留平台原标题，不写留痕", async () => {
    const run = buildAudioAwareRun();
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: passthroughPrepareAudio,
      transcribe: async () => "这是转写出来的文字稿",
      getFormatterConfig: () => null,
      getSummaryConfig: () => ({
        provider: "openai",
        apiProtocol: "openai" as const,
        apiKey: "sk-test",
        apiUrl: "https://api.openai.com",
        model: "main-model",
      }),
      summarize: async () => ({ summary: "- 要点", title: null }),
    });
    expect(extracted.title).toBe("测试视频标题");
    expect(extracted.content).not.toContain("原标题：");
    expect(extracted.content).toContain("## 视频总结");
  });

  it("视频总结生成失败 → 保留文字稿与原标题，不影响导入", async () => {
    const run = buildAudioAwareRun();
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: passthroughPrepareAudio,
      transcribe: async () => "这是转写出来的文字稿",
      getFormatterConfig: () => null,
      getSummaryConfig: () => ({
        provider: "openai",
        apiProtocol: "openai" as const,
        apiKey: "sk-test",
        apiUrl: "https://api.openai.com",
        model: "main-model",
      }),
      summarize: async () => {
        throw new Error("HTTP 500");
      },
    });
    expect(extracted.title).toBe("测试视频标题");
    expect(extracted.transcript).toBe("这是转写出来的文字稿");
    expect(extracted.content).not.toContain("## 视频总结");
    expect(extracted.content).toContain("这是简介");
  });

  it("按序上报子阶段，长链路不再全程停在「抓取中」", async () => {
    const stages: string[] = [];
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run: buildAudioAwareRun(),
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      transcribe: async () => "这是转写出来的文字稿",
      prepareAudio: passthroughPrepareAudio,
      getFormatterConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "fast-model",
        apiProtocol: "openai",
      }),
      formatTranscript: async (text) => text,
      getSummaryConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "main-model",
        apiProtocol: "openai",
      }),
      summarize: async () => ({ summary: "总结正文", title: "AI 标题" }),
      onStage: (stage) => stages.push(stage),
    });

    expect(extracted.transcript).toBe("这是转写出来的文字稿");
    expect(stages).toEqual([
      "video-metadata",
      "video-audio",
      "transcoding",
      "transcribing",
      "formatting",
      "summarizing",
    ]);
  });

  it("未配置转写时只上报元数据阶段", async () => {
    const stages: string[] = [];
    await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run: buildAudioAwareRun(),
      getTranscriptionConfig: () => null,
      onStage: (stage) => stages.push(stage),
    });
    expect(stages).toEqual(["video-metadata"]);
  });

  it("元数据解析失败 → 降级并透出 yt-dlp 原始错误", async () => {
    const run: RunCommand = async () => {
      throw new Error("yt-dlp 退出码 1: Unsupported URL");
    };
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run,
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toContain("解析失败");
    expect(extracted.degradedReason).toContain("Unsupported URL");
    expect(extracted.sourceUri).toBeNull();
  });
});

describe("extractVideoUrl（抖音）", () => {
  const douyinUrl = "https://www.iesdouyin.com/share/video/7663897644049173802/";
  const videoAweme = {
    awemeId: "7663897644049173802",
    kind: "video" as const,
    title: "抖音视频文案",
    description: "",
    author: "曲率出逃",
    durationSeconds: 243,
    playUrl: "https://aweme.snssdk.com/aweme/v1/play/?video_id=v02",
    imageMirrors: [] as string[][],
    webpageUrl: "https://www.douyin.com/video/7663897644049173802",
  };

  /** 抖音链路一旦调用 yt-dlp 就说明分流没生效 */
  const forbiddenRun: RunCommand = async () => {
    throw new Error("抖音链路不应调用 yt-dlp");
  };

  it("元数据走分享页解析，全程不碰 yt-dlp", async () => {
    const extracted = await extractVideoUrl(douyinUrl, "douyin", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchDouyin: async (url) => {
        expect(url).toBe(douyinUrl);
        return videoAweme;
      },
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toBeUndefined();
    expect(extracted.title).toBe("抖音视频文案");
    expect(extracted.content).toContain("平台：抖音");
    expect(extracted.content).toContain("作者：曲率出逃");
    expect(extracted.content).toContain("4:03");
    // 短链 / 分享链都收敛到规范来源，去重才不会漏
    expect(extracted.sourceUri).toBe(videoAweme.webpageUrl);
  });

  it("配置转写模型 → 直下无水印视频，不经 yt-dlp 下载音轨", async () => {
    let downloaded = "";
    const extracted = await extractVideoUrl(douyinUrl, "douyin", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchDouyin: async () => videoAweme,
      downloadDouyin: async (playUrl) => {
        downloaded = playUrl;
        const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-douyin-test-"));
        const filePath = path.join(dir, "douyin.mp4");
        fs.writeFileSync(filePath, "fake-video");
        return { dir, filePath };
      },
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: async (filePath) => ({ filePath, cleanup: () => {} }),
      transcribe: async () => "抖音口播文字稿",
      getFormatterConfig: () => null,
      getSummaryConfig: () => null,
    });
    expect(downloaded).toBe(videoAweme.playUrl);
    expect(extracted.transcript).toBe("抖音口播文字稿");
  });

  it("图文作品 → 图片条目，走配图落盘而非转写链路", async () => {
    const stages: string[] = [];
    const extracted = await extractVideoUrl(douyinUrl, "douyin", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchDouyin: async () => ({
        ...videoAweme,
        kind: "note" as const,
        title: "图文文案",
        playUrl: null,
        imageMirrors: [["https://p1.douyinpic.com/a.webp"]],
      }),
      // 配了转写模型也不该进转写：图文没有音轨
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      douyinNote: {
        downloadImage: async () => {
          const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-note-"));
          const filePath = path.join(dir, "image.webp");
          fs.writeFileSync(filePath, "fake-image");
          return { dir, filePath };
        },
        saveAsset: async () => "asset.webp",
        getOcrConfig: () => null,
        // 不读真实 ai-config.json，单测不该发出网络请求
        getTitleConfig: () => null,
      },
      onStage: (stage) => stages.push(stage),
    });
    expect(extracted.itemType).toBe("image");
    expect(extracted.title).toBe("图文文案");
    expect(extracted.content).toContain("图文 1 张");
    expect(extracted.content).toContain("![图 1](local-image://asset.webp)");
    expect(extracted.transcript).toBeUndefined();
    expect(stages).toEqual(["video-metadata", "image-download"]);
  });

  it("分享页解析失败 → 降级并透出原因，不占住去重", async () => {
    const extracted = await extractVideoUrl(douyinUrl, "douyin", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchDouyin: async () => {
        throw new Error("该作品已被作者删除");
      },
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toContain("该作品已被作者删除");
    expect(extracted.sourceUri).toBeNull();
  });
});

describe("buildVideoContent", () => {
  it("元数据引用块承载平台/作者/时长/简介，来源不写入正文", () => {
    const content = buildVideoContent(
      {
        title: "t",
        uploader: "作者A",
        durationSeconds: 65,
        description: "简介内容\n第二行",
        webpageUrl: "https://example.com/v",
      },
      "youtube",
    );
    expect(content).toContain("平台：YouTube");
    expect(content).toContain("作者：作者A");
    expect(content).toContain("1:05");
    // 简介压平为引用块单行
    expect(content).toContain("> 简介：简介内容 第二行");
    expect(content).not.toContain("来源：<");
    expect(content).not.toContain("https://example.com/v");
  });
});

describe("stripTranscriptionNote", () => {
  const metadata = {
    title: "t",
    uploader: "作者A",
    durationSeconds: 65,
    description: "简介内容",
    webpageUrl: "https://example.com/v",
  };

  it("移除「文字稿生成失败」注记，其余内容保持不变", () => {
    const failed = buildVideoContent(
      metadata,
      "bilibili",
      '文字稿生成失败：转写请求失败 (HTTP 429): {"error":{"message":"saturated"}}',
    );
    const clean = buildVideoContent(metadata, "bilibili");
    expect(stripTranscriptionNote(failed)).toBe(clean);
  });

  it("移除「未配置语音转写模型」注记", () => {
    const noted = buildVideoContent(
      metadata,
      "bilibili",
      "未配置「语音转写」模型，本次仅保存视频信息；配置后可在任务列表重试生成文字稿。",
    );
    expect(stripTranscriptionNote(noted)).not.toContain("未配置");
    expect(stripTranscriptionNote(noted)).toContain("简介内容");
  });

  it("无注记的内容原样返回", () => {
    const clean = buildVideoContent(metadata, "bilibili");
    expect(stripTranscriptionNote(clean)).toBe(clean);
  });
});
