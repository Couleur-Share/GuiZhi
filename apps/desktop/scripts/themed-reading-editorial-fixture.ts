/** 使用用户原文与图片制作两份内容设计；与应用共用清理、回填、目录和导出路径。 */
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";
import type { ThemedReadingAsset, ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";
import { escapeThemedText } from "../src/main/services/themed-reading/content";

const directory = path.resolve("../../artifacts/themed-reading/editorial");
const inputs = [
  { id: "fish-oil", file: "C:/Users/g2014/Desktop/有在吃鱼油的佬吗.html" },
  { id: "beer", file: "C:/Users/g2014/Desktop/生啤、熟啤、鲜啤、精酿的区别：三个独立维度与选购判断方法.html" },
];
const hash = (value: string | Buffer) => createHash("sha256").update(value).digest("hex");
const text = (value: string) => value.replace(/\s+/gu, " ").trim();
const imageHash = (uri: string) => hash(Buffer.from(uri.slice(uri.indexOf(",") + 1), "base64"));
const slot = (index: number, cls = "") => `<div class="${cls}" data-source-block="b${index}"></div>`;

async function redesign(input: typeof inputs[number]) {
  const original = await fs.readFile(input.file, "utf8");
  const { document } = parseHTML(original);
  const assets: ThemedReadingAsset[] = [], urls: Record<string, string> = {};
  const expectedImages = [...document.querySelectorAll("img")].map(image => ({ alt: image.getAttribute("alt"), sha256: imageHash(image.getAttribute("src")) }));
  for (const [index, image] of [...document.querySelectorAll("img")].entries()) {
    const id = image.getAttribute("data-theme-asset") || `original-${index}`;
    const uri = image.getAttribute("src");
    assert.match(uri, /^data:image\/(png|jpeg|webp|gif|avif);base64,/);
    const fileName = `${input.id}-${index}.${uri.slice(11, uri.indexOf(";"))}`;
    assets.push({ id, role: image.hasAttribute("data-theme-asset") ? "generated" : "original", purpose: "原有图片", alt: image.getAttribute("alt"), status: "ready", fileName, originalUrl: `local-image://${fileName}` });
    urls[id] = uri;
    image.setAttribute("src", `local-image://${fileName}`);
  }
  const blocks = [...document.querySelectorAll("[data-source-block]")].map(element => ({ id: element.getAttribute("data-source-block"), html: element.innerHTML, text: element.textContent, markdown: element.textContent }));
  const title = document.title;
  const chapter = (index: number, id: string, cls = "") => `<section id="${id}" class="reading-section ${cls}">${slot(index)}${slot(index + 1)}</section>`;
  const heading = (index: number) => escapeThemedText(blocks[index].text.trim());
  const heroArt = `<figure class="reading-art"><img data-theme-asset="${assets.find(asset => asset.role === "generated").id}"></figure>`;
  let html: string;
  if (input.id === "fish-oil") {
    const entries = [[1, "food"], [3, "popular"], [5, "brands"], [7, "criteria"], [9, "experience"], [11, "synthesis"]] as const;
    html = `<article id="reading-top" class="chapter-0 reading-page reading-discussion">
      <header class="reading-masthead"><span class="reading-kicker">主题阅读</span><h1 class="reading-title">${escapeThemedText(title)}</h1>
      <div class="reading-hero">${slot(0, "reading-lead")}${heroArt}</div></header>
      <details class="reading-contents"><summary>文章目录</summary><nav>${entries.map(([index, id]) => `<a href="#${id}">${heading(index)}</a>`).join("")}</nav></details>
      <div class="reading-pair">${chapter(1, "food")}${chapter(3, "popular")}</div>
      ${chapter(5, "brands", "reading-comparison")}${chapter(7, "criteria", "reading-checklist")}
      ${chapter(9, "experience", "reading-notes")}${chapter(11, "synthesis", "reading-synthesis")}
      <nav><a href="#reading-top">返回顶部</a></nav></article>`;
  } else {
    html = `<article id="reading-top" class="chapter-0 reading-page reading-explainer">
      <header class="reading-masthead">${slot(0, "reading-context")}<h1 class="reading-title">${escapeThemedText(title)}</h1>
      <div class="reading-hero">${slot(1, "reading-lead")}${heroArt}</div></header>
      <nav class="reading-dimensions"><span>核心概念</span>${[[2, "sterilization"], [5, "freshness"], [8, "producer"]].map(([index, id]) => `<a href="#${id}">${heading(Number(index))}</a>`).join("")}</nav>
      <section id="sterilization" class="reading-section">${slot(2)}<div class="reading-dimension-body">${slot(3)}<figure class="reading-art">${slot(4)}</figure></div></section>
      <section id="freshness" class="reading-section">${slot(5)}<div class="reading-dimension-body">${slot(6)}<figure class="reading-art">${slot(7)}</figure></div></section>
      ${chapter(8, "producer", "reading-principle")}${chapter(10, "criteria", "reading-checklist reading-three")}
      <nav><a href="#reading-top">返回顶部</a></nav></article>`;
  }
  const page: ThemedReadingVersion = {
    id: `editorial-${input.id}`, itemId: input.id, sourceKind: "summary", role: "current", formatVersion: 1,
    source: { title, content: blocks.map(block => block.text).join("\n\n"), blocks, sourceUri: null, fingerprint: hash(original) },
    design: { direction: input.id === "fish-oil" ? "讨论梳理：并列体验、品牌对比、判断条件与观点对照" : "概念解释：三维概览、图文说明与判断清单", html, css: "", assets: [] },
    assets, options: { style: "内容驱动的专题阅读", generateImages: false, maxImages: 0 }, warnings: [], createdAt: 0, updatedAt: 0,
  };
  const files: Record<string, string> = {};
  for (const variant of ["offline", "embedded"]) {
    const html = themedReadingDocument(page, variant === "embedded" ? "editorial-fixture" : undefined, urls);
    const doc = parseHTML(html).document;
    assert.deepEqual([...doc.querySelectorAll("[data-source-block]")].map(node => ({ id: node.getAttribute("data-source-block"), text: text(node.textContent) })), blocks.map(block => ({ id: block.id, text: text(block.text) })));
    assert.deepEqual([...doc.querySelectorAll("img")].map(image => ({ alt: image.getAttribute("alt"), sha256: imageHash(image.getAttribute("src")) })), expectedImages);
    files[variant] = path.join(directory, `${input.id}-${variant}.html`);
    await fs.writeFile(files[variant], html);
  }
  assert.equal(hash(await fs.readFile(input.file)), hash(original));
  return { id: input.id, title, files, page, images: expectedImages, authoredDesign: true, sourceBlocks: blocks.map(block => ({ id: block.id, text: text(block.text) })) };
}

async function main() {
  await fs.mkdir(directory, { recursive: true });
  const fixtures = [];
  for (const input of inputs) fixtures.push(await redesign(input));
  await fs.writeFile(path.join(directory, "fixtures.json"), JSON.stringify({ modelCalls: 0, fixtures }, null, 2));
  process.stdout.write(`${directory}\n`);
}
void main();
