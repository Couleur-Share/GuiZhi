/**
 * aiflow/jsx.go 的 golden：Go 里模拟 JS 字符串与数字语义的叶子工具，逐项对拍 V8 的真实行为。
 * trim/\s 的空白集、大小写映射、NFKC、UTF-16 长度与截取、localeCompare（仅 ASCII）、Number() 与数字转字符串。
 * Go 侧见 apps/desktop-go/aiflow/jsx_golden_test.go；约定见 aiflow-golden-kit.ts。
 *
 * 不进 golden：切口落在代理对中间的截取（JS 得到孤立代理项，Go 字符串装不下，统一成 U+FFFD，见 jsx.go 头注释）；
 * 非 ASCII 的 localeCompare（取决于运行环境的区域，中文按拼音还是按码点不可复刻）。
 */
import { describe, it } from "vitest";
import { writeOrCompareGolden } from "./aiflow-golden-kit";

const STRINGS = [
  "", " ", "abc", "  abc  ", "\t\n abc \r\n", "\u00a0abc\u00a0", "\ufeffabc\ufeff", "\u2028abc\u2029",
  "\u3000全角空格\u3000", "\u180eabc\u180e", "\u200babc\u200b", "\u0085abc\u0085",
  "\u1680 abc \u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u202f\u205f",
  "a  b\t\tc\n\nd", "a\u00a0\u00a0b\u3000c\u2028d", "中文 标题  测试", "😀 emoji 😀", "𠮷野家 吉野家",
  "Straße", "STRASSE", "ǅungla", "İstanbul", "ıi", "ΑΣ", "ΑΣΑ", "ΣΑΣ", "ﬁnance", "ﬃ", "Ａｂｃ１２３", "① ② ³", "ｶﾞｷﾞ",
  "e\u0301", "\u212b", "ſ", "ǰ", "ŉ", "ᾳ", "Ⅷ", "ⓐ", "µ", "ÿ", "ǆ", "ꭰ", "ᏸ",
  "Wiki  Title", "  Mixed\tCASE\nText ", "AI（人工智能）", "ＧＰＴ－４", "a\u0000b", "ﷺ", "㍿", "ℌ",
];

const SLICES: Array<[number, number]> = [[0, 1], [0, 2], [0, 3], [1, 4], [2, 2], [3, 1], [0, 200], [5, 200], [0, 0]];

const LONE_SURROGATE = /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/;

const IDS = [
  "gpt-4", "gpt-4o", "GPT-4", "gpt-4.1", "gpt-4-turbo", "gpt-4o-mini", "claude-3-5-sonnet", "Claude-3", "claude-3-5-sonnet-20241022",
  "text-embedding-3-small", "text-embedding-3-large", "deepseek-chat", "deepseek-r1", "DeepSeek-V3", "qwen-plus", "Qwen3-235B-A22B",
  "o1", "o3-mini", "gemini-2.5-flash", "gemini-2.5-pro", "models/gemini", "a_b", "a-b", "a.b", "a b", "a/b", "a:b", "a", "A", "b", "B",
  "1", "10", "2", "", "model10", "model2", "Model", "model", "~x", "$x", "@x", "#x", "x_y", "X_Y", "x-y", "x+y", "x=y", "x|y", "(x)", "[x]",
];

const NUMBERS = [0, -0, 1, -1, 1.5, 0.1 + 0.2, 100, 1e20, 1e21, 1.5e21, 123456789012345680000, 1e-6, 1e-7, 1.5e-7, 123456.789,
  0.000001, 5e-324, 1.7976931348623157e308, 2 ** 53, -(2 ** 31), 4294967295, 1 / 3];

const TO_NUMBER = ["", " ", "\n", "12", " 12 ", "1e3", "1E3", "0x10", "0X1f", "0b11", "0B1", "0o7", "0O17", "-0x10", "+0x10", "1_000", "Infinity", "+Infinity",
  "-Infinity", "infinity", "+5", "-5", "5.", ".5", "-.5", "+.5e1", "abc", "12px", "١٢", "\u00a012\u00a0", "1,5", "\u22125", "1e", "e5", "--1", "+-1", ".", "+", "-",
  "0", "-0", "00012", "1.2.3", "0.0000001", "1e400", "-1e400", "1e-400", "9007199254740993", "NaN", "null", "true", "\ufeff7\ufeff", "0x", "0xg"];

describe("aiflow golden：JS 语义小工具", () => {
  it("与 V8 的真实行为一致", () => {
    const strings = STRINGS.map((s) => ({
      in: s,
      trim: s.trim(),
      trimStart: s.trimStart(),
      trimEnd: s.trimEnd(),
      upper: s.toUpperCase(),
      lower: s.toLowerCase(),
      nfkc: s.normalize("NFKC"),
      collapse: s.replace(/\s+/g, " "),
      removeSpaces: s.replace(/\s+/g, ""),
      fields: s.split(/\s+/).filter(Boolean),
      length: s.length,
      slices: SLICES.flatMap(([from, to]) => {
        const out = s.slice(from, to);
        return LONE_SURROGATE.test(out) ? [] : [{ from, to, out }];
      }),
      units: Array.from({ length: Math.min(s.length, 6) }, (_, i) => s.charCodeAt(i)),
    }));

    const compare = IDS.flatMap((a) => IDS.map((b) => ({ a, b, out: Math.sign(a.localeCompare(b)) })));

    const numberString = NUMBERS.map((n) => ({ in: n, out: String(n) }));
    const toNumber = TO_NUMBER.map((s) => {
      const n = Number(s);
      return { in: s, ok: !Number.isNaN(n), out: Number.isFinite(n) ? n : null, infinite: n === Infinity ? 1 : n === -Infinity ? -1 : 0 };
    });
    const sorted = [...IDS].sort((a, b) => a.localeCompare(b));

    writeOrCompareGolden("text", { strings, compare, numberString, toNumber, sorted });
  });
});
