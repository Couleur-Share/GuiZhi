// JS 值模型与证据脱敏的对拍基准：用真实的 packages/shared 实现生成输入输出，供 Go 的 jsvalue/evidence 测试逐字节比较。
// 用法：pnpm dlx tsx spikes/mygo-db/make-golden-evidence.mts <输出目录>   （写入 <输出目录>/evidence.json）
import fs from "node:fs";
import path from "node:path";
import { evidenceText, evidenceUrl } from "../../packages/shared/utils/evidence.ts";
import { sanitizeEvidenceJson, sanitizeEvidenceTarget } from "../../packages/shared/utils/evidence-sanitizer.ts";

const out = process.argv[2];
if (!out) throw new Error("用法: make-golden-evidence.mts <输出目录>");
fs.mkdirSync(out, { recursive: true });

type R = { out?: unknown; error?: true };
const run = (fn: () => unknown): R => {
  try {
    const v = fn();
    return v === undefined ? { out: null, undef: true } as R : { out: v };
  } catch {
    return { error: true };
  }
};

// ── JSON.parse → JSON.stringify 往返 ──
const jsonInputs = [
  "{}", "[]", "null", "true", "123", '"x"', "  [1, 2 , 3 ]  ",
  '{"b":1,"a":2,"10":3,"2":4,"01":5,"-1":6,"4294967295":7,"4294967294":8,"x":{"z":1,"1":2}}',
  '{"a":1,"b":2,"a":3}', '{"__proto__":1,"a":2}',
  "0", "-0", "0.1", "1.5e3", "1E21", "1e21", "1e-7", "1e-6", "123456789012345678901234567890", "0.000001", "0.0000001",
  "1.7976931348623157e308", "5e-324", "1e999", "-1e999", "100", "1e2", "12345678901234567890", "0.30000000000000004", "4.35", "2.5e-10",
  "9007199254740993", "-9007199254740993", "1.0", "1.10", "123e-2", "99999999999999999999", "999999999999999900000", "1e22",
  '"\\u0000\\u001f\\u007f\\u2028\\u2029\\b\\f\\n\\r\\t\\"\\\\\\/"', '"😀 中文 é"', '"\\ud83d\\ude00"', '"a\\u00e9b"', '"\\ud83d"', '"\\ude00x"', '"\\ud83dx"', '"\\ude00\\ud83d"', '"\\ud83d\\u0041"', '{"\\ud83d":1}',
  '[1,[2,[3,{"a":[]}]]]', '{"a":null,"b":false,"c":[null,true]}', "[ ]", "{ }", '{"":1}', '{"\\u0041":1}',
  "", "{", "[1,]", "{,}", '{"a":1,}', "01", "1.", ".5", "+1", "0x10", "NaN", "undefined", "'a'", '"a', '"\t"', '"\\x41"', "[1 2]", '{"a" 1}', "nul", "truee", "1e", "1e+", "-", "--1", "{}x", '"\\u12"',
];
const jsonCases = jsonInputs.map((input) => ({ input, ...run(() => JSON.stringify(JSON.parse(input))) }));

// ── evidenceUrl（输入用 JSON 值表示，包含非字符串）──
const urlInputs: unknown[] = [
  "https://example.com/a?b=1#frag", "http://user:pass@example.com:8080/p?x=1", "HTTP://EXAMPLE.com:80/A%20b", "https://example.com:443/",
  "ftp://example.com/x", "javascript:alert(1)", "data:text/plain,hi", "file:///C:/x", "mailto:a@b.com", "not a url", "", null, 0, false,
  "https://example.com/?token=abc&q=1", "https://example.com/?Token=abc&q=1&KEY=2", "https://example.com/?a=1&a=2&token=3&token=4",
  "https://example.com/?sig=1&signature=2&xsig=3&x-amz-credential=4&X-Goog-Algorithm=5&myauth=6&q=%E4%B8%AD%E6%96%87",
  "https://example.com/?q=a b&r=c+d&s=%41&t=~!*'()", "https://example.com/?token=1", "https://example.com/?token=1&", "https://example.com/?&token=1",
  "https://example.com/?a=%zz&token=1", "https://example.com/?a=1;b=2&auth=1", "https://example.com/?=x&token=1", "https://example.com/?token&q",
  "https://example.com/?q=中文&token=1", "https://例え.jp/パス?q=値&key=1#x", "https://xn--r8jz45g.jp/", "https://EXAMPLE.COM./", "https://example.com/a/../b/./c",
  "https://example.com/a b?c d#e f", "https://[::1]:8080/x", "https://127.0.0.1/", "https://0x7f.1/", "https://1.2.3/", "https://example.com:99999/", "https://exa mple.com/",
  "https://example.com\\a\\b", "https:example.com/x", "https:/example.com/x", "https:///example.com/x", "  https://example.com/x  ", "https://a.com/?q=1\n2",
  ["https://example.com/arr"], { a: 1 }, 5, true, "https://example.com/?x=1#", "https://example.com/#", "https://example.com/?",
  "https://u@example.com/", "https://:p@example.com/", "https://example.com/%7Euser/%2f", "https://example.com/?q=%E0%A4%A", "https://example.com/?password=1&x=2&PASSWORD=3",
  "https://example.com/?credentials=1&secrets=2&oauth=3&keyword=4&monkey=5",
];
const urlCases = urlInputs.map((input) => {
  const r = run(() => evidenceUrl(input as string));
  return { input, out: (r as { undef?: boolean }).undef ? null : r.out, undef: (r as { undef?: boolean }).undef ?? false };
});

