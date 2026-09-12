/** 仅为离屏 UI 验证生成夹具；不调用模型，不接触用户数据。 */
import fs from "node:fs/promises";
import path from "node:path";
import type { KnowledgeItem } from "@guizhi/shared/types";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { buildThemedReadingSource } from "../src/main/services/themed-reading/content";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

async function main() {
  const title = "生啤、熟啤、鲜啤、精酿：三个独立维度";
  const content = `# 啤酒标签的三个维度

生啤与熟啤描述处理方式，鲜啤关注新鲜程度，精酿涉及酿造主体与风格。阅读包装时，不要把它们误认为同一条品质等级。

## 生啤与熟啤

熟啤通常经过热处理；生啤采用其他方式处理。两者体现的是保存与风味之间的不同选择，不是高低之分。

## 鲜啤：看新鲜状态

新鲜程度还受生产日期、运输和保存条件影响。不能仅凭一个“鲜”字判断口感或品质。

## 精酿：看酿造与风格

精酿与是否热处理、新鲜程度属于不同维度。一瓶酒可以同时具有多个标签。

## 选购时依次检查

1. 阅读配料与产品说明。
2. 查看生产日期及保质期。
3. 核对保存条件，选择适合自己的风味。`;
  const source = await buildThemedReadingSource({ id: "fixture", title, content, itemType: "note" } as KnowledgeItem, "body");
  const now = Date.now();
  const page: ThemedReadingVersion = {
    id: "fixture-page", itemId: "fixture", sourceKind: "body", role: "current", formatVersion: 1, source,
    options: { style: "温暖啤酒手册风格，琥珀色与奶油色", generateImages: true, maxImages: 3 },
    assets: [{ id: "theme-hops", role: "generated", purpose: "酒花与麦穗主题插画", prompt: "Hops and barley, amber handbook illustration", alt: "酒花与麦穗", aspectRatio: "16:9", status: "failed", error: "图片请求超时（UI 测试夹具，可单独重试）" }],
    warnings: [], createdAt: now, updatedAt: now,
    design: { direction: "温暖、清晰的啤酒知识手册", assets: [], html: `<main class="handbook">${source.blocks.map((block, index) => `<div class="${index < 2 ? "intro" : "chapter"}" data-source-block="${block.id}"></div>`).join("")}</main>`, css: ":root{--theme-surface:#fff9e9;--theme-text:#372914;--theme-accent:#b26c12}[data-theme=dark]{--theme-surface:#24201b;--theme-text:#fff3d8;--theme-accent:#e9af52}.handbook{padding:32px;border-radius:20px;border-top:6px solid var(--theme-accent)}.intro{padding:12px 20px;background:linear-gradient(120deg,#efd294,#fff7de)}.chapter{padding:12px 20px}h1{font-size:32px;letter-spacing:1px}h2{color:var(--theme-accent);font-size:22px;border-bottom:1px solid var(--theme-accent);padding-bottom:12px}p,li{line-height:1.8}" },
  };
  const output = path.resolve("../../artifacts/themed-reading/ui-fixture.json");
  await fs.mkdir(path.dirname(output), { recursive: true });
  await fs.writeFile(output, JSON.stringify({ page, document: themedReadingDocument(page, "fixture-instance") }, null, 2), "utf8");
  process.stdout.write(`${output}\n`);
}
void main();
