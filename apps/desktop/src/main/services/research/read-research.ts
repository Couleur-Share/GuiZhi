import { resolvePublicAddress } from "../net-safety";
import { createHash, randomUUID } from "node:crypto";
import type { ResearchCandidate, ResearchDocument, ResearchPassage } from "@guizhi/shared/types";
import type { BrowserCaptureService } from "../platform-capture/browser-capture";
import { fetchAuthenticatedDouyin, fetchAuthenticatedXiaohongshu } from "../platform-capture/authenticated-platforms";
import { downloadPlatformCaptions } from "../import/video-captions";
import { runCommand, parseYtDlpMetadata } from "../import/video-url";
import { resolveYtDlpExecutable } from "../media/ytdlp-manager";

export type ResearchReader = (candidate: ResearchCandidate, signal: AbortSignal, options?: { includeComments?: boolean }) => Promise<ResearchDocument>;
export function textPassages(text: string, kind: ResearchPassage["kind"]): ResearchPassage[] {
  return text.split(/\n\s*\n|\n/).flatMap((line) => line.match(/[\s\S]{1,2000}/g) ?? []).filter((s) => s.trim()).map((text, position) => ({ text: text.trim(), position, kind }));
}

export function createResearchReader(browser: BrowserCaptureService, toolPath: () => string | null): ResearchReader {
  return async (candidate, signal, options) => {
    const doc: ResearchDocument = { id: randomUUID(), runId: candidate.runId, candidateId: candidate.id, source: candidate.source, url: candidate.url, title: candidate.title, author: candidate.author, publishedAt: candidate.publishedAt, capturedAt: Date.now(), status: "reading", passages: [], contentHash: null, truncated: false };
    const warnings: string[] = [];
    try {
      signal.throwIfAborted();
      if (candidate.source === "web") throw new Error("公开网页应从本轮网页快照读取");
      // Only platform candidates are accepted. Never pass arbitrary URLs/arguments to yt-dlp.
      const url = new URL(candidate.url);
      const allowed = candidate.source === "bilibili" ? /(^|\.)bilibili\.com$/ : candidate.source === "douyin" ? /(^|\.)douyin\.com$/ : /(^|\.)xiaohongshu\.com$/;
      if (url.protocol !== "https:" || !allowed.test(url.hostname) || url.username || url.password) throw new Error("研究材料链接不属于所选平台");
      if (candidate.source === "bilibili") {
        if (!/^\/video\/BV[\w]+\/?$/.test(url.pathname)) throw new Error("不是有效的 B 站视频路径");
        await resolvePublicAddress(url.hostname);
        signal.throwIfAborted();
        const executable = resolveYtDlpExecutable(toolPath());
        try {
          const result = await runCommand(executable, ["--ignore-config", "--dump-json", "--skip-download", "--no-playlist", "--no-warnings", candidate.url], { signal, timeoutMs: 60_000 });
          const meta = parseYtDlpMetadata(result.stdout);
          doc.title = meta.title; doc.author = meta.uploader ?? doc.author;
          doc.passages.push(...textPassages(meta.description ?? "", "description"));
        } catch { signal.throwIfAborted(); warnings.push("视频元数据读取失败"); }
        const captions = await downloadPlatformCaptions(executable, candidate.url, runCommand, signal);
        signal.throwIfAborted();
        if (captions) {
          doc.passages.push(...(captions.cues?.length ? captions.cues.map((cue) => ({ ...cue, kind: "caption" as const, position: 0 })) : textPassages(captions.text, "caption")));
          if (captions.source === "platform-ai-captions") warnings.push("文字来自平台自动字幕，可能存在识别误差");
        } else warnings.push("未取得字幕或字幕依赖不可用；未下载音频、未执行转写");
      } else {
        const note = candidate.source === "douyin" ? await fetchAuthenticatedDouyin(browser, candidate.url, signal) : await fetchAuthenticatedXiaohongshu(browser, candidate.url, signal);
        doc.title = note.title; doc.author = note.author;
        doc.passages = textPassages([note.title, note.description].filter(Boolean).join("\n\n"), note.kind === "video" ? "description" : "body");
        warnings.push(note.kind === "video" ? "仅取得视频文案，未读取口播" : "未识别配图中的文字");
        if (options?.includeComments === true) {
          try {
            const comments = await browser.captureComments(candidate.source, candidate.url, 20, signal);
            const authors = new Set<string>(); const texts = new Set<string>();
            for (const comment of comments.slice(0, 20).sort((a, b) => (b.likeCount ?? 0) - (a.likeCount ?? 0))) {
              if (texts.has(comment.content) || (comment.authorName && authors.has(comment.authorName))) continue;
              texts.add(comment.content); if (comment.authorName) authors.add(comment.authorName);
              doc.passages.push({ kind: "comment", text: comment.content.slice(0, 5000), author: comment.authorName, externalId: comment.externalId, position: 0 });
            }
          } catch { signal.throwIfAborted(); warnings.push("评论未完整取得"); }
        }
      }
    } catch (error) {
      doc.error = signal.aborted ? "精读已中断" : error instanceof Error ? error.message : String(error);
    }
    let remaining = 200_000;
    doc.passages = doc.passages.flatMap((p, position) => {
      const text = p.text.slice(0, Math.max(0, remaining)); remaining -= text.length;
      if (text.length < p.text.length) doc.truncated = true;
      return text ? [{ ...p, text, position }] : [];
    });
    const body = doc.passages.filter((p) => p.kind !== "comment").map((p) => p.text).join("\n");
    doc.contentHash = body ? createHash("sha256").update(body.normalize("NFKC").replace(/\s+/g, " ")).digest("hex") : null;
    doc.warning = warnings.join("；") || undefined;
    doc.status = signal.aborted ? "interrupted" : doc.passages.length ? doc.error || doc.warning ? "partial" : "ready" : "failed";
    return doc;
  };
}
