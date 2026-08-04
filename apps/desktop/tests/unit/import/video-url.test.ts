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
  describeTranscriptionFailure,
  detectVideoPlatform,
  extractVideoUrl,
  formatDuration,
  parseYtDlpMetadata,
  stripTranscriptionNote,
  upsertTranscriptionSourceNote,
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
    expect(detectVideoPlatform("https://www.youtube.com/shorts/abcdef")).toBe(
      "youtube",
    );
    expect(detectVideoPlatform("https://v.douyin.com/xyz/")).toBe("douyin");
    expect(detectVideoPlatform("https://www.xiaohongshu.com/explore/123")).toBe(
      "xiaohongshu",
    );
  });

  it("普通网页与非视频路径不误判", () => {
    expect(detectVideoPlatform("https://example.com/article")).toBeNull();
    expect(
      detectVideoPlatform("https://www.bilibili.com/read/cv123"),
    ).toBeNull();
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

describe("describeTranscriptionFailure", () => {
  it("把平台拒绝音频的 HTTP 403 说清原因和下一步", () => {
    expect(describeTranscriptionFailure(new Error("HTTP 403"))).toBe(
      "文字稿生成失败：平台拒绝访问音频（HTTP 403）。视频可能仅限登录或私密可见，也可能是平台暂时限制解析；请确认链接能在未登录状态打开后稍后重试。",
    );
  });

  it("未知错误保留完整原文，方便后续诊断", () => {
    expect(describeTranscriptionFailure(new Error("服务返回了未知字段"))).toBe(
      "文字稿生成失败：服务返回了未知字段",
    );
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

  it("发布者字幕优先：即使未配置 ASR 也直接入库，不下载音轨", async () => {
    const stages: string[] = [];
    const transcribe = vi.fn();
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run: async () => ({ stdout: JSON.stringify(SAMPLE_METADATA) }),
      getPlatformCaptions: async () => ({
        text: "这是发布者提供的字幕。",
        source: "platform-subtitles",
        language: "zh-CN",
      }),
      getTranscriptionConfig: () => null,
      getSummaryConfig: () => null,
      transcribe,
      onStage: (stage) => stages.push(stage),
    });

    expect(extracted.transcript).toBe("这是发布者提供的字幕。");
    expect(extracted.content).toContain("> 文字稿来源：发布者字幕（zh-CN）");
    expect(extracted.content).not.toContain("未配置「语音转写」模型");
    expect(transcribe).not.toHaveBeenCalled();
    expect(stages).toEqual(["video-metadata", "video-captions"]);
  });

  it("平台字幕缺失时回退到 ASR，并如实标明模型来源", async () => {
    const extracted = await extractVideoUrl(url, "bilibili", {
      getYtDlpPath: () => null,
      run: buildAudioAwareRun(),
      getPlatformCaptions: async () => null,
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: passthroughPrepareAudio,
      transcribe: async () => "ASR 文字稿",
      getFormatterConfig: () => null,
      getSummaryConfig: () => null,
    });

    expect(extracted.transcript).toBe("ASR 文字稿");
    expect(extracted.content).toContain("> 文字稿来源：音频识别（whisper-1）");
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
        return { text: "原始转写，已排版。" };
      },
      getSummaryConfig: () => null,
    });
    expect(extracted.transcript).toBe("原始转写，已排版。");
  });

  it("排版带上取自标题与简介的专名表：本地引擎听错的正是这批词", async () => {
    // 元数据里出现的拉丁专名要进术语表，中文部分不进
    const run: RunCommand = async (_exe, args) => {
      const outputIndex = args.indexOf("-o");
      if (outputIndex >= 0) {
        const filePath = args[outputIndex + 1].replace(".%(ext)s", ".m4a");
        fs.mkdirSync(path.dirname(filePath), { recursive: true });
        fs.writeFileSync(filePath, "fake-audio");
        return { stdout: "" };
      }
      return {
        stdout: JSON.stringify({
          ...SAMPLE_METADATA,
          title: "React 的 useEffect 到底怎么用",
          description: "顺带聊聊 Docker 与 GitHub Actions。",
        }),
      };
    };
    let received: string[] | undefined;
    await extractVideoUrl(url, "bilibili", {
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
      formatTranscript: async (raw, _config, options) => {
        received = options?.glossary;
        return { text: raw };
      },
      getSummaryConfig: () => null,
    });
    expect(received).toEqual([
      "React",
      "useEffect",
      "Docker",
      "GitHub Actions",
    ]);
  });

  it("导入时按设置决定要不要区分说话人，默认不分", async () => {
    const seen: (boolean | undefined)[] = [];
    const deps = {
      getYtDlpPath: () => null,
      run: buildAudioAwareRun(),
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: passthroughPrepareAudio,
      transcribe: async (
        _path: string,
        _config: unknown,
        _signal?: AbortSignal,
        options?: { diarize?: boolean },
      ) => {
        seen.push(options?.diarize);
        return "文字稿";
      },
      getFormatterConfig: () => null,
      getSummaryConfig: () => null,
    };

    await extractVideoUrl(url, "bilibili", deps);
    await extractVideoUrl(url, "bilibili", { ...deps, getDiarize: () => true });

    expect(seen).toEqual([false, true]);
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

  it("超长文字稿跳过排版 → 保留原始转写，导入照常完成", async () => {
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
      formatTranscript: async (raw) => ({
        text: raw,
        skippedReason: "文字稿 60000 字，超过自动排版上限 50000 字",
      }),
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
      formatTranscript: async (text) => ({ text }),
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
      "video-captions",
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
    expect(stages).toEqual(["video-metadata", "video-captions"]);
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
  const douyinUrl =
    "https://www.iesdouyin.com/share/video/7663897644049173802/";
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
        const dir = fs.mkdtempSync(
          path.join(os.tmpdir(), "guizhi-douyin-test-"),
        );
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
      imageNote: {
        downloadImage: async () => {
          const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-note-"));
          const filePath = path.join(dir, "image.webp");
          fs.writeFileSync(filePath, "fake-image");
          return { dir, filePath };
        },
        saveAsset: async () => "asset.webp",
        // 不读真实 ai-config.json，单测不该发出网络请求
        getOcrConfig: () => null,
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

describe("extractVideoUrl（小红书）", () => {
  const shareUrl =
    "https://www.xiaohongshu.com/discovery/item/6a59e7f3000000000301fc49?xsec_token=CBai8=&xsec_source=pc_share";
  const videoNote = {
    noteId: "6a59e7f3000000000301fc49",
    kind: "video" as const,
    title: "小红书视频标题",
    authoredTitle: true,
    description: "笔记文案",
    author: "司晨视觉",
    durationSeconds: 128,
    playUrls: [
      "https://sns-video-bd.xhscdn.com/master.mp4",
      "https://sns-video-hw.xhscdn.com/backup.mp4",
    ],
    imageMirrors: [] as string[][],
    webpageUrl: "https://www.xiaohongshu.com/explore/6a59e7f3000000000301fc49",
  };

  /** 小红书链路一旦调用 yt-dlp 就说明分流没生效 */
  const forbiddenRun: RunCommand = async () => {
    throw new Error("小红书链路不应调用 yt-dlp");
  };

  it("元数据走笔记页解析，全程不碰 yt-dlp", async () => {
    const extracted = await extractVideoUrl(shareUrl, "xiaohongshu", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchXiaohongshu: async (url) => {
        // 带 token 的原始链接要原样传下去，去掉就取不到笔记
        expect(url).toBe(shareUrl);
        return videoNote;
      },
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toBeUndefined();
    expect(extracted.content).toContain("平台：小红书");
    expect(extracted.content).toContain("作者：司晨视觉");
    expect(extracted.content).toContain("2:08");
    // 分享链每次带的 token 都不同，来源必须收敛到规范链接
    expect(extracted.sourceUri).toBe(videoNote.webpageUrl);
  });

  it("配置转写模型 → 直下视频源，主源与备源一并交给下载器", async () => {
    let received: string[] = [];
    const extracted = await extractVideoUrl(shareUrl, "xiaohongshu", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchXiaohongshu: async () => videoNote,
      downloadXiaohongshu: async (playUrls) => {
        received = playUrls;
        const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-xhs-test-"));
        const filePath = path.join(dir, "xiaohongshu.mp4");
        fs.writeFileSync(filePath, "fake-video");
        return { dir, filePath };
      },
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      prepareAudio: async (filePath) => ({ filePath, cleanup: () => {} }),
      transcribe: async () => "小红书口播文字稿",
      getFormatterConfig: () => null,
      getSummaryConfig: () => null,
    });
    expect(received).toEqual(videoNote.playUrls);
    expect(extracted.transcript).toBe("小红书口播文字稿");
  });

  it("图文笔记 → 图片条目，配图落盘且保留作者写的标题", async () => {
    const stages: string[] = [];
    const extracted = await extractVideoUrl(shareUrl, "xiaohongshu", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchXiaohongshu: async () => ({
        ...videoNote,
        kind: "note" as const,
        title: "AI漫剧培训实战课程",
        durationSeconds: null,
        playUrls: [],
        imageMirrors: [["https://sns-webpic-qc.xhscdn.com/a"]],
      }),
      // 配了转写模型也不该进转写：图文没有音轨
      getTranscriptionConfig: () => ({
        apiUrl: "https://api.openai.com",
        apiKey: "sk-test",
        model: "whisper-1",
      }),
      imageNote: {
        downloadImage: async () => {
          const dir = fs.mkdtempSync(
            path.join(os.tmpdir(), "guizhi-xhs-note-"),
          );
          const filePath = path.join(dir, "image.jpg");
          fs.writeFileSync(filePath, "fake-image");
          return { dir, filePath };
        },
        saveAsset: async () => "asset.jpg",
        getOcrConfig: () => null,
      },
      onStage: (stage) => stages.push(stage),
    });
    expect(extracted.itemType).toBe("image");
    expect(extracted.title).toBe("AI漫剧培训实战课程");
    expect(extracted.content).toContain(
      "平台：小红书 · 作者：司晨视觉 · 图文 1 张",
    );
    expect(extracted.content).toContain("![图 1](local-image://asset.jpg)");
    expect(extracted.transcript).toBeUndefined();
    expect(stages).toEqual(["video-metadata", "image-download"]);
  });

  it("笔记页解析失败 → 降级并透出原因，不占住去重", async () => {
    const extracted = await extractVideoUrl(shareUrl, "xiaohongshu", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchXiaohongshu: async () => {
        throw new Error("小红书拒绝了该链接（缺少 xsec_token 访问令牌…）");
      },
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toContain("xsec_token");
    expect(extracted.sourceUri).toBeNull();
  });

  it("视频笔记没给出播放地址 → 降级，不静默产出空条目", async () => {
    const extracted = await extractVideoUrl(shareUrl, "xiaohongshu", {
      getYtDlpPath: () => null,
      run: forbiddenRun,
      fetchXiaohongshu: async () => ({ ...videoNote, playUrls: [] }),
      getTranscriptionConfig: () => null,
    });
    expect(extracted.degradedReason).toContain("未能取到视频播放地址");
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

  it("简介里的链接单独列一行，且从未截断的原文里抠", () => {
    // 作者常把仓库地址、文档链接放在长简介末尾，而简介写进正文时截到 300 字
    const content = buildVideoContent(
      {
        title: "t",
        uploader: "作者A",
        durationSeconds: 65,
        description: `${"废话".repeat(200)}源码在 https://github.com/foo/bar 配套文档 https://example.com/doc`,
        webpageUrl: "https://example.com/v",
      },
      "bilibili",
    );

    expect(content).toContain("> 相关链接：");
    expect(content).toContain("https://github.com/foo/bar");
    expect(content).toContain("https://example.com/doc");
    // 简介本身仍然按 300 字截断，链接不受影响
    expect(content).toContain("…");
  });

  it("简介里没有链接时不摆一个空行", () => {
    const content = buildVideoContent(
      {
        title: "t",
        uploader: "作者A",
        durationSeconds: 65,
        description: "纯文字简介，没有链接",
        webpageUrl: "https://example.com/v",
      },
      "bilibili",
    );

    expect(content).not.toContain("相关链接");
  });

  it("链接一堆时只列前几条，避免推广短链刷屏", () => {
    const many = Array.from(
      { length: 12 },
      (_, index) => `https://example.com/${index}`,
    ).join(" ");
    const content = buildVideoContent(
      {
        title: "t",
        uploader: "作者A",
        durationSeconds: 65,
        description: many,
        webpageUrl: "https://example.com/v",
      },
      "bilibili",
    );

    const linkLine = content
      .split("\n")
      .find((line) => line.startsWith("> 相关链接："))!;
    expect(linkLine.match(/https:/g)).toHaveLength(5);
  });

  it("元数据引用块必须连续：链接行不能把简介与后续内容切开", () => {
    // parseVideoMetaBlock 只吃连续的 `>` 行，中间夹空行会让解析提前截断
    const content = buildVideoContent(
      {
        title: "t",
        uploader: "作者A",
        durationSeconds: 65,
        description: "看这里 https://example.com/doc",
        webpageUrl: "https://example.com/v",
      },
      "bilibili",
      "文字稿生成失败：xxx",
    );
    const quoteBlock = content.split("\n\n")[0];

    expect(quoteBlock.split("\n").every((line) => line.startsWith(">"))).toBe(
      true,
    );
    expect(quoteBlock).toContain("相关链接");
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

  it("手动重新转录会覆盖已有来源注记", () => {
    const content = buildVideoContent(
      metadata,
      "bilibili",
      undefined,
      "平台 AI 字幕（zh-CN）",
    );
    const updated = upsertTranscriptionSourceNote(
      content,
      "音频识别（whisper-1）",
    );
    expect(updated).toContain("> 文字稿来源：音频识别（whisper-1）");
    expect(updated).not.toContain("平台 AI 字幕");
  });
});