// ── evidenceText ──
const textInputs: unknown[] = [
  "plain", "see https://example.com/a?token=1&q=2#x and (https://example.com/b?key=1) and http://a.com/c）后文",
  "https://a.com/x?token=1\nhttps://b.com/y?sig=1 https://c.com/z>tail", "ftp://x.com https://", "https://exa mple.com", "HTTPS://upper.com/a?token=1", "http://x/?a=1,https://y/?token=2",
  "[链接](https://example.com/?auth=1) 和 <https://example.com/?x=1>", "\"https://example.com/?token=1\"", "https://中文.com/路径?token=1",
  "https://a.com/?q=1\u00a0https://b.com/?token=1\u3000end", "https://a.com/?q=1\u2028https://b.com/?k=1", "😀https://example.com/?token=1😀", "",
  5, null, ["x"], { a: 1 },
];
const textCases = textInputs.map((input) => ({ input, ...run(() => evidenceText(input as string)) }));

// ── sanitizeEvidenceTarget ──
const targetInputs: unknown[] = [
  null, 0, "", "abc", "😀x", 5, true, [], ["a", "b"], {}, { itemId: "a", view: "x" }, { itemId: "a", selection: "see https://e.com/?token=1" },
  { selection: "" }, { selection: 5 }, { selection: ["x"] }, { selection: { a: 1 } }, { "2": "b", "1": "a", z: 1, selection: "plain" }, { selection: null, a: 1 },
];
const targetCases = targetInputs.map((input) => {
  try {
    const v = sanitizeEvidenceTarget(input as never);
    return { input, outJson: v === undefined ? null : JSON.stringify(v) };
  } catch {
    return { input, error: true };
  }
});

