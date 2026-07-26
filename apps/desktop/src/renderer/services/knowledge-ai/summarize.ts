/**
 * 条目摘要生成：短文单发，长文 map-reduce 分块。
 */
import {
  SUMMARY_CHUNK_SIZE,
  SUMMARY_MAP_SYSTEM_PROMPT,
  SUMMARY_SYSTEM_PROMPT,
  buildSummaryMapPrompt,
  buildSummaryReducePrompt,
  buildUserPrompt,
  chunkContent,
} from "./prompts";
import { runScenarioChat } from "./ai-invoke";

const SUMMARY_TEMPERATURE = 0.3;
/**
 * 摘要正文不超过 200 字（约 150 token），余量全部留给思考类模型的推理消耗——
 * 推理 token 同样计入 max_tokens，额度不够时模型会在思考阶段就被截断、
 * 只回一个空正文（v0.7.1 实际踩到过：800 token 被 799 个推理 token 吃光）。
 */
const SUMMARY_MAX_TOKENS = 4096;

export interface SummaryResult {
  text: string;
  /** 任一阶段撞上 max_tokens：结果能用，但话没说完，得让用户知道 */
  truncated: boolean;
}

export async function generateSummary(
  title: string,
  content: string,
): Promise<SummaryResult> {
  const text = content.trim();
  if (!text) {
    throw new Error("内容为空，无法生成摘要");
  }
  const displayTitle = title.trim() || "无标题";

  if (text.length <= SUMMARY_CHUNK_SIZE) {
    const result = await runScenarioChat(
      "summary",
      [
        { role: "system", content: SUMMARY_SYSTEM_PROMPT },
        { role: "user", content: buildUserPrompt(displayTitle, text) },
      ],
      { temperature: SUMMARY_TEMPERATURE, maxTokens: SUMMARY_MAX_TOKENS },
    );
    return {
      text: result.content.trim(),
      truncated: result.finishReason === "length",
    };
  }

  // map：逐片段提取要点
  const chunks = chunkContent(text);
  const partials: string[] = [];
  // 被截断的片段要点会残缺，reduce 出来的摘要同样不完整，所以逐段累计
  let truncated = false;
  for (const [index, chunk] of chunks.entries()) {
    const result = await runScenarioChat(
      "summary",
      [
        { role: "system", content: SUMMARY_MAP_SYSTEM_PROMPT },
        {
          role: "user",
          content: buildSummaryMapPrompt(
            displayTitle,
            chunk,
            index + 1,
            chunks.length,
          ),
        },
      ],
      { temperature: SUMMARY_TEMPERATURE, maxTokens: SUMMARY_MAX_TOKENS },
    );
    partials.push(result.content.trim());
    truncated ||= result.finishReason === "length";
  }

  // reduce：综合去重输出最终要点
  const reduced = await runScenarioChat(
    "summary",
    [
      { role: "system", content: SUMMARY_SYSTEM_PROMPT },
      {
        role: "user",
        content: buildSummaryReducePrompt(displayTitle, partials),
      },
    ],
    { temperature: SUMMARY_TEMPERATURE, maxTokens: SUMMARY_MAX_TOKENS },
  );
  return {
    text: reduced.content.trim(),
    truncated: truncated || reduced.finishReason === "length",
  };
}
