/** 将用户提供的两份导出只读重建为离线夹具；正文、布局和图片来自文件，不调用模型。 */
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { parseHTML } from "linkedom";
import type { ThemedReadingAsset, ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

const output = path.resolve("../../artifacts/themed-reading/user-regression");
const inputs = [
  { id: "fish-oil", file: "C:/Users/g2014/Desktop/有在吃鱼油的佬吗.html" },
  { id: "beer", file: "C:/Users/g2014/Desktop/生啤、熟啤、鲜啤、精酿的区别：三个独立维度与选购判断方法.html" },
];
const hash = (value: string | Buffer) => createHash("sha256").update(value).digest("hex");
const normalize = (value: string) => value.replace(/\s+/gu, " ").trim();
const imageHash = (uri: string) => hash(Buffer.from(uri.slice(uri.indexOf(",") + 1), "base64"));

async function fixture(input: typeof inputs[number]) {
  const before = await fs.readFile(input.file, "utf8");
  const { document } = parseHTML(before);
  const title = document.title;
  const style = document.querySelector("style")?.textContent ?? "";
  const modelStart = style.indexOf(".chapter-0{");
  const guardStart = style.indexOf("\nhtml,body{display:block!important");
  assert(modelStart > 0 && guardStart > modelStart, "输入应为本次两份导出，且包含可识别的模型 CSS 与安全样式边界");
  const css = style.slice(modelStart, guardStart);
  const images = [...document.querySelectorAll("img")];
  const expectedImages = images.map(image => ({ alt: image.getAttribute("alt"), sha256: imageHash(image.getAttribute("src")) }));
  const assets: ThemedReadingAsset[] = [];
  const assetUrls: Record<string, string> = {};
  for (const [index, image] of images.entries()) {
    const uri = image.getAttribute("src");
    assert.match(uri, /^data:image\/(png|jpeg|gif|webp|avif);base64,/u, "原文件中的图片必须已内嵌");
    const generatedId = image.getAttribute("data-theme-asset");
    const id = generatedId || `original-${index}`;
    const extension = uri.slice("data:image/".length, uri.indexOf(";"));
    const fileName = `${input.id}-${index}.${extension}`;
    const localUrl = `local-image://${fileName}`;
    assets.push({ id, role: generatedId ? "generated" : "original", purpose: "用户导出中的现有图片", prompt: "", alt: image.getAttribute("alt") || "", aspectRatio: "16:9", status: "ready", fileName, originalUrl: localUrl });
    assetUrls[id] = uri;
    image.setAttribute("src", localUrl);
  }
  const blocks = [...document.querySelectorAll("[data-source-block]")].map(slot => {
    const block = { id: slot.getAttribute("data-source-block"), html: slot.innerHTML, text: slot.textContent, markdown: slot.textContent };
    slot.innerHTML = "";
    return block;
  });
  for (const element of document.querySelectorAll("[data-source-layout]")) element.removeAttribute("data-source-layout");
  const content = blocks.map(block => block.text).join("\n\n");
  const page: ThemedReadingVersion = {
    id: `${input.id}-user-fixture`, itemId: input.id, sourceKind: "summary", role: "current", formatVersion: 1,
    source: { title, content, sourceUri: null, fingerprint: hash(content), blocks },
    options: { style: "保留用户导出的既有设计", generateImages: false, maxImages: 0 },
    design: { direction: "用户现有 HTML 的隔离回归夹具", html: document.body.innerHTML, css, assets: [] },
    assets, warnings: [], createdAt: 0, updatedAt: 0,
  };
  const rendered: Record<string, string> = {
    before,
    after: themedReadingDocument(page, undefined, assetUrls),
    embedded: themedReadingDocument(page, "user-fixture-instance", assetUrls),
  };
  if (input.id === "fish-oil") {
    // 仅展示经过真实安全生成器允许的 HTML 排版能力，不声称这是模型生成的新设计。
    const capabilityCss = "#chapter-0-brands ul{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;list-style-type:none}#chapter-0-brands li{--theme-surface:#e7eeea;--theme-text:#223b34;border:1px solid #99ada3;border-radius:10px;padding:16px;margin:0}@media(prefers-color-scheme:dark){#chapter-0-brands li{--theme-surface:#243d36;--theme-text:#eff6f1}}";
    rendered.capabilities = themedReadingDocument({ ...page, design: { ...page.design, css: page.design.css + capabilityCss } }, undefined, assetUrls);
  }
  const files: Record<string, string> = {};
  for (const [variant, html] of Object.entries(rendered)) {
    const result = parseHTML(html).document;
    const currentBlocks = [...result.querySelectorAll("[data-source-block]")].map(slot => ({ id: slot.getAttribute("data-source-block"), text: normalize(slot.textContent) }));
    assert.deepEqual(currentBlocks, blocks.map(block => ({ id: block.id, text: normalize(block.text) })), `${input.id}/${variant} 的正文必须逐块一致`);
    assert.deepEqual([...result.querySelectorAll("img")].map(image => ({ alt: image.getAttribute("alt"), sha256: imageHash(image.getAttribute("src")) })), expectedImages, `${input.id}/${variant} 的图片必须逐字节一致`);
    files[variant] = path.join(output, `${input.id}-${variant}.html`);
    await fs.writeFile(files[variant], html, "utf8");
  }
  assert.equal(hash(await fs.readFile(input.file)), hash(before), "生成夹具不能修改用户原文件");
  return { ...input, title, inputSha256: hash(before), files, images: expectedImages, blocks: blocks.map(block => ({ id: block.id, text: normalize(block.text) })), htmlStructurePreserved: true };
}

async function main() {
  await fs.mkdir(output, { recursive: true });
  const fixtures = [];
  for (const input of inputs) fixtures.push(await fixture(input));
  await fs.writeFile(path.join(output, "fixtures.json"), JSON.stringify({ createdAt: new Date().toISOString(), modelCalls: 0, fixtures }, null, 2), "utf8");
  process.stdout.write(`${path.join(output, "fixtures.json")}\n`);
}
void main();
