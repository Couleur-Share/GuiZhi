/**
 * aiflow 回答流抽取的 golden：answer-stream.ts（半截 JSON 里增量抽出 "text" 字段并还原转义）。
 * 真实 TS 实现按分块逐次喂入，导出每一块之后的输出；Go 侧重放见 apps/desktop-go/aiflow/answer_stream_golden_test.go。
 *
 * 约定见 aiflow-golden-kit.ts / aiflow-golden-flow-kit.ts。
 * 孤立代理项的处理（lone: true 的用例）：TS 的输出里会出现孤立代理项（\ud83d 先到、\ude00 后到），
 * Go 的 string 表示不了，落成 U+FFFD（见 native-ui-development.md §8.4）。这类用例在导出时按 toWellFormed 的规则
 * 把孤立代理项换成 U+FFFD 再记录；没有标 lone 的用例若产生孤立代理项会直接报错。
 */
import { describe, it } from "vitest";
import { createAnswerStreamState, pushAnswerChunk } from "../../../src/renderer/services/knowledge-ai/answer-stream";
import { writeOrCompareGolden } from "./aiflow-golden-kit";
import { A, C, L, compactText, render, safeText, type Raw, type Spec } from "./aiflow-golden-flow-kit";

interface StreamCase {
  name: string;
  chunks: Spec[];
  lone?: boolean;
}

/** 孤立代理项 → U+FFFD（与 String.prototype.toWellFormed 一致，自己写一份免得依赖运行时版本）。 */
function wellFormed(text: string): string {
  let out = "";
  for (let i = 0; i < text.length; i++) {
    const code = text.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = text.charCodeAt(i + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        out += text[i] + text[i + 1];
        i++;
      } else {
        out += "\ufffd";
      }
    } else {
      out += code >= 0xdc00 && code <= 0xdfff ? "\ufffd" : text[i];
    }
  }
  return out;
}

/** 把整段文本切成每块 n 个码元（n=1 即逐字符）；不会把代理对切开。 */
function slice(text: string, size: number): string[] {
  const chunks: string[] = [];
  for (let i = 0; i < text.length; ) {
    let end = Math.min(text.length, i + size);
    const last = text.charCodeAt(end - 1);
    if (end < text.length && last >= 0xd800 && last <= 0xdbff) end++;
    chunks.push(text.slice(i, end));
    i = end;
  }
  return chunks;
}

const ANSWER = (text: string): string => `{"action":"answer","text":"${text}"}`;

const SAMPLE = '{"action":"answer","text":"归知用 embedding 做语义检索。\\n第二行 \\"引用\\" \\u4f60\\u597d 😀 结束"}';

