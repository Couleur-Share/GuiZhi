import fs from "node:fs/promises";
import type { Stats } from "node:fs";
import type { ThemedReadingAsset, ThemedReadingVersion } from "@guizhi/shared/types";
import { localThemeAssetPath, readThemeAsset } from "./assets";
import { retainAssetFiles } from "../asset-cleanup";

const MAX_VERIFIED_FILES = 512;
const verified = new Map<string, string>();
const pending = new Map<string, Promise<void>>();

function signature(asset: ThemedReadingAsset, stat: Stats): string {
  return JSON.stringify([asset.sha256, asset.bytes, stat.size, stat.mtimeMs, stat.ctimeMs, stat.ino, stat.dev]);
}

function verifyStat(asset: ThemedReadingAsset, stat: Stats): void {
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error("图片不是可读取的本地文件");
  if (!asset.sha256 || !/^[a-f0-9]{64}$/.test(asset.sha256) || !Number.isSafeInteger(asset.bytes)) throw new Error("图片缺少完整校验信息，请重新生成");
  if (stat.size !== asset.bytes) throw new Error("图片文件长度发生变化，请补图或重新生成");
}

async function verifyAsset(asset: ThemedReadingAsset): Promise<void> {
  const file = localThemeAssetPath(asset.fileName ?? "");
  const before = await fs.lstat(file);
  verifyStat(asset, before);
  const stamp = signature(asset, before);
  if (verified.get(file) === stamp) return;
  const key = `${file}\0${stamp}`;
  const existing = pending.get(key);
  if (existing !== undefined) return existing;
  const check = (async () => {
    await readThemeAsset(asset);
    const after = await fs.lstat(file);
    verifyStat(asset, after);
    if (signature(asset, after) !== stamp) throw new Error("图片在校验期间发生变化，请重试读取");
    verified.delete(file);
    verified.set(file, stamp);
    while (verified.size > MAX_VERIFIED_FILES) verified.delete(verified.keys().next().value!);
  })();
  pending.set(key, check);
  try { await check; }
  finally { if (pending.get(key) === check) pending.delete(key); }
}

function healthError(error: unknown): string {
  if ((error as NodeJS.ErrnoException)?.code === "ENOENT") return "图片文件已丢失，请补图或重新生成";
  return error instanceof Error ? error.message : String(error);
}

/** 仅降级本次读取的副本，不把磁盘暂时缺失写回已保存的版本。 */
export async function inspectThemedReadingImages(version: ThemedReadingVersion): Promise<ThemedReadingVersion> {
  const view = {...version, assets: version.assets.map(asset => ({...asset})), warnings: [...version.warnings]};
  const release = retainAssetFiles(view.assets.flatMap(asset => asset.fileName ? [asset.fileName] : []));
  try {
    // 控制内存占用：大图校验串行，跨读取请求共享相同文件的校验过程。
    for (const asset of view.assets) {
      if (asset.status !== "ready") continue;
      try { await verifyAsset(asset); }
      catch (error) {
        asset.status = "failed";
        asset.error = healthError(error);
        const warning = `${asset.alt || asset.purpose || "页面图片"}：${asset.error}`;
        if (!view.warnings.includes(warning)) view.warnings.push(warning);
      }
    }
    return view;
  } finally { release(); }
}

export async function assertThemedReadingImagesHealthy(version: ThemedReadingVersion, allowKnownIncomplete = false): Promise<void> {
  const view = await inspectThemedReadingImages(version);
  // 已发布的部分成功页本身可读；只阻止原来成功、现在损坏或丢失的素材恢复。
  const previouslyReady = new Set(version.assets.filter(asset => asset.status === "ready").map(asset => asset.id));
  const failures = view.assets.filter(asset => asset.status !== "ready" && (!allowKnownIncomplete || previouslyReady.has(asset.id)));
  if (failures.length) throw new Error(`上一版图片不完整，当前页面保持不变：${failures.map(asset => `${asset.alt || asset.purpose}：${asset.error || "图片尚未完成"}`).join("；")}`);
}

/** 隔离测试使用；缓存键也包含完整数据目录与文件身份，不跨目录信任。 */
export function resetThemedReadingImageHealthCache(): void {
  verified.clear();
  pending.clear();
}
