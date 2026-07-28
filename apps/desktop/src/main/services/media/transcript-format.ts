/**
 * 文字稿 AI 排版：语音转写的原始输出通常无标点、无分段，
 * 用 chat 模型分块整理（补标点 / 分自然段 / 去语气词），不改写内容。
 *
 * 模型解析走 ai-config.json 的 fastText 路由（回退 mainText → 默认 chat 模型），
 * 与转写一样由主进程直读配置；未配置文本模型或排版失败时，调用方保留原始文字稿。
 *
 * 每块输出做验收（标点密度 / 长度比 / 截断），不合格自动重试一次——
 * 思考类模型偶发"复读式敷衍"（原样返回不加标点），不能把未排版结果当成功写库。
 * 重试后仍不合格：**第一块**就失败直接抛错（多半是模型配错），
 * 中途失败则收下已排好的部分、其余接回原文，并回一个 `partialReason`。
 */
import {
  chatCompletion,
  coreAIConfigService,
  type AIChatMessage,
  type AIChatResult,
  type AIClientConfig,
} from "@guizhi/core";
import {
  TRANSCRIPT_FORMAT_CHUNK_CHARS,
  TRANSCRIPT_FORMAT_LONG_CHARS,
} from "@guizhi/shared/constants";
import { countSpeakerPrefixes } from "@guizhi/shared/utils/speaker-note";
import { recordMainAiUsage } from "../ai-usage";

/** 单块上限：兼顾输出 token 限制与单请求耗时 */
const CHUNK_MAX_CHARS = TRANSCRIPT_FORMAT_CHUNK_CHARS;
/**
 * 单块请求超时。
 *
 * 原本是 120 秒，实测偏紧：同一条短提示词在中转站的思考类模型上
 * 耗时 75 / 117 / 130 秒都出现过——120 秒正好卡在正常区间的上沿，
 * 于是「本来会成功」的请求被自己的超时掐掉，重试一次再掐，整块作废。
 * 取 240 秒与生图那边的单次上限同口径。
 */
const CHUNK_TIMEOUT_MS = 240_000;
/**
 * 整篇排版的时间预算，按块数缩放。
 *
 * 单块超时 240 秒 × 每块 2 次尝试，32 块理论上能拖两个多小时。固定预算
 * 对 32 块和 63 块又都不合适（后者是用户确认过代价的超长稿），
 * 所以按块摊：正常每块 30~60 秒，90 秒/块留足余量，卡死的上游会很快耗光它。
 */
const BUDGET_PER_CHUNK_MS = 90_000;
/** 预算下限：只有一两块时也得给够一次重试的时间 */
const MIN_TIME_BUDGET_MS = 300_000;
/** 完整复述 1600 字 ≈ 1100 token，思考类模型另耗 2000+ 推理 token，需留足余量 */
const CHUNK_MAX_TOKENS = 6144;
/** 每块最多尝试次数（1 次重试）：思考类模型的敷衍输出有随机性，重试通常可恢复 */
const MAX_ATTEMPTS_PER_CHUNK = 2;
/**
 * 并发块数。
 *
 * 块之间互不依赖（各排各的、按下标归位），而中转站单次要几十秒——实测同一条
 * 提示词在思考类模型上 75~130 秒都出现过。串行的话 4400 字的稿子要发 3 次、
 * 拖上好几分钟，是整条链路里仅次于转写的一段。3 路够把常见长度压到一次请求的
 * 时间，又不至于撞上限流。
 */
const FORMAT_CONCURRENCY = 3;

const SYSTEM_PROMPT = "你是文字稿排版助手，负责为语音转写文本补标点、分段。";

/** 术语表上限：够覆盖一条内容的专名，又不至于把提示词撑大 */
const GLOSSARY_MAX_TERMS = 40;
/** 简介只取前若干字符：长简介后半段多是推广话术与链接，提不出有效术语 */
const GLOSSARY_SOURCE_MAX_CHARS = 500;
/** 单条术语的最短长度：单个字母（「C 语言」的 C）噪音大于价值 */
const GLOSSARY_MIN_TERM_CHARS = 2;

/**
 * 连续的拉丁词串。相邻两词以单个空格相连时并成一条——
 * 「GitHub Actions」是一个专名而不是两个，拆开就对不上转写里的错误形式。
 */
