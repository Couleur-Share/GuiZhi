/** 多内容类型的离线验证夹具：全部经过真实正文解析、清理与文档生成。 */
import fs from "node:fs/promises";
import path from "node:path";
import type { KnowledgeItem } from "@guizhi/shared/types";
import type { ThemedReadingSourceKind, ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { buildThemedReadingSource } from "../src/main/services/themed-reading/content";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

const technical = `# 本地知识文件的整理流程

这是一份纯离线界面验证教程，用来检查中文、英文标识符、代码和长表格的阅读效果。

## 一、先定义目录

1. 原始资料保留独立目录。
2. 生成内容保存到派生目录。
3. 检查成功后再更新索引。

\`\`\`typescript
const example = { title: "长字段不会挤出阅读区域", path: "${"very-long-segment/".repeat(18)}sample.md" };
console.log(example.title);
\`\`\`

## 二、检查字段映射

| 输入标识符 | 文件名 | 资源类型 | 内容来源 | 编码 | 检查状态 | 索引策略 | 重试方式 |
| --- | --- | --- | --- | --- | --- | --- | --- |
${Array.from({ length: 8 }, (_, index) => `| input_${index} | ${"long-name-".repeat(7)}${index}.md | Markdown | 本地示例 | UTF-8 | 已检查 | 内容指纹去重 | 单条重试 |`).join("\n")}

## 三、确认结果

> 完成条件：正文、代码和表格都保持完整，窄屏不会遮住关键字段。

技术教程尾部标记：TECHNICAL-END`;
const travel = `# 山城周末漫步手册

这是一份虚构旅行文章，仅用于验证阅读布局。路线、场景和安排均为示例。

## 清晨：沿河慢走

从旧桥旁出发，沿步道观察晨雾。给相机和自己都留一些停下来的时间。

- 随身物品：水杯、轻便外套、充电设备。
- 拍摄主题：桥梁线条、水面倒影、街角植物。
- 节奏建议：每走一段，就找一处能坐下的地方。

## 午后：穿过街巷

把巷口的咖啡馆作为休息点。可选择阅读一本书，或整理清晨拍下的照片。

## 傍晚：回到高处

沿台阶走到观景平台，在天色变化时结束当天行程。天气不适合时直接返回住处。

| 场景 | 主要活动 | 备用安排 |
| --- | --- | --- |
| 沿河步道 | 慢走与拍照 | 室内阅读 |
| 街巷 | 小店与休息 | 整理照片 |
| 高处平台 | 观察天色 | 提前返回 |

旅行文章尾部标记：TRAVEL-END`;
const forum = `## 讨论总结

# 围绕笔记整理的共同意见

参与者认为，先把资料放到容易检索的位置，再逐步整理，会比一次建完复杂目录更容易坚持。

## 已有共识

- 保留资料来源。
- 标题写清内容回答的问题。
- 复杂规则应该有可验证的理由。

## 仍有分歧

| 观点 | 支持理由 | 需要注意 |
| --- | --- | --- |
| 先分类后保存 | 目录一致 | 可能打断收集 |
| 先保存后分类 | 操作顺畅 | 需要定期整理 |

讨论总结尾部标记：FORUM-SUMMARY-END

## 正文

FORUM_BODY_EXCLUDED：这是主楼正文，不应混入讨论总结的主题页。

## 讨论

FORUM_REPLIES_EXCLUDED：这是原始回复，不应混入讨论总结的主题页。`;
const long = `# 长文的完整性与尾部可达性\n\n这份长文包含 120 个小节，用于离线验证完整呈现和阅读定位。\n\n${Array.from({ length: 120 }, (_, i) => `## 第 ${i + 1} 节：保存与理解\n\n${`这是第 ${i + 1} 节的原始段落。信息整理需要兼顾来源、上下文与后续检索，页面设计应完整呈现这些文字。`.repeat(5)}\n\n- 第一个观察：${i + 1}。\n- 第二个观察：保持顺序。\n- 第三个观察：保留尾部。${i % 15 === 0 ? `\n\n\`\`\`text\ncheckpoint_${i} = ${"a-long-identifier-".repeat(15)}\n\`\`\`` : ""}`).join("\n\n")}\n\n## 最后一节\n\n长文尾部标记：END-THEME-READING-2026`;

async function main() {
  const entries: Array<{ id: string; title: string; content: string; itemType: string; sourceKind: ThemedReadingSourceKind; palette: string; tail: string }> = [
    { id: "technical", title: "本地知识文件的整理流程（离线夹具）", content: technical, itemType: "note", sourceKind: "body", palette: ":root{--theme-surface:#f2f7fc;--theme-text:#163044;--theme-accent:#286eac}[data-theme=dark]{--theme-surface:#16232e;--theme-text:#e5f0fa;--theme-accent:#7bb9ec}", tail: "TECHNICAL-END" },
    { id: "travel", title: "山城周末漫步手册（虚构示例）", content: travel, itemType: "note", sourceKind: "body", palette: ":root{--theme-surface:#fbf6e9;--theme-text:#243a30;--theme-accent:#397d65}[data-theme=dark]{--theme-surface:#1b2b25;--theme-text:#f4f0df;--theme-accent:#8cbea9}", tail: "TRAVEL-END" },
    { id: "forum", title: "笔记整理讨论（离线夹具）", content: forum, itemType: "forum", sourceKind: "summary", palette: ":root{--theme-surface:#f6f2fb;--theme-text:#352747;--theme-accent:#7952a3}[data-theme=dark]{--theme-surface:#282134;--theme-text:#f2e8ff;--theme-accent:#baa0d7}", tail: "FORUM-SUMMARY-END" },
    { id: "long", title: "120 节长文完整性验证（离线夹具）", content: long, itemType: "note", sourceKind: "body", palette: ":root{--theme-surface:#fcf7ef;--theme-text:#362c22;--theme-accent:#9a6428}[data-theme=dark]{--theme-surface:#28231e;--theme-text:#f8edde;--theme-accent:#d4ac75}", tail: "END-THEME-READING-2026" },
  ];
  const fixtures = [];
  for (const entry of entries) {
    const source = await buildThemedReadingSource({ ...entry, id: `fixture-${entry.id}` } as unknown as KnowledgeItem, entry.sourceKind);
    const now = Date.now();
    const page: ThemedReadingVersion = { id: `fixture-${entry.id}`, itemId: `fixture-${entry.id}`, sourceKind: entry.sourceKind, role: "current", formatVersion: 1, source, options: { style: "离线排版验证", generateImages: false, maxImages: 3 }, assets: [], warnings: [], createdAt: now, updatedAt: now,
      design: { direction: "按主题配色，保留所有原文块", assets: [], html: `<main class="reading-sheet">${source.blocks.map((block) => `<div class="reading-block" data-source-block="${block.id}"></div>`).join("")}</main>`, css: `${entry.palette}.reading-sheet{padding:28px;border-top:5px solid var(--theme-accent);border-radius:16px}.reading-block{padding:8px 16px}h1{font-size:30px;border-bottom:2px solid var(--theme-accent);padding-bottom:16px}h2{font-size:22px;color:var(--theme-accent)}p,li{line-height:1.8}table{border-collapse:collapse}td,th{border:1px solid var(--theme-accent);padding:10px}pre{border-left:3px solid var(--theme-accent);padding:16px}` } };
    fixtures.push({ ...entry, page, document: themedReadingDocument(page, "fixture-instance") });
  }
  const output = path.resolve("../../artifacts/themed-reading/content-fixtures.json");
  await fs.mkdir(path.dirname(output), { recursive: true });
  await fs.writeFile(output, JSON.stringify(fixtures, null, 2));
  process.stdout.write(`${output}\n`);
}
void main();
