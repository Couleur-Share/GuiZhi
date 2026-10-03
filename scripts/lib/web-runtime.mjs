/**
 * 网页采集验收脚本共用：为隔离副本准备网页组件运行时。
 * Shared by web-capture validation scripts: make the web runtime available in the isolated copy.
 *
 * resources/crawl4ai 是被 Git 忽略的构建产物，pnpm shot 的隔离副本里没有；
 * 这里为副本建立指向开发目录的只读联接，不复制、不修改开发目录。
 */
import fs from "node:fs";
import path from "node:path";
import { isValidationSnapshot } from "../desktop-validation-snapshot.mjs";

const REPO = path.resolve(import.meta.dirname, "../..");

export function ensureWebRuntime(mainEntry) {
  const desktopRoot = path.resolve(mainEntry, "../../..");
  const link = path.join(desktopRoot, "resources", "crawl4ai");
  if (fs.existsSync(path.join(link, "manifest.json"))) return;
  if (!isValidationSnapshot(desktopRoot))
    throw new Error("网页组件运行时缺失，且当前不是隔离副本，拒绝自动建立联接");
  const source = path.join(REPO, "apps/desktop/resources/crawl4ai");
  if (!fs.existsSync(path.join(source, "manifest.json")))
    throw new Error(
      `开发目录没有网页组件运行时：请先运行 python scripts/build-crawl4ai.py --target win32-x64（${source}）`,
    );
  fs.mkdirSync(path.dirname(link), { recursive: true });
  fs.symlinkSync(source, link, "junction");
}
