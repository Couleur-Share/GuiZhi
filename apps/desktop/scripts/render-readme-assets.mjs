/**
 * 将 README 的 SVG 首图渲染为 GitHub Social Preview 可直接使用的 PNG。
 *
 *   pnpm --filter @guizhi/desktop readme:assets
 */
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "../../..");
const sourcePath = path.join(repoRoot, "docs/images/readme-hero.svg");
const outputPath = path.join(repoRoot, "docs/images/social-preview.png");

async function launchBrowser() {
  const channels =
    process.platform === "win32"
      ? ["msedge", "chrome", undefined]
      : ["chrome", undefined];
  let lastError;

  for (const channel of channels) {
    try {
      return await chromium.launch({
        ...(channel ? { channel } : {}),
        headless: true,
      });
    } catch (error) {
      lastError = error;
    }
  }

  throw lastError;
}

const browser = await launchBrowser();

try {
  const page = await browser.newPage({
    viewport: { width: 1280, height: 640 },
  });
  await page.goto(new URL(`file:///${sourcePath.replaceAll("\\", "/")}`).href);
  // README 中保留透明圆角；Social Preview 用满版暗底，避免卡片四角发白。
  await page.evaluate(() => {
    document.documentElement.style.background = "#090c12";
  });
  await page.screenshot({ path: outputPath });
  console.log(`已生成 ${path.relative(repoRoot, outputPath)}（1280x640）`);
} finally {
  await browser.close();
}
