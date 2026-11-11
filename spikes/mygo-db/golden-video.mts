// 视频连接器差分基线：真实 video-url.ts / video-captions.ts（tsx 直接加载），
// 命令执行（yt-dlp）、转写、排版、总结全部以注入的脚本化替身驱动，不启动外部进程、不访问网络。
// 用法：run-golden-video.mts <输出目录>（从 D:\Project\GuiZhi-mygo-spike 目录运行 tsx）。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

/* eslint-disable @typescript-eslint/no-explicit-any */
export interface Rec {
  kind: string;
  name: string;
  input: unknown;
  output: unknown;
}

const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));

const VTT = [
  "WEBVTT", "Kind: captions", "Language: zh", "", "1", "00:00:00.000 --> 00:00:02.000", "你好 &amp; 世界", "",
  "00:00:02.000 --> 00:00:04.000", "<c>你好</c> 世界", "你好 世界", "", "NOTE 注释", "不应出现", "", "STYLE", "::cue {}", "",
  "2", "00:00:04.000 --> 00:00:06.000", "&lt;tag&gt;&nbsp;末句 ", "",
].join("\n");
const SRT = ["1", "00:00:00,000 --> 00:00:01,500", "第一行", "", "2", "00:00:01,500 --> 00:00:03,000", "第二行", "第二行", ""].join("\n");
const EMPTY_VTT = "WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n\n";

