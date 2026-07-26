/**
 * AI 标签建议：为条目生成 2~3 个中文标签。
 */
import { TAG_SUGGESTION_SYSTEM_PROMPT, buildUserPrompt } from "./prompts";
import { runScenarioChat } from "./ai-invoke";

const TAG_MAX_LENGTH = 12;
const TAG_MAX_COUNT = 5;
/** 标签本身只要几十 token，余量留给思考类模型的推理消耗（推理计入 max_tokens） */
const TAG_MAX_TOKENS = 2048;

/** 解析模型输出的标签列表：容忍编号、井号、多种分隔符与引号污染。 */
export function parseTagResponse(response: string): string[] {
  const tags: string[] = [];
  for (const raw of response.split(/[,，、;；\n]/)) {
    const cleaned = raw
      .replace(/^\s*(?:\d+[.、)]\s*)?#?/, "")
      .replace(/["'「」『』`]/g, "")
      .trim();
    if (
      cleaned &&
      cleaned.length <= TAG_MAX_LENGTH &&
      !tags.includes(cleaned)
    ) {
      tags.push(cleaned);
    }
    if (tags.length >= TAG_MAX_COUNT) {
      break;
    }
  }
  return tags;
}

export async function suggestTags(
  title: string,
  content: string,
): Promise<string[]> {
  const text = content.trim();
  if (!text && !title.trim()) {
    throw new Error("内容为空，无法建议标签");
  }

  const result = await runScenarioChat(
    "tagging",
    [
      { role: "system", content: TAG_SUGGESTION_SYSTEM_PROMPT },
      {
        role: "user",
        content: buildUserPrompt(title.trim() || "无标题", text),
      },
    ],
    { temperature: 0.3, maxTokens: TAG_MAX_TOKENS },
  );

  const tags = parseTagResponse(result.content);
  if (tags.length === 0) {
    throw new Error("未能解析出有效标签");
  }
  return tags;
}