// ── sanitizeEvidenceJson ──
const url = "https://example.com/p?token=abc&q=1#frag";
const sanitizeCases: { json: string; cleared: string[]; out?: string; error?: true }[] = [];
const addSan = (value: unknown, cleared: string[] = []) => {
  const json = typeof value === "string" ? value : JSON.stringify(value);
  try {
    sanitizeCases.push({ json, cleared, out: sanitizeEvidenceJson(json, new Set(cleared)) });
  } catch {
    sanitizeCases.push({ json, cleared, error: true });
  }
};
addSan([]);
addSan([{ question: "q", answer: "a" }]);
addSan([{ role: "assistant", content: `see ${url}`, sources: [{ ordinal: 1, kind: "item", refId: "i1", sourceId: "i1", title: "T", text: `x ${url}`, url, target: { itemId: "i1", selection: `s ${url}` } }] }]);
addSan([{ sources: [{ ordinal: 1, kind: "item", refId: "i1", sourceId: "i1", title: "T", text: "secret text" }] }], ["i1"]);
addSan([{ evidenceSources: [{ ordinal: 2, kind: "item", refId: "i2", title: "T", text: "t", evidence: { version: 1, kind: "item", sourceId: "i2", title: "T", text: "body", fingerprint: "f", capturedAt: 123, reviewStatus: "needs_review", reviewReasons: ["r"], url } }] }], ["i2"]);
addSan([{ evidenceSources: [{ ordinal: 2, kind: "item", refId: "i2", title: "T", evidence: { version: 1, sourceId: "i2", title: "T", text: `b ${url}`, url, sourceItemIds: ["i9"] } }] }], ["i9"]);
addSan([{ evidenceSources: [{ ordinal: 3, kind: "web", title: "W", url, evidence: { sourceId: "w", title: "W", text: `w ${url}`, url } }] }]);
addSan([{ sources: [{ kind: "web", sourceItemIds: ["c1", "c2"], title: "W" }] }], ["c2"]);
addSan([{ sources: [{ kind: "item", refId: "r1", title: "no sourceId, uses refId" }] }], ["r1"]);
addSan([{ sources: [{ kind: "wiki", refId: "r1", title: "refId ignored when kind != item" }] }], ["r1"]);
addSan([{ sources: [{ target: { itemId: "t1" }, title: "via target" }] }], ["t1"]);
addSan([{ sources: [{ sourceId: "", target: { itemId: "t1" }, title: "empty sourceId is not nullish" }] }], ["t1"]);
addSan([{ sources: [{ sourceId: 5, title: "number id" }] }], ["5"]);
addSan([{ sources: [{ sourceId: null, target: { itemId: "t2" } }] }], ["t2"]);
addSan([{ sources: [null, 1, "s", true, [], [1, 2], ["x"], {}] }]);
addSan([{ sources: [{ sourceItemIds: "notarray" }] }]);
addSan([{ sources: [{ sourceItemIds: "notarray", sourceId: "hit" }] }], ["hit"]);
addSan([{ sources: [{ sourceItemIds: null, evidence: { sourceItemIds: ["e1"] }, title: "x" }] }], ["e1"]);
addSan([{ sources: [{ evidence: "str", title: "x" }] }]);
addSan([{ sources: [{ evidence: 5, title: "x", text: 5 }] }]);
addSan([{ sources: [{ url: 5, text: "t" }, { url: ["https://example.com/arr?token=1"] }, { url: {} }, { url: "" }, { url: "ftp://x" }] }]);
addSan([{ sources: [{ target: "abc" }, { target: 7 }, { target: [1, 2] }, { target: { selection: 5 } }] }]);
addSan([{ sources: [{ target: { selection: 5 } }] }]);
addSan([{ context: { target: { itemId: "c1", view: "v", selection: `sel ${url}` }, sources: [{ sourceId: "c1", title: "inherits" }, { title: "inherited id only" }], extra: 1 } }]);
addSan([{ context: { target: { itemId: "c1" }, sources: [{ title: "inherited id c1" }] } }], ["c1"]);
addSan([{ context: { target: { itemId: "c1" }, sources: [{ title: "inherited id c1" }] } }], ["other"]);
addSan([{ context: { target: { itemId: "c1" }, sources: [{ title: "x" }] } }, { context: { target: { itemId: "c2" }, sources: [{ title: "inh" }] } }], ["c2"]);
addSan([{ context: { target: { itemId: "" } } }]);
addSan([{ context: { target: { itemId: 0 } } }]);
addSan([{ context: { target: { itemId: 5 } } }], ["5"]);
addSan([{ context: { target: null, sources: null } }]);
addSan([{ context: { target: undefined } }]);
addSan([{ context: { sources: "str" } }]);
addSan([{ context: { sources: {} } }]);
addSan([{ context: "str" }]);
addSan([{ context: 5 }]);
addSan([{ context: [1, 2] }]);
addSan([{ context: { target: "abc" } }]);
addSan([{ context: {}, evidenceCleared: "old" }]);
addSan([{ evidenceCleared: "old", context: { target: { itemId: "z" } } }], ["z"]);
addSan([{ context: false, evidenceCleared: 1 }]);
addSan([{ context: 0 }]);
addSan([{ context: { target: { itemId: "c1" } }, sources: [{ sourceId: "c1" }] }], ["c1"]);
addSan([null, 1, "m", true, [], [1, 2], { a: 1 }]);
addSan([["x", "y"], { "2": 1, "1": 2, b: 1, "0": 0 }]);
addSan([{ "b": 1, "a": 2, "10": 3, "2": 4, sources: [{ "z": 1, "5": 2, "1": 3, text: "t" }] }]);
addSan('{"a":1}');
addSan("null");
addSan('"x"');
addSan("not json");
addSan("");
addSan('[{"a":1,"a":2}]');
addSan('[{"sources":[{"title":"dup","title":"dup2"}]}]');
addSan('[ {"a" : 1 , "b":[ 1 , 2 ] } ]');
addSan([{ text: "t", sources: [], evidenceSources: [] }]);
addSan([{ sources: "notarray", evidenceSources: {} }]);
addSan([{ sources: [{ ordinal: 1.5, capturedAt: 1e21, n: 1e-7, big: 12345678901234567890, neg: -0 }] }]);
addSan([{ sources: [{ text: "😀 emoji \u2028 \u0000 \u007f", title: "中文" }] }]);

fs.writeFileSync(path.join(out, "evidence.json"), JSON.stringify({ jsonCases, urlCases, textCases, targetCases, sanitizeCases }));
console.log("evidence golden ok:", out, `json=${jsonCases.length} url=${urlCases.length} text=${textCases.length} target=${targetCases.length} sanitize=${sanitizeCases.length}`);