export async function buildVideoGolden(): Promise<{ recs: Rec[] }> {
  const recs: Rec[] = [];
  const add = (kind: string, name: string, input: unknown, output: unknown) => recs.push({ kind, name, input, output });
  const V: any = await import("../../apps/desktop/src/main/services/import/video-url.ts");
  const C: any = await import("../../apps/desktop/src/main/services/import/video-captions.ts");

  // ---------- 纯函数 ----------
  for (const m of ["HTTP 403", "HTTP403 Forbidden", "yt-dlp 退出码 1: HTTP Error 403: Forbidden", "HTTP 401", "HTTP 404 not found", "HTTP 429", "HTTP 500", "HTTP  503", "连接被重置", "", "文字稿服务 http 502"]) {
    add("describeFailure", "describe", m, V.describeTranscriptionFailure(new Error(m)));
  }
  for (const s of [0, 5, 59, 60, 61, 599, 3599, 3600, 3661, 86399, 90061]) add("formatDuration", `dur-${s}`, s, V.formatDuration(s));

  const metaCases: string[] = [
    JSON.stringify({ title: "标题", uploader: "UP", duration: 125.4, description: "简介", webpage_url: "https://x/v" }),
    "WARNING: 噪声\n" + JSON.stringify({ title: "T", channel: "频道", duration: 59.5 }) + "\n[download] 尾部噪声",
    JSON.stringify({ title: 3, uploader: null, channel: "c", duration: "5", description: 1, webpage_url: "" }),
    JSON.stringify({ duration: Infinity }),
    JSON.stringify({ title: "a", duration: 0 }),
    JSON.stringify({ title: "第一行" }) + "\n" + JSON.stringify({ title: "最后一行胜出" }),
    "  \r\n  {\"title\":\"CRLF\"}\r\n",
    "",
    "没有 JSON 的行",
    "[1,2,3]",
  ];
  for (const [i, s] of metaCases.entries()) {
    let out: unknown;
    try { out = { ok: V.parseYtDlpMetadata(s) }; } catch (e) { out = { error: errMsg(e) }; }
    // 非法 JSON 的 SyntaxError 文本引擎相关，只对拍是否失败
    add("ytMeta", `meta-${i}`, s, out);
  }

  const bigDesc = "详细描述 ".repeat(100) + " 链接 https://a.example/1 https://b.example/2?x=1 https://c.example/3 https://d.example/4 https://e.example/5 https://f.example/6 https://g.example/7";
  const contentCases: { meta: any; platform: string; note?: string; source?: string }[] = [
    { meta: { title: "t", uploader: "UP", durationSeconds: 3725, description: "简介  内容\n换行", webpageUrl: "https://x" }, platform: "bilibili" },
    { meta: { title: "t", uploader: "", durationSeconds: null, description: "", webpageUrl: "" }, platform: "youtube" },
    { meta: { title: "t", uploader: "UP", durationSeconds: 0, description: bigDesc, webpageUrl: "" }, platform: "douyin", note: "文字稿生成失败：原因" },
    { meta: { title: "t", uploader: "UP", durationSeconds: 59, description: "短 https://only.example/a", webpageUrl: "" }, platform: "xiaohongshu", source: "发布者字幕（zh）" },
    { meta: { title: "t", uploader: "", durationSeconds: 61, description: "   ", webpageUrl: "" }, platform: "bilibili", note: "注记", source: "音频识别（m）" },
    { meta: { title: "t", uploader: "UP", durationSeconds: 5, description: "😀".repeat(200) + "尾", webpageUrl: "" }, platform: "youtube" },
  ];
  for (const [i, c] of contentCases.entries()) {
    add("videoContent", `content-${i}`, c, V.buildVideoContent(c.meta, c.platform, c.note, c.source));
  }

  const captionTexts = [VTT, SRT, EMPTY_VTT, "", "\uFEFFWEBVTT\n\n00:00:00.000 --> 00:00:01.000\n甲\n甲\n乙\n甲", "WEBVTT\nKind: captions\nLanguage: en\n00:00:00.000 --> 00:00:01.000\n无空行头部\n", "NOTE a\n续行\n\nREGION\nid:x\n\n00:00:01.000 --> 00:00:02.000\n正文\n7\n8"];
  for (const [i, s] of captionTexts.entries()) add("captionText", `ct-${i}`, s, C.parseCaptionText(s));

  // ---------- 字幕下载（脚本化 run，往目录里写字幕文件） ----------
  type Step = { error?: string; files?: Record<string, string>; removeDir?: boolean };
  const capCases: { name: string; steps: Step[]; }[] = [
    { name: "published-zh", steps: [{ files: { "caption.zh-Hans.vtt": VTT, "caption.en.vtt": SRT } }] },
    { name: "published-empty-then-auto", steps: [{ files: { "caption.zh.vtt": EMPTY_VTT } }, { files: { "caption.en-orig.vtt": VTT, "caption.zh.vtt": SRT, "caption.fr.srt": SRT } }] },
    { name: "published-error-auto-ok", steps: [{ error: "yt-dlp 退出码 1: no subs" }, { files: { "caption.en.srt": SRT } }] },
    { name: "both-error", steps: [{ error: "e1" }, { error: "e2" }] },
    { name: "both-none", steps: [{}, {}] },
    { name: "dir-removed", steps: [{ removeDir: true }, { removeDir: true }] },
    { name: "sort-order", steps: [{ files: { "caption.ja.vtt": VTT, "caption.en-US.vtt": SRT, "caption.en.vtt": SRT, "caption.cmn-Hans.vtt": VTT, "caption.b.vtt": VTT, "caption.A.vtt": VTT } }] },
    { name: "language-parse", steps: [{ files: { "caption.zh-Hant.srt": SRT } }] },
    { name: "non-caption-name", steps: [{ files: { "other.vtt": VTT } }] },
  ];
  for (const c of capCases) {
    const calls: unknown[] = [];
    let n = 0;
    const run = async (exe: string, args: string[], opts: { timeoutMs: number }) => {
      const out = args[args.indexOf("-o") + 1];
      const dir = path.dirname(out);
      calls.push({ exe, args: args.map((a) => a.split(dir).join("<DIR>")), timeoutMs: opts.timeoutMs });
      const step = c.steps[Math.min(n++, c.steps.length - 1)];
      if (step.removeDir) fs.rmSync(dir, { recursive: true, force: true });
      if (step.error) throw new Error(step.error);
      for (const [name, content] of Object.entries(step.files ?? {})) fs.writeFileSync(path.join(dir, name), content);
      return { stdout: "" };
    };
    const r = await C.downloadPlatformCaptions("/fake/yt-dlp", "https://v.example/1", run);
    add("captions", c.name, c, { result: r ? { text: r.text, source: r.source, language: r.language ?? null } : null, calls });
  }

  // ---------- 整条采集流程 ----------
  interface Scn {
    name: string;
    url?: string;
    platform?: string;
    metadata?: { stdout?: string; error?: string; notFound?: boolean };
    captions?: { published?: Record<string, string>; auto?: Record<string, string>; error?: boolean };
    audio?: { files?: Record<string, string>; error?: string };
    transcription?: boolean;
    transcribe?: string | { error: string };
    diarize?: boolean;
    formatter?: null | { output?: string; error?: string; skipped?: string; partial?: string };
    summary?: null | { summary: string; title: string | null } | { error: string };
  }
  const META_FULL = JSON.stringify({ title: "原视频标题", uploader: "UP主", duration: 3725.4, description: "视频简介 https://github.com/a/b", webpage_url: "https://www.bilibili.com/video/BV1xx" });
  const scns: Scn[] = [
    { name: "asr-full", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: { "audio.m4a": "x" } }, transcription: true, transcribe: "原始转写文本", formatter: { output: "排版后的文字稿。" }, summary: { summary: "- 要点一\n- 要点二", title: "AI 拟的标题" } },
    { name: "captions-published", metadata: { stdout: META_FULL }, captions: { published: { "caption.zh-Hans.vtt": VTT } }, transcription: true, transcribe: "不应被调用", summary: { summary: "字幕总结", title: null } },
    { name: "captions-auto", platform: "youtube", url: "https://www.youtube.com/watch?v=abc", metadata: { stdout: META_FULL }, captions: { auto: { "caption.en-orig.vtt": VTT } }, transcription: false, summary: null },
    { name: "captions-no-transcription-config", metadata: { stdout: META_FULL }, captions: {}, transcription: false },
    { name: "no-transcription-no-captions-error", metadata: { stdout: META_FULL }, captions: { error: true }, transcription: false, summary: { summary: "不会调用", title: "x" } },
    { name: "not-installed", metadata: { notFound: true } },
    { name: "metadata-error", metadata: { error: "yt-dlp 退出码 1: ERROR: Video unavailable" } },
    { name: "metadata-no-json", metadata: { stdout: "WARNING: nothing\n" } },
    { name: "audio-error-403", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { error: "yt-dlp 退出码 1: HTTP 403" }, transcription: true },
    { name: "audio-error-unknown", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { error: "yt-dlp 退出码 1: 未知故障" }, transcription: true },
    { name: "audio-empty", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: {} }, transcription: true },
    { name: "transcribe-error-429", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: { "audio.webm": "x" } }, transcription: true, transcribe: { error: "转写接口 HTTP 429" } },
    { name: "transcribe-empty", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: { "audio.webm": "x" } }, transcription: true, transcribe: "", formatter: { output: "不会调用" }, summary: { summary: "不会调用", title: "x" } },
    { name: "diarize-on", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: { "audio.m4a": "x" } }, transcription: true, diarize: true, transcribe: "[说话人1] 你好", formatter: null, summary: null },
    { name: "format-skipped", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: { "audio.m4a": "x" } }, transcription: true, transcribe: "原文", formatter: { output: "原文", skipped: "文字稿过长" }, summary: null },
    { name: "format-partial", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: { "audio.m4a": "x" } }, transcription: true, transcribe: "原文", formatter: { output: "半排版", partial: "时间预算耗尽" }, summary: null },
    { name: "format-error", metadata: { stdout: META_FULL }, captions: { error: true }, audio: { files: { "audio.m4a": "x" } }, transcription: true, transcribe: "原始文字", formatter: { error: "模型 500" }, summary: { summary: "总结", title: null } },
    { name: "summary-error", metadata: { stdout: META_FULL }, captions: { published: { "caption.zh.srt": SRT } }, summary: { error: "总结模型失败" } },
    { name: "summary-same-title", metadata: { stdout: META_FULL }, captions: { published: { "caption.zh.srt": SRT } }, summary: { summary: "总结", title: "原视频标题" } },
    { name: "summary-title-no-meta-title", metadata: { stdout: JSON.stringify({ uploader: "UP", description: "" }) }, captions: { published: { "caption.zh.srt": SRT } }, summary: { summary: "总结", title: "新标题" } },
    { name: "no-title-no-webpage", metadata: { stdout: JSON.stringify({ duration: 10 }) }, captions: {}, transcription: false },
    { name: "uploader-channel-fallback", metadata: { stdout: JSON.stringify({ title: "T", channel: "频道名", duration: 59.5, description: "d", webpage_url: "https://w/x" }) }, captions: {}, transcription: false },
  ];
  for (const s of scns) {
    const platform = s.platform ?? "bilibili";
    const url = s.url ?? "https://www.bilibili.com/video/BV1xx";
    const stages: string[] = [];
    const runCalls: unknown[] = [];
    const transcribeCalls: unknown[] = [];
    const formatCalls: unknown[] = [];
    const summarizeCalls: unknown[] = [];
    const prepareCalls: unknown[] = [];
    const run = async (exe: string, args: string[], opts: { timeoutMs: number }) => {
      const oIdx = args.indexOf("-o");
      const dir = oIdx >= 0 ? path.dirname(args[oIdx + 1]) : "";
      runCalls.push({ exe, args: args.map((a) => (dir ? a.split(dir).join("<DIR>") : a)), timeoutMs: opts.timeoutMs });
      if (args.includes("--dump-json")) {
        if (s.metadata?.notFound) throw new V.YtDlpNotFoundError();
        if (s.metadata?.error) throw new Error(s.metadata.error);
        return { stdout: s.metadata?.stdout ?? "" };
      }
      if (args.includes("--skip-download")) {
        if (s.captions?.error) throw new Error("no captions");
        const files = args.includes("--write-auto-subs") ? s.captions?.auto : s.captions?.published;
        for (const [name, content] of Object.entries(files ?? {})) fs.writeFileSync(path.join(dir, name), content);
        return { stdout: "" };
      }
      if (s.audio?.error) throw new Error(s.audio.error);
      for (const [name, content] of Object.entries(s.audio?.files ?? {})) fs.writeFileSync(path.join(dir, name), content);
      return { stdout: "" };
    };
    const cfg = { model: "asr-model", apiUrl: "http://example.test/v1", apiKey: "k", apiProtocol: "openai", provider: "custom" };
    const deps: any = {
      getYtDlpPath: () => "/fake/yt-dlp",
      getFfmpegPath: () => "/fake/ffmpeg",
      run,
      onStage: (st: string) => stages.push(st),
      getDiarize: () => s.diarize === true,
      getTranscriptionConfig: () => (s.transcription ? cfg : null),
      transcribe: async (filePath: string, _c: unknown, _sig: unknown, opts: { diarize?: boolean }) => {
        transcribeCalls.push({ file: path.basename(filePath), diarize: opts?.diarize === true });
        const r = s.transcribe ?? "默认转写";
        if (typeof r !== "string") throw new Error(r.error);
        return r;
      },
      prepareAudio: async (filePath: string, ffmpeg: string) => {
        prepareCalls.push({ file: path.basename(filePath), ffmpeg });
        return { filePath, cleanup() {} };
      },
      getFormatterConfig: () => (s.formatter ? { provider: "custom", apiProtocol: "openai", apiKey: "k", apiUrl: "http://x", model: "fm" } : null),
      formatTranscript: async (raw: string, _c: unknown, o: { glossary?: string[] }) => {
        formatCalls.push({ raw, glossary: o?.glossary ?? [] });
        const f = s.formatter!;
        if (f.error) throw new Error(f.error);
        return { text: f.output ?? raw, skippedReason: f.skipped, partialReason: f.partial };
      },
      getSummaryConfig: () => (s.summary ? { provider: "custom", apiProtocol: "openai", apiKey: "k", apiUrl: "http://x", model: "sm" } : null),
      summarize: async (input: unknown) => {
        summarizeCalls.push(input);
        const r = s.summary as any;
        if (r && "error" in r) throw new Error(r.error);
        return { summary: r.summary, title: r.title };
      },
    };
    let result: unknown, error: string | undefined;
    try {
      const orig = console.warn;
      console.warn = () => {};
      try { result = await V.extractVideoUrl(url, platform, deps); } finally { console.warn = orig; }
    } catch (e) { error = errMsg(e); }
    add("extract", s.name, { ...s, platform, url }, { result: result ?? null, error: error ?? null, stages, runCalls, transcribeCalls, formatCalls, summarizeCalls, prepareCalls });
  }
  const { addImageNoteCases } = await import(pathToFileURL(path.join(path.dirname(fileURLToPath(import.meta.url)), "imagenote-cases.mts")).href);
  await addImageNoteCases(add);
  const { addDouyinCases } = await import(pathToFileURL(path.join(path.dirname(fileURLToPath(import.meta.url)), "douyin-cases.mts")).href);
  await addDouyinCases(add);
  const { addXhsCases } = await import(pathToFileURL(path.join(path.dirname(fileURLToPath(import.meta.url)), "xhs-cases.mts")).href);
  await addXhsCases(add);
  const { addArticleCases } = await import(pathToFileURL(path.join(path.dirname(fileURLToPath(import.meta.url)), "article-cases.mts")).href);
  await addArticleCases(add);
  void os;
  return { recs };
}
