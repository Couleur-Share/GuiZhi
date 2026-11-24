// 把渲染层的 AI 厂商品牌图标（SVG）栅格化成 Viva 原生界面嵌入的 PNG。
// Rasterise the renderer's AI provider brand SVGs into the PNGs embedded by the Viva UI.
//
//   node apps/desktop-go/scripts/gen-viva-provider-icons.mjs            # 重写 viva/kit/assets/providers/*.png
//   node apps/desktop-go/scripts/gen-viva-provider-icons.mjs --out DIR  # 写到别处（对比用）
//
// 输入：apps/desktop/src/renderer/assets/providers/*.svg（ModelIcons.tsx 引用的 15 个，渲染层仍是事实来源）。
// 输出：120×120 透明底 PNG，文件名与 SVG 同名；Go 侧 viva/kit/provider_icon.go 用 go:embed 嵌入，
//       运行期按设备像素缩小（面积平均），不做 SVG 解析。
//
// 栅格化只用本机已有的 playwright + Chromium（apps/desktop-go 的 devDependency），完全离线：
// SVG 以内联 data URL 载入 <img>，页面不发任何网络请求；渲染规则与渲染层一致（<img> 宽高 = 画布，objectFit: contain）。
// 这是开发期工具，不进安装包，也不在 CI 跑；产物入库。要改图标：先改渲染层 SVG，再运行本脚本并一起提交。
//
// 这里的清单与 ModelIcons.tsx 的 import 一一对应（Spark 与 Hunyuan 共用 tencent-cloud-ti）；
// 增删厂商时同步 viva/kit/provider_icon.go 的 providerIconFiles。

import fs from "node:fs";
import path from "node:path";

import { chromium } from "playwright";

const REPO_ROOT = path.resolve(import.meta.dirname, "../../..");
const SRC_DIR = path.join(REPO_ROOT, "apps/desktop/src/renderer/assets/providers");
const DEFAULT_OUT = path.join(REPO_ROOT, "apps/desktop-go/viva/kit/assets/providers");

/** 画布边长（CSS 像素，deviceScaleFactor=1）。足够在 2× 缩放下画 40px 以内的图标仍然是缩小而非放大。 */
const SIZE = 120;

/** ModelIcons.tsx 引用的 15 个厂商 SVG（不含扩展名）。 */
const NAMES = [
  "openai",
  "anthropic",
  "azureai",
  "gemini",
  "deepseek",
  "qwen",
  "doubao",
  "zhipu",
  "moonshot",
  "mistral",
  "zero-one",
  "tencent-cloud-ti",
  "newapi",
  "ollama",
  "grok",
];

function parseArgs(argv) {
  let out = DEFAULT_OUT;
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--out" && argv[i + 1]) {
      out = path.resolve(argv[++i]);
    } else {
      throw new Error(`未知参数：${argv[i]}`);
    }
  }
  return { out };
}

async function main() {
  const { out } = parseArgs(process.argv.slice(2));
  fs.mkdirSync(out, { recursive: true });

  const browser = await chromium.launch();
  try {
    const context = await browser.newContext({
      viewport: { width: SIZE, height: SIZE },
      deviceScaleFactor: 1,
      offline: true,
    });
    const page = await context.newPage();
    for (const name of NAMES) {
      const file = path.join(SRC_DIR, `${name}.svg`);
      const svg = fs.readFileSync(file);
      const src = `data:image/svg+xml;base64,${svg.toString("base64")}`;
      await page.setContent(
        `<!doctype html><html><body style="margin:0;background:transparent">` +
          `<img id="i" src="${src}" width="${SIZE}" height="${SIZE}" ` +
          `style="display:block;object-fit:contain"></body></html>`,
      );
      // 等 <img> 解码完成：SVG 载入失败要报错，不能静默写出一张空图。
      const ok = await page.evaluate(async () => {
        const img = document.getElementById("i");
        try {
          await img.decode();
        } catch {
          return false;
        }
        return img.naturalWidth > 0;
      });
      if (!ok) {
        throw new Error(`SVG 解码失败：${file}`);
      }
      const png = await page.screenshot({
        type: "png",
        omitBackground: true,
        clip: { x: 0, y: 0, width: SIZE, height: SIZE },
      });
      fs.writeFileSync(path.join(out, `${name}.png`), png);
      console.log(`${name}.png  ${png.length} B`);
    }
  } finally {
    await browser.close();
  }
  console.log(`已写入 ${NAMES.length} 个图标到 ${out}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
