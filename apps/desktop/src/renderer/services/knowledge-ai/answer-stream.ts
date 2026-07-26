/**
 * 从未闭合的动作 JSON 里增量抽出 `text` 字段。
 *
 * Agent 协议要求模型输出 `{"action":"answer","text":"..."}`，流式下拿到的是
 * 半截 JSON——直接把原始分块喷到界面上，用户看到的是转义符和花括号。
 * 这里只认 `"text"` 后面那段字符串字面量，边到边解转义。
 *
 * 纯函数式状态机，便于单测。
 */

export interface AnswerStreamState {
  raw: string;
}

export function createAnswerStreamState(): AnswerStreamState {
  return { raw: "" };
}

const TEXT_KEY_PATTERN = /"text"\s*:\s*"/;
const ANSWER_ACTION_PATTERN = /"action"\s*:\s*"answer"/i;

/** JSON 字符串转义还原；末尾半个转义序列按未完成处理，留到下一块 */
function decodeJsonStringFragment(fragment: string): string {
  let output = "";
  let index = 0;
  while (index < fragment.length) {
    const char = fragment[index];
    if (char !== "\\") {
      output += char;
      index += 1;
      continue;
    }
    const escape = fragment[index + 1];
    if (escape === undefined) {
      break;
    }
    switch (escape) {
      case "n":
        output += "\n";
        break;
      case "t":
        output += "\t";
        break;
      case "r":
        output += "\r";
        break;
      case "b":
        output += "\b";
        break;
      case "f":
        output += "\f";
        break;
      case "u": {
        const hex = fragment.slice(index + 2, index + 6);
        if (hex.length < 4) {
          return output;
        }
        const code = Number.parseInt(hex, 16);
        output += Number.isNaN(code) ? "" : String.fromCharCode(code);
        index += 6;
        continue;
      }
      default:
        // \" \\ \/ 以及未知转义一律取原字符
        output += escape;
    }
    index += 2;
  }
  return output;
}

/** 找到字符串字面量的结束位置（未转义的引号）；未闭合返回 -1 */
function findStringEnd(raw: string, from: number): number {
  let index = from;
  while (index < raw.length) {
    const char = raw[index];
    if (char === "\\") {
      index += 2;
      continue;
    }
    if (char === '"') {
      return index;
    }
    index += 1;
  }
  return -1;
}

/**
 * 喂入一块原始输出，返回到目前为止的完整回答文本（尚未开始则为空串）。
 *
 * 返回全量而非增量：调用方直接覆盖显示即可。这一轮若最终被判为违规
 * （没读过资料就作答之类），下一轮换一份新 state，界面自然被新文本盖掉，
 * 不会留下上一轮的半截内容。
 *
 * 动作不是 answer、或 `"text"` 尚未出现时一律返回空——推理模型会在 JSON
 * 之前先吐一大段思考，那些不该进回答区。
 */
export function pushAnswerChunk(
  state: AnswerStreamState,
  chunk: string,
): string {
  state.raw += chunk;

  if (!ANSWER_ACTION_PATTERN.test(state.raw)) {
    return "";
  }
  const keyMatch = TEXT_KEY_PATTERN.exec(state.raw);
  if (!keyMatch) {
    return "";
  }
  const valueStart = keyMatch.index + keyMatch[0].length;
  const valueEnd = findStringEnd(state.raw, valueStart);
  const fragment = state.raw.slice(
    valueStart,
    valueEnd === -1 ? state.raw.length : valueEnd,
  );
  return decodeJsonStringFragment(fragment);
}
