/**
 * 阅读来源取文与改写的 golden 导出与校验（packages/shared/utils/reading-source.ts）。
 *
 * apps/desktop-go/native/notes 的 ReadingSourceText / ReplaceReadingSource 是它的 Go 移植：改写的结果直接写进条目正文，
 * 要求与 TS 逐字节一致（含小节标题匹配式 /^##\s*(…)\s*$/gm 的 JS 多行语义：行终止符四种、\s 跨行、贪婪回退）。
 * 做法：本测试用真实的 TS 实现跑一组固定语料（手写的边界用例加固定种子的随机组合），把「输入 → 取文结果、改写结果或错误文案」
 * 写成 JSON；Go 侧 native/notes/reading_source_golden_test.go 逐条重放并逐项核对。
 *
 *   - 平时（CI）：TS 当前行为必须等于已提交的 golden，否则说明 TS 变了而 Go 没跟上（或反过来）；
 *   - 有意改了行为：UPDATE_GOLDEN=1 pnpm --filter @guizhi/desktop exec vitest run tests/unit/import/reading-source-golden.test.ts
 *     重新导出，同时更新 Go 移植，一起提交。
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { readingSourceText, replaceReadingSource } from "@guizhi/shared/utils/reading-source";
import type { KnowledgeItemType } from "@guizhi/shared/types";
import type { ThemedReadingSourceKind } from "@guizhi/shared/types/themed-reading";

const GOLDEN_FILE = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../../../desktop-go/native/notes/testdata/reading-source-golden.json",
);

interface Outcome {
  text?: string;
  error?: string;
}

interface GoldenCase {
  itemType: KnowledgeItemType;
  kind: ThemedReadingSourceKind;
  content: string;
  replacement: string;
  source: Outcome;
  result: Outcome;
}

function run(fn: () => string): Outcome {
  try {
    return { text: fn() };
  } catch (error) {
    return { error: error instanceof Error ? error.message : String(error) };
  }
}

function makeCase(itemType: KnowledgeItemType, kind: ThemedReadingSourceKind, content: string, replacement: string): GoldenCase {
  const item = { itemType, content };
  return {
    itemType,
    kind,
    content,
    replacement,
    source: run(() => readingSourceText(item, kind)),
    result: run(() => replaceReadingSource(item, kind, replacement)),
  };
}

// ---------- 手写的边界用例 ----------

const META = "> 平台：V2EX · 作者：a · 1 条回复\n> 发布：2026-07-16";
const FORUM = [META, "", "## 讨论总结", "", "旧总结", "", "## 正文", "", "旧主楼", "", "## 讨论（2 条）", "", "**1 楼 · a**", "", "回复一", "", "**2 楼 · b**", "", "回复二"].join("\n");
const IMAGE = ["> 平台：图片 · 作者：测试", "", "旧文案", "", "![原图](local-image://a.png)", "", "![第二张](local-image://b.png) ![第三张](local-image://c.png)", "", "## 图中文字", "", "识别结果"].join("\n");

const REPLACEMENTS = ["新内容", "", "多段\n\n内容\n", "带\r\n换行\r\n", "😀 表情与 𠮷", "  前后空白  ", "## 正文\n伪标题\n", "## 讨论总结\n伪总结"];

function handwritten(): GoldenCase[] {
  const cases: GoldenCase[] = [];
  const add = (itemType: KnowledgeItemType, kind: ThemedReadingSourceKind, content: string, replacements: string[] = REPLACEMENTS) => {
    for (const replacement of replacements) cases.push(makeCase(itemType, kind, content, replacement));
  };
  const same = (itemType: KnowledgeItemType, kind: ThemedReadingSourceKind, content: string) => {
    const text = run(() => readingSourceText({ itemType, content }, kind)).text;
    if (text !== undefined) add(itemType, kind, content, [text, text.replace(/\n/g, "\r\n")]);
  };

  // 论坛：标准形态、CRLF、没有对应小节、重复小节、小节顺序颠倒
  for (const kind of ["body", "summary"] as const) {
    add("forum", kind, FORUM);
    add("forum", kind, FORUM.replace(/\n/g, "\r\n"), REPLACEMENTS.slice(0, 4));
    same("forum", kind, FORUM);
    same("forum", kind, FORUM.replace(/\n/g, "\r\n"));
  }
  const forums: string[] = [
    "只有游离内容，没有任何小节",
    "游离在前\n\n## 正文\n\n主楼\n\n## 讨论（1 条）\n\n**1 楼 · a**\n\n回复",
    "## 讨论总结\n\n总结甲\n\n## 正文\n\n主楼甲\n\n## 讨论总结\n\n总结乙\n\n## 正文\n\n主楼乙",
    "## 正文\n\n主楼\n\n## 讨论总结\n\n总结\n",
    "## 讨论总结\n总结\n## 讨论\n\n**1 楼 · a**\n\n回复",
    "## 讨论总结\n\n\n\n\n总结后有很多空行\n\n\n## 正文\n\n\n\n主楼",
    "## 正文",
    "## 正文\n",
    "## 正文\n\n\n",
    "\n\n## 正文\n\n主楼",
    "",
    META,
    META + "\n\n只有元数据后的游离内容",
    "## 正文\n\n主楼 😀 表情\n\n## 讨论总结\n\n总结 𠮷",
  ];
  for (const content of forums) {
    for (const kind of ["body", "summary"] as const) {
      add("forum", kind, content, REPLACEMENTS.slice(0, 4));
      same("forum", kind, content);
      add("forum", kind, content.replace(/\n/g, "\r\n"), REPLACEMENTS.slice(0, 1));
    }
  }

  // 小节标题的各种写法：空白、制表符、不带空格、跨行、全角括号的贪婪、不构成标题的近似写法
  const headingForums: string[] = [];
  const headings = [
    "##正文", "##   正文   ", "## 正文 ", "## 正文\u00a0", "##\t讨论总结", "##\u3000正文", "## 正文\ufeff",
    "##\n正文", "##\n\n讨论总结", "## 讨论", "## 讨论（2 条）", "## 讨论（楼主 3 条 · 原帖共 9 条）", "## 讨论（a）（b）",
    "## 讨论（a）b）", "## 讨论（未闭合", "## 讨论（）", "## 讨论（a）后面还有字", "## 讨论总结额外", "### 正文", " ## 正文", "#正文", "## 正 文",
    // 括号里的 [^\n]* 只排除 \n：孤立的 \r、U+2028 能被它吞进去，贪婪匹配失败后要回退到更靠前的「）」
    "## 讨论（a）\r后（b）x", "## 讨论（a）\u2028后（b）x", "## 讨论（a） \u2029 （b）",
  ];
  for (const heading of headings) {
    headingForums.push(`前言\n\n${heading}\n\n内容\n\n## 讨论总结\n\n总结`);
    headingForums.push(`${heading}\n\n内容甲\n\n## 正文\n\n内容乙`);
    headingForums.push(`## 正文\n\n主楼\n\n${heading}\n\n尾巴`);
  }
  // 行终止符：孤立的 \r、U+2028、U+2029 前后的标题
  for (const sep of ["\r", "\u2028", "\u2029"]) {
    headingForums.push(`前言${sep}## 正文${sep}主楼${sep}## 讨论总结${sep}总结`);
    headingForums.push(`## 正文${sep}${sep}主楼${sep}${sep}## 讨论（1 条）${sep}**1 楼 · a**`);
    headingForums.push(`## 讨论总结 ${sep} 总结 ${sep}## 正文 ${sep}主楼`);
  }
  for (const content of headingForums) {
    for (const kind of ["body", "summary"] as const) add("forum", kind, content, [REPLACEMENTS[0], REPLACEMENTS[2]]);
  }

  // 图片：文案、多张图、识别文字、没有文案、没有图、元数据
  const images: string[] = [
    IMAGE,
    IMAGE.replace(/\n/g, "\r\n"),
    "> 平台：图片 · 作者：测试\n\n![原图](local-image://a.png)",
    "只有文案",
    "![原图](local-image://a.png)",
    "![原图](local-image://a.png)\n\n文案夹在两张图之间\n\n![次图](local-image://b.png)",
    "文案\n\n## 图中文字\n\n识别\n\n还有一段",
    "## 图中文字\n\n识别",
    "文案\n\n![a](local-image://a.png)\n![b](local-image://b.png)\n\n![c](local-image://c.png)  \n\n文案二",
    "文案\n\n\n\n![a](local-image://a.png)\n\n\n\n## 图中文字\n\n\n识别",
    // 图片段落前后的空白（含制表符与不换行空格）不影响它被认作纯图片段落
    "文案\n\n  ![a](local-image://a.png)  \n\n## 图中文字\n\n识别",
    "文案\n\n\t![a](local-image://a.png)\u00a0\n\n![b](local-image://b.png)",
    "",
  ];
  for (const content of images) {
    add("image", "body", content, REPLACEMENTS.slice(0, 6));
    same("image", "body", content);
    add("image", "summary", content, REPLACEMENTS.slice(0, 1));
  }

  // 其余类型：整篇是正文；有无元数据块；总结报错
  const plains: string[] = [
    "整篇正文",
    "> 平台：B 站 · 作者：甲 · 时长：12:52\n\n正文开头\n\n第二段",
    "> 平台：B 站 · 作者：甲\n> 原标题：旧标题\n> 简介：简介里有 · 字符\n\n\n\n正文隔了几个空行",
    "> 普通引用不是元数据\n\n正文",
    "> 平台：B 站\r\n\r\n正文\r\n第二行",
    "",
  ];
  // 除论坛与图片外各类型走同一条路径，取两种有代表性的类型即可。
  for (const itemType of ["note", "video"] as const) {
    for (const content of plains) {
      add(itemType, "body", content, REPLACEMENTS.slice(0, 5));
      same(itemType, "body", content);
    }
    add(itemType, "summary", plains[0], REPLACEMENTS.slice(0, 1));
  }
  return cases;
}

// ---------- 固定种子的随机组合 ----------

function rng(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const FORUM_LINES = [
  "## 讨论总结", "## 正文", "## 讨论", "## 讨论（2 条）", "##正文", "##   讨论总结  ", "## 正文 ", "##\t正文", "## 讨论（a）（b）", "## 讨论（未闭合",
  "## 讨论总结额外", "### 正文", " ## 正文", "##", "## ",
  "主楼内容", "总结要点", "**1 楼 · a**", "回复一句", "- 要点", "😀 表情", "𠮷 与 é", "  ", "\u00a0", "\ufeff", "\u2028", "\u2029", "\r", "",
  "", "", "> 平台：V2EX · 作者：a", "> 发布：2026-07-16",
];
const IMAGE_LINES = [
  "文案一句", "文案二句", "![图](local-image://a.png)", "![图 2](local-image://b.png) ![x](y.png)", "![图](local-image://a.png)  ", "## 图中文字", "识别文字", "识别二",
  "> 平台：图片 · 作者：测试", "", "", "", "😀",
];
const PLAIN_LINES = ["正文一段", "第二段", "> 平台：B 站 · 作者：甲 · 时长：1:02", "> 原标题：旧", "> 普通引用", "- 列表", "", "", "😀", "\u2028"];

function randomCases(count: number): GoldenCase[] {
  const rand = rng(20261121);
  const pick = <T>(items: readonly T[]): T => items[Math.floor(rand() * items.length)];
  const cases: GoldenCase[] = [];
  const types: KnowledgeItemType[] = ["forum", "forum", "forum", "image", "note", "video"];
  for (let i = 0; i < count; i++) {
    const itemType = pick(types);
    const pool = itemType === "forum" ? FORUM_LINES : itemType === "image" ? IMAGE_LINES : PLAIN_LINES;
    const lines: string[] = [];
    if (rand() < 0.4) lines.push(META);
    const n = 2 + Math.floor(rand() * 12);
    for (let j = 0; j < n; j++) lines.push(pick(pool));
    const eolRoll = rand();
    let content = lines.join(eolRoll < 0.25 ? "\r\n" : "\n");
    if (rand() < 0.2) content += "\n";
    const kind: ThemedReadingSourceKind = itemType === "forum" && rand() < 0.5 ? "summary" : "body";
    const current = run(() => readingSourceText({ itemType, content }, kind)).text;
    const replacement = rand() < 0.15 && current !== undefined ? current : pick(REPLACEMENTS);
    cases.push(makeCase(itemType, kind, content, replacement));
  }
  return cases;
}

describe("阅读来源取文与改写的 golden（与 apps/desktop-go/native/notes 对拍）", () => {
  it("TS 当前行为等于已提交的 golden", () => {
    const cases = [...handwritten(), ...randomCases(200)];
    // 语料里要有足够多的真实改写与报错，避免只剩「原样返回」。
    const changed = cases.filter(c => c.result.text !== undefined && c.result.text !== c.content).length;
    const failed = cases.filter(c => c.result.error !== undefined).length;
    expect(cases.length).toBeGreaterThan(500);
    expect(changed).toBeGreaterThan(300);
    expect(failed).toBeGreaterThan(5);

    // 一行一个用例：体积小，改了语料时 diff 也看得清。
    const text = `{"cases":[\n${cases.map(c => JSON.stringify(c)).join(",\n")}\n]}\n`;
    if (process.env.UPDATE_GOLDEN === "1") {
      fs.mkdirSync(path.dirname(GOLDEN_FILE), { recursive: true });
      fs.writeFileSync(GOLDEN_FILE, text);
      return;
    }
    const committed = fs.existsSync(GOLDEN_FILE) ? fs.readFileSync(GOLDEN_FILE, "utf8") : "";
    expect(
      JSON.parse(committed || "null"),
      "阅读来源 golden 与 TS 当前行为不一致：确认这是有意的改动后，用 UPDATE_GOLDEN=1 重新导出并提交，同时更新 Go 侧移植",
    ).toEqual(JSON.parse(text));
  });
});