const LATIN_RUN = /[A-Za-z][A-Za-z0-9.+#_-]*(?: [A-Za-z][A-Za-z0-9.+#_-]*)*/g;

/**
 * 从标题与简介里提取拉丁字母专名，作为排版时的术语表。
 *
 * 只收拉丁串是有实测依据的：本地引擎（SenseVoiceSmall）纯中文逐字正确，
 * 中英混杂时错误全部落在拉丁词上——Docker 听成 dacker、useState 听成
 * us state、TypeScript 听成 typepescript。而被毁掉的恰好是专有名词，
 * 也就是全文检索与语义召回最依赖的那批词。
 *
 * SenseVoice 不支持热词（funasr 里只有 paraformer 系支持），这层等于
 * 在文本侧补上同一件事，不额外发起任何模型调用。
 */
export function extractGlossaryTerms(
  ...sources: (string | null | undefined)[]
): string[] {
  const seen = new Set<string>();
  const terms: string[] = [];
  for (const source of sources) {
    const text = source?.trim().slice(0, GLOSSARY_SOURCE_MAX_CHARS);
    if (!text) {
      continue;
    }
    for (const match of text.match(LATIN_RUN) ?? []) {
      // 句末标点会被贴进词尾（Node.js. / Docker-），统一剥掉
      const term = match.replace(/[.+#_-]+$/, "");
      const key = term.toLowerCase();
      if (term.length < GLOSSARY_MIN_TERM_CHARS || seen.has(key)) {
        continue;
      }
      seen.add(key);
      terms.push(term);
      if (terms.length >= GLOSSARY_MAX_TERMS) {
        return terms;
      }
    }
  }
  return terms;
}

/**
 * 说话人对话体的排版约束。
 *
 * 分离后的正文每段以「说话人 N：」开头，而排版这一步是要重写正文的——
 * 不明确要求保留，模型会把前缀当成口播里的赘语删掉，或者把相邻段落合并，
 * 分离就白做了。提示词管不住排版，所以 `rejectFormattedChunk` 还会数一遍。
 */
const SPEAKER_INSTRUCTION = [
  "",
  "本段文字稿已区分说话人，每个自然段以「说话人 N：」开头。整理时必须原样保留每个",
  "「说话人 N：」前缀（包括编号），不得删除、改写或增补；不同说话人的段落不得合并，",
  "段落顺序也不得调整。",
].join("\n");

/**
 * 常驻的通用技术专名。
 *
 * 标题与简介抓到的是这一条内容特有的专名（pi-agent、seaco-paraformer），
 * 但实测毁得最狠的恰恰是那些哪条技术视频都会出现、又偏偏不会写进标题的词。
 * 一条讲 Agent 的抖音视频里：SQL 听成「circle」、function call 成了「方声扣」、
 * Python 成了「拍摄」（"很多同学擅长的其实是拍摄"）、Claude Code 成了
 * 「C code」、schema 成了「sma」「scama」、harness 成了「哈尼斯」。
 * 这些词不依赖具体内容，值得常驻。
 *
 * 收得克制：每多一条都在撑大提示词，也多一分被过度匹配的机会。只放
 * 「几乎必然出现 + 音译后完全不可读」的那批，泛用词（data、model、user）
 * 不收——它们即便听错也还原得出来，反倒容易误伤。
 */
const COMMON_TECH_TERMS = [
  // 语言与查询
  "Python", "JavaScript", "TypeScript", "Java", "Rust", "Golang", "SQL", "CSS", "HTML",
  // 接口与数据格式
  "API", "JSON", "YAML", "Markdown", "HTTP", "schema", "function call", "SDK", "CLI",
  // AI
  "LLM", "token", "prompt", "embedding", "RAG", "Agent", "MCP", "GPT", "Claude",
  "Gemini", "Transformer",
  // 工具与生态
  "Git", "GitHub", "Docker", "npm", "Node.js", "React", "Vue", "VS Code",
  "Claude Code", "Cursor", "harness",
  // 存储
  "MySQL", "PostgreSQL", "Redis", "MongoDB", "SQLite",
];

/**
 * 术语表指令。措辞刻意收紧到「把文中已有的错误形式改成表中写法」：
 * 放开成自由改写就是拿幻觉换错字，比留着错字更糟。
 *
 * 两份表分开列而不是合并：前者是这条内容特有的、优先级更高，后者是通用的。
 * 合成一锅会让模型分不清哪些是「作者真的说过的专名」。
 */
function buildGlossaryInstruction(glossary: string[]): string {
  const lines = [""];
  if (glossary.length > 0) {
    lines.push(`本条内容的专有名词表（取自标题与简介）：${glossary.join("、")}`);
  }
  lines.push(`常见技术名词：${COMMON_TECH_TERMS.join("、")}`);
  lines.push(
    "语音转写常把这类名词听错（如 dacker → Docker、us state → useState、" +
      "circle → SQL）。文中出现表内名词的错误形式时，改成表中写法——" +
      "这是第 4 条的唯一放宽之处。表以外的词不要改动；" +
      "表中没有在文里出现的词，也不要添加进去。",
  );
  return lines.join("\n");
}

/**
 * 排版指令并入 user 消息（部分中转站会丢弃或弱化 system 消息），
 * 原文用显式分隔符包裹，避免被误当成对话内容。
 */
const FORMAT_INSTRUCTIONS = [
  "下面是一段语音转写的原始文字，请整理排版，要求：",
  "1. 为全文添加或修正标点符号；",
  "2. 按语义划分自然段，段落之间用空行分隔；",
  // 只删纯寒暄，不碰结尾那段「源码在 GitHub / 我做了 Python 版 / 有 PDF 版」——
  // 那看着像推广，实际是全篇最能直接用上的信息，删掉才是真的遗漏
  "3. 删除明显的口头填充词（嗯、啊、呃等）、开场与结尾**不含任何信息**的寒暄套话" +
    "（「话不多说」「感谢观看，我们下次见」这类），以及转写残留的多余空格；" +
    "凡是提到资源、链接、版本、后续计划的句子一律保留；",
  "4. 仅在十分确定时修正明显的同音错别字；",
  "5. 严格保持原文的表述与信息：不概括、不扩写、不调整语序、不遗漏内容、不添加原文没有的信息，整理后字数应与原文相当；",
  "6. 输入可能是长文字稿的中间片段，开头结尾不完整属正常，按原样整理；",
  "7. 直接输出整理后的正文，不要输出任何说明、标题或 Markdown 标记。",
].join("\n");

function buildChunkMessages(
  chunk: string,
  glossary: string[],
): AIChatMessage[] {
  // 通用词表不依赖具体内容，所以这一段总是给——此前只在标题/简介抓到专名时
  // 才附术语指令，而恰恰是没写进标题的那些通用词错得最狠
  const parts = [FORMAT_INSTRUCTIONS, buildGlossaryInstruction(glossary)];
  if (countSpeakerPrefixes(chunk) > 0) {
    parts.push(SPEAKER_INSTRUCTION);
  }
  const instructions = parts.join("\n");
  return [
    { role: "system", content: SYSTEM_PROMPT },
    {
      role: "user",
      content: `${instructions}\n\n【待整理文字稿开始】\n${chunk}\n【待整理文字稿结束】`,
    },
  ];
}

/** 从共享 AI 配置解析排版用 chat 模型；未配置返回 null */
export function resolveTranscriptFormatterConfig(): AIClientConfig | null {
  try {
    const config = coreAIConfigService.read();
    const chatModels = config.models.filter(
      (model) => model.capabilities?.chat !== false,
    );
    const byId = (id: string | undefined) =>
      id ? chatModels.find((model) => model.id === id) : undefined;
    const model =
      byId(config.modelRouteDefaults.fastText) ??
      byId(config.modelRouteDefaults.mainText) ??
      chatModels.find((candidate) => candidate.isDefault) ??
      chatModels[0];
    if (
      !model ||
      !model.apiUrl?.trim() ||
      !model.apiKey?.trim() ||
      !model.model?.trim()
    ) {
      return null;
    }
    return {
      provider: model.provider,
      apiProtocol: model.apiProtocol,
      apiKey: model.apiKey.trim(),
      apiUrl: model.apiUrl.trim(),
      model: model.model.trim(),
    };
  } catch (error) {
    console.warn("[media] 读取文字稿排版模型配置失败:", error);
    return null;
  }
}

/** 按句读边界把长文字稿切成排版块；无标点时退化到空格/硬切 */
export function splitTranscriptChunks(
  raw: string,
  maxChars = CHUNK_MAX_CHARS,
): string[] {
  const text = raw.trim();
  if (!text) {
    return [];
  }
  if (text.length <= maxChars) {
    return [text];
  }

  const boundary = /[。！？!?；;.\n\s]/;
  const chunks: string[] = [];
  let start = 0;
  while (start < text.length) {
    let end = Math.min(start + maxChars, text.length);
    if (end < text.length) {
      // 从窗口末尾向前找边界，避免把句子拦腰截断；最多回退半个窗口
      for (let i = end - 1; i > start + Math.floor(maxChars / 2); i--) {
        if (boundary.test(text[i])) {
          end = i + 1;
          break;
        }
      }
    }
    const chunk = text.slice(start, end).trim();
    if (chunk) {
      chunks.push(chunk);
    }
    start = end;
  }
  return chunks;
}

const PUNCTUATION_PATTERN = /[。，、！？：；…,.!?:;\n]/g;

/**
 * 排版输出验收：空输出、被 max_tokens 截断、长度跑偏、
 * 仍无标点分段（模型偷懒复读）都判不合格。返回不合格原因，合格返回 null。
 */
export function rejectFormattedChunk(
  rawChunk: string,
  result: AIChatResult,
): string | null {
  const cleaned = result.content.trim();
  if (!cleaned) {
    return "输出为空";
  }
  if (result.finishReason === "length") {
    return "输出被 max_tokens 截断";
  }
  const ratio = cleaned.length / rawChunk.length;
  if (ratio < 0.65 || ratio > 2) {
    return `输出长度异常（原文的 ${Math.round(ratio * 100)}%）`;
  }
  const punctuation = (cleaned.match(PUNCTUATION_PATTERN) ?? []).length;
  const required = Math.max(1, Math.floor(cleaned.length / 200));
  if (punctuation < required) {
    return `输出仍缺少标点分段（仅 ${punctuation} 处）`;
  }
  // 说话人前缀必须一条不少一条不多：少了是把标记当赘语删了或把段落合并了，
  // 多了是模型自己编的。两种都会让分离结果失真，而提示词拦不住
  const expectedSpeakers = countSpeakerPrefixes(rawChunk);
  if (expectedSpeakers > 0) {
    const actual = countSpeakerPrefixes(cleaned);
    if (actual !== expectedSpeakers) {
      return `说话人前缀数量不符（原文 ${expectedSpeakers} 处，输出 ${actual} 处）`;
    }
  }
  return null;
}

export interface FormatTranscriptOptions {
  signal?: AbortSignal;
  /**
   * 超过该长度直接跳过排版。默认是自动链路的上限；
   * 用户在详情页确认代价后可放开（传一个足够大的值）。
   */
  maxTotalChars?: number;
  /** 已完成块数（并发下不保证按顺序完成，报的是计数不是序号） */
  onProgress?: (completed: number, total: number) => void;
  /** 专有名词表（`extractGlossaryTerms` 从标题与简介提取），用于纠正听错的专名 */
  glossary?: string[];
  /** 整篇时间预算，默认按块数缩放；测试注入用 */
  timeBudgetMs?: number;
  /** 测试注入：底层 chat 调用 */
  chat?: typeof chatCompletion;
}

export interface FormatTranscriptResult {
  /** 排版后的文字稿；跳过时原样返回入参 */
  text: string;
  /** 非空表示本次没有排版（超长跳过），调用方据此留痕或告知用户 */
  skippedReason?: string;
  /**
   * 非空表示只排完了前面若干块，其余按原文拼回（内容不丢，只是没排版）。
   * 排到一半的成果不该因为后面某块失败就整篇作废——这些请求的钱已经花了，
   * 而半篇排好的稿子严格优于一篇没排的。
   */
  partialReason?: string;
}

/**
 * 分块排版整个文字稿并按空行拼接，块之间并发（见 `FORMAT_CONCURRENCY`）。
 *
 * 返回对象而非裸字符串：跳过与排版完成都给得出原文，
 * 只回字符串的话调用方分不清「排好了」和「太长没排」。
 */
export async function formatTranscript(
  raw: string,
  config: AIClientConfig,
  options?: FormatTranscriptOptions,
): Promise<FormatTranscriptResult> {
  const text = raw.trim();
  if (!text) {
    return { text };
  }
  const maxTotalChars = options?.maxTotalChars ?? TRANSCRIPT_FORMAT_LONG_CHARS;
  if (text.length > maxTotalChars) {
    return {
      text,
      skippedReason: `文字稿 ${text.length} 字，超过自动排版上限 ${maxTotalChars} 字`,
    };
  }

  const chat = options?.chat ?? chatCompletion;
  const glossary = options?.glossary ?? [];
  const chunks = splitTranscriptChunks(text);
  const deadline =
    Date.now() +
    (options?.timeBudgetMs ??
      Math.max(MIN_TIME_BUDGET_MS, chunks.length * BUDGET_PER_CHUNK_MS));
  console.log(
    `[media] 文字稿排版开始（model=${config.model}，共 ${chunks.length} 块，术语 ${glossary.length} 条）`,
  );

  // 每块在原文里的起始下标：中断时用它切出未排版的尾巴。
  // 拿 chunks 反拼会丢掉切分点上的空白（ASCII 会把两个词黏起来）
  const offsets = chunkOffsets(text, chunks);
  const done: (string | null)[] = new Array(chunks.length).fill(null);
  const reasons: string[] = new Array(chunks.length).fill("");
  let completed = 0;
  let nextIndex = 0;

  /** 排一块：验收不合格或请求失败都重试一次，仍不行就交回原因 */
  const runChunk = async (
    chunk: string,
    index: number,
  ): Promise<{ text: string | null; reason: string }> => {
    let lastReason = "";
    for (let attempt = 1; attempt <= MAX_ATTEMPTS_PER_CHUNK; attempt++) {
      options?.signal?.throwIfAborted();
      let result: AIChatResult;
      try {
        result = await chat(config, buildChunkMessages(chunk, glossary), {
          temperature: 0.1,
          maxTokens: CHUNK_MAX_TOKENS,
          signal: options?.signal,
          timeoutMs: CHUNK_TIMEOUT_MS,
        });
      } catch (error) {
        // 用户点的取消不该留下半成品，直接透传
        if (options?.signal?.aborted) {
          throw error;
        }
        // 记在这里而不是重试成功之后：超时与限流同样可能已经产生费用，
        // 而一篇长稿是几十块 × 最多两次尝试，漏记会低估一大截
        recordMainAiUsage({
          scenario: "formatting",
          model: config.model,
          failed: true,
        });
        const message = error instanceof Error ? error.message : String(error);
        if (attempt >= MAX_ATTEMPTS_PER_CHUNK) {
          return { text: null, reason: message };
        }
        console.warn(
          `[media] 第 ${index + 1}/${chunks.length} 块排版请求失败（${message}），重试一次`,
        );
        continue;
      }
      // 请求本身成功了就要记账，哪怕下面的验收把它判为不合格——
      // 钱已经花了，「重试一次」花的是第二笔
      recordMainAiUsage({
        scenario: "formatting",
        model: config.model,
        promptTokens: result.usage?.promptTokens,
        completionTokens: result.usage?.completionTokens,
      });
      const reason = rejectFormattedChunk(chunk, result);
      if (!reason) {
        return { text: result.content.trim(), reason: "" };
      }
      lastReason = reason;
      console.warn(
        `[media] 第 ${index + 1}/${chunks.length} 块排版验收不合格：${reason}${
          attempt < MAX_ATTEMPTS_PER_CHUNK ? "，重试一次" : ""
        }`,
      );
    }
    return { text: null, reason: `模型未按排版要求输出（${lastReason}）` };
  };

  const worker = async (): Promise<void> => {
    for (;;) {
      options?.signal?.throwIfAborted();
      const index = nextIndex++;
      if (index >= chunks.length) {
        return;
      }
      // 预算耗尽就不再发新请求；但第一块无论如何要试一次，
      // 否则一个过小的预算会让整篇直接失败而不是「没排版」
      if (index > 0 && Date.now() >= deadline) {
        return;
      }
      const outcome = await runChunk(chunks[index], index);
      done[index] = outcome.text;
      reasons[index] = outcome.reason;
      completed += 1;
      options?.onProgress?.(completed, chunks.length);
    }
  };

  await Promise.all(
    Array.from({ length: Math.min(FORMAT_CONCURRENCY, chunks.length) }, () =>
      worker(),
    ),
  );

  // 并发下「成功了几块」不再等于「排到第几块」：能留下的是**第一个没完成的块之前**
  // 那一段，它后面即便有块排好了也不能用——中间缺一块就接不上原文
  const firstIncomplete = done.findIndex((entry) => entry === null);
  if (firstIncomplete === -1) {
    return { text: done.join("\n\n") };
  }
  const reason = reasons[firstIncomplete] || "整篇排版超出时间预算";
  if (firstIncomplete === 0) {
    // 一块都没成，多半是模型名/鉴权这类硬故障，不能粉饰成「部分成功」
    throw new Error(reason);
  }
  console.warn(
    `[media] 文字稿排版中止于第 ${firstIncomplete + 1}/${chunks.length} 块：${reason}`,
  );
  const tail = text.slice(offsets[firstIncomplete]).trim();
  return {
    text: [done.slice(0, firstIncomplete).join("\n\n"), tail]
      .filter(Boolean)
      .join("\n\n"),
    partialReason: `已排版 ${firstIncomplete}/${chunks.length} 块，其余保留原始转写：${reason}`,
  };
}

/** 每块在原文中的起始下标（chunks 是 text 的连续切片，trim 过） */
function chunkOffsets(text: string, chunks: string[]): number[] {
  const offsets: number[] = [];
  let cursor = 0;
  for (const chunk of chunks) {
    const at = text.indexOf(chunk, cursor);
    const start = at >= 0 ? at : cursor;
    offsets.push(start);
    cursor = start + chunk.length;
  }
  return offsets;
}