const CASES: StreamCase[] = [
  // 基本形态
  { name: "动作是 answer：逐块吐出累计文本", chunks: ['{"action"', ':"answer","te', 'xt":"归知用 ', "embedding 做", '语义检索。"}'] },
  { name: "检索动作不吐任何内容", chunks: ['{"action":"search","query":"语义检索"}'] },
  { name: "推理模型在 JSON 之前的思考不进回答区", chunks: ["让我先想想用户在问什么。", '好的。{"action":"answer","text":"结论是 A。"}'] },
  { name: "转义序列被还原，半个转义留到下一块", chunks: ['{"action":"answer","text":"第一行\\', 'n第二行 \\"引用\\" 结束"}'] },
  { name: "字符串里的引号不会被当成结束", chunks: ['{"action":"answer","text":"他说\\"好\\"，然后走了"}'] },
  { name: "\\u 转义还原", chunks: ['{"action":"answer","text":"\\u4f60\\u597d"}'] },
  { name: "text 在 action 之前出现也能识别", chunks: ['{"text":"先给文本","action":"answer"}'] },
  { name: "空块不改变状态", chunks: ["", '{"action":"answer","text":"甲', "", '乙"}', ""] },
  { name: "text 值为空串", chunks: ['{"action":"answer","text":""}'] },
  { name: "text 值未闭合：流式中间状态", chunks: ['{"action":"answer","text":"还没写完'] },
  { name: "text 不是字符串（null）：没有可抽取的内容", chunks: ['{"action":"answer","text":null}'] },
  { name: "text 的冒号与引号之间有换行", chunks: ['{"action":"answer","text"\n:\n"换行之后"}'] },
  { name: "完成之后追加的内容不影响结果", chunks: [ANSWER("完成"), ' 后面还有 {"text":"别的"}'] },
  { name: "先检索后回答：任意位置出现 answer 动作即可", chunks: ['{"action":"search","query":"x"}', '\n{"action":"answer","text":"检索后的结论"}'] },
  { name: "两处 text：以第一处为准", chunks: ['草稿 {"text": "draft"} 最终 ', '{"action":"answer","text":"final"}'] },

  // 单字符转义
  { name: "全部单字符转义：n t r b f 引号 反斜杠 斜杠 与未知转义", chunks: [ANSWER('a\\nb\\tc\\rd\\be\\ff\\"g\\\\h\\/i\\qj')] },
  { name: "转义的反斜杠之后紧跟引号：字符串在此结束", chunks: [ANSWER("末尾反斜杠\\\\"), " 以后的内容忽略"] },
  { name: "转义的反斜杠后接 u：按字面处理", chunks: [ANSWER("\\\\u4f60")] },
  { name: "末尾孤立的反斜杠留到下一块", chunks: ['{"action":"answer","text":"甲\\', '\\乙"}'] },
  { name: "反斜杠后接引号被拆在两块之间", chunks: ['{"action":"answer","text":"他说\\', '"好\\"了"}'] },
  { name: "反斜杠后接 u 时字符串提前结束（\\u 后不足 4 位）", chunks: ['{"action":"answer","text":"ab\\u12"}'] },

  // \u 转义
  { name: "\\u 半个转义：不足 4 位时先不输出，补齐后还原", chunks: ['{"action":"answer","text":"x\\u4', "f6", '0y"}'] },
  { name: "\\u 十六进制大小写混用", chunks: [ANSWER("\\u4F60\\u597D\\u00e9\\u00C9")] },
  { name: "\\u 后不是十六进制：NaN 不输出，继续往后", chunks: [ANSWER("a\\uzzzzb\\u00zzc")] },
  { name: "\\u 与 parseInt 的怪异形态：前导空白、正负号、0x 前缀、尾部多余字符", chunks: [ANSWER("[\\u 1a2][\\u+123][\\u-123][\\u0x12][\\u12  ][\\u1g2h][\\u-000]")] },
  { name: "\\u0000 与 \\u2028 等特殊码位", chunks: [ANSWER("a\\u0000b\\u2028c\\u2029d\\u00a0e")] },
  { name: "\\u 转义拼成完整的代理对（emoji）", chunks: [ANSWER("\\ud83d\\ude00")] },
  { name: "代理对的两半被拆在两块之间：中间状态是孤立代理项", chunks: ['{"action":"answer","text":"\\ud83d', '\\ude00"}'], lone: true },
  { name: "孤立的高代理项在末尾", chunks: [ANSWER("甲\\ud83d")], lone: true },
  { name: "孤立的低代理项", chunks: [ANSWER("甲\\ude00乙")], lone: true },
  { name: "高代理项后面不是低代理项", chunks: [ANSWER("\\ud83dX")], lone: true },
  { name: "低代理项在前、高代理项在后", chunks: [ANSWER("\\ude00\\ud83d")], lone: true },
  { name: "原文直接含 emoji 与生僻汉字", chunks: ['{"action":"answer","text":"😀', '𠀀 好"}'] },

  // 空白与大小写（JS 的 \s 与非 Unicode /i 的边界）
  { name: "冒号两侧的空格", chunks: ['{"action" : "answer","text" : "有空格"}'] },
  { name: "冒号两侧是换行与制表", chunks: ['{"action"\n\t:\r\n"answer","text"\t:\n"有换行"}'] },
  { name: "JS 的 \\s 含 NBSP、全角空格、U+FEFF、U+2028：都能识别", chunks: ['{"action"\u00a0:\u3000"answer","text"\ufeff:\u2028"特殊空白"}'] },
  { name: "U+0085、U+200B、U+180E 不是 JS 的 \\s：不识别", chunks: ['{"action"\u0085:"answer","text":"甲"}', '{"action":\u200b"answer","text":"乙"}', '{"action":\u180e"answer","text":"丙"}'] },
  { name: "action 与 answer 不分大小写", chunks: ['{"ACTION":"ANSWER","text":"大写"}'] },
  { name: "混合大小写的 Action / Answer", chunks: ['{"Action": "Answer","text":"混合"}'] },
  { name: "text 键区分大小写：大写的 TEXT 不认", chunks: ['{"action":"answer","TEXT":"大写键"}'] },
  { name: "长 s（U+017F）与无点 i（U+0131）不折叠成 ASCII：不识别", chunks: ['{"action":"an\u017fwer","text":"甲"}', '{"act\u0131on":"answer","text":"乙"}'] },
  { name: "action 的值是别的动作但文本里提到 answer", chunks: ['{"action":"search","query":"\\"action\\":\\"answer\\"","text":"不该吐出"}'] },

  // 分块无关性与长文本
  { name: "整段一次喂入", chunks: [SAMPLE] },
  { name: "逐字符喂入同一段文本：最终结果与整段一致", chunks: slice(SAMPLE, 1) },
  { name: "每 7 个码元一块", chunks: slice(SAMPLE, 7) },
  { name: "长文本三块（输出只记指纹）", chunks: [[L('{"action":"answer","text":"'), C(3000)], [A(2000), L("\\n")], [C(500), L('"}')]] },
];

function run(c: StreamCase): Raw {
  const state = createAnswerStreamState();
  const outputs: unknown[] = [];
  for (const spec of c.chunks) {
    const chunk = render(spec);
    safeText(chunk);
    const output = pushAnswerChunk(state, chunk);
    outputs.push(compactText(c.lone ? wellFormed(output) : output));
  }
  return { outputs };
}

describe("aiflow golden：回答流抽取", () => {
  it("pushAnswerChunk 与已提交的 golden 一致", () => {
    const cases = CASES.map((c) => ({ name: c.name, chunks: c.chunks, lone: c.lone ?? false, expect: run(c) }));
    writeOrCompareGolden("answer-stream", { cases });
  });
});
