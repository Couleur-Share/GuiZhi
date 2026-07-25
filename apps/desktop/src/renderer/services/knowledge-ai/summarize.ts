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
const SUMMARY_MAX_TOKENS = 800;

export async function generateSummary(
  title: string,
  content: string,
): Promise<string> {
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
    return result.content.trim();
  }

  // map：逐片段提取要点
  const chunks = chunkContent(text);
  const partials: string[] = [];
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
  return reduced.content.trim();
}
