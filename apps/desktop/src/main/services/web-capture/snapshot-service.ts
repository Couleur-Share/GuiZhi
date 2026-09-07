import { dialog } from "electron";
import fs from "node:fs/promises";
import path from "node:path";
import { randomUUID, createHash } from "node:crypto";
import { KnowledgeItemDB, WebSourceDB, webContentHash } from "@guizhi/db";
import type Database from "../../database/sqlite";
import type { WebSnapshotView } from "@guizhi/shared/types";
import { getImagesDir } from "../../runtime-paths";
import { sanitizeSnapshot } from "./snapshot-sanitize";
import { snapshotDocument, escapeSnapshotText } from "./snapshot-document";
import { isWechatUrl } from "./wechat";

export function snapshotSource(db: Database.Database, itemId: string): string {
  const item = new KnowledgeItemDB(db).get(itemId);
  if (!item || item.deletedAt) throw new Error("条目不存在或已删除");
  const source = db.get(
    "SELECT source_uri,access_uri FROM source_records WHERE item_id=? ORDER BY captured_at DESC LIMIT 1",
    itemId,
  ) as { source_uri: string; access_uri?: string };
  const url = source?.access_uri || source?.source_uri;
  if (!isWechatUrl(url)) throw new Error("此条目不是微信公众号文章");
  return url;
}
export async function readSnapshot(
  db: Database.Database,
  itemId: string,
  versionId?: string,
): Promise<WebSnapshotView> {
  const item = new KnowledgeItemDB(db).get(itemId);
  if (!item) throw new Error("条目不存在");
  const sources = new WebSourceDB(db),
    baseline = sources.baseline(itemId),
    versions = sources.versions(itemId);
  const version = versionId
    ? versions.find((v) => v.id === versionId)
    : versions.find((v) => v.id === baseline?.version_id && v.snapshot) ||
      versions.find((v) => v.snapshot);
  if (versionId && !version) throw new Error("原文版本不存在");
  const edited =
    !!baseline && webContentHash(item.content) !== baseline.content_hash;
  if (!version?.snapshot) return { version: null, edited, pending: false };
  try {
    version.snapshot = sanitizeSnapshot(version.snapshot);
    for (const asset of version.snapshot.assets) {
      const data = await fs.readFile(path.join(getImagesDir(), asset.fileName));
      if (createHash("sha256").update(data).digest("hex") !== asset.sha256)
        throw new Error("原文图片校验失败，请重新补采");
    }
    const instanceId = randomUUID();
    return {
      version,
      edited,
      pending: versions.some(
        (v) =>
          v.snapshot &&
          v.id !== baseline?.version_id &&
          v.capturedAt > version.capturedAt,
      ),
      instanceId,
      document: snapshotDocument(version.snapshot, instanceId),
    };
  } catch (error) {
    return {
      version: null,
      edited,
      pending: false,
      error: error instanceof Error ? error.message : "原文快照损坏",
    };
  }
}
export async function exportSnapshot(
  db: Database.Database,
  itemId: string,
  versionId: string,
) {
  const view = await readSnapshot(db, itemId, versionId);
  if (!view.version?.snapshot)
    throw new Error(view.error || "此版本没有原文排版");
  const chosen = await dialog.showOpenDialog({
    title: "选择 HTML 导出目录",
    properties: ["openDirectory", "createDirectory"],
  });
  if (chosen.canceled) return { canceled: true };
  const parent = chosen.filePaths[0],
    name = `公众号原文-${Date.now()}-${randomUUID().slice(0, 6)}`;
  const stage = path.join(parent, `.${name}.tmp`),
    target = path.join(parent, name),
    snapshot = view.version.snapshot;
  await fs.mkdir(path.join(stage, "assets"), { recursive: true });
  try {
    for (const asset of snapshot.assets)
      await fs.copyFile(
        path.join(getImagesDir(), asset.fileName),
        path.join(stage, "assets", asset.fileName),
      );
    let document = snapshotDocument(snapshot);
    for (const asset of snapshot.assets)
      document = document.replaceAll(
        `local-image://${asset.fileName}`,
        `assets/${asset.fileName}`,
      );
    const sourceUrl = isWechatUrl(view.version.sourceUrl)
      ? view.version.sourceUrl
      : "https://mp.weixin.qq.com/";
    const metadata = `<header><h1>${escapeSnapshotText(view.version.title)}</h1><p>${escapeSnapshotText(snapshot.account)} ${escapeSnapshotText(snapshot.author)}</p><p>采集于 ${new Date(view.version.capturedAt).toISOString()}</p><p><a href="${escapeSnapshotText(sourceUrl)}" rel="noreferrer">打开原文</a></p></header>`;
    document = document.replace("<body>", `<body>${metadata}`);
    await fs.writeFile(path.join(stage, "index.html"), document, "utf8");
    // Windows 扫描器可能短暂占用刚写完的目录；有限重试，不退回非原子复制。
    for (let attempt = 0; ; attempt++) {
      try { await fs.rename(stage, target); break; }
      catch (error) {
        const code = (error as NodeJS.ErrnoException).code;
        if (process.platform !== "win32" || !["EPERM", "EBUSY", "EACCES"].includes(code) || attempt >= 4) throw error;
        await new Promise(resolve => setTimeout(resolve, 100 * 2 ** attempt));
      }
    }
    return {
      path: target,
      incomplete: snapshot.failures.length > 0 || snapshot.warnings.length > 0,
    };
  } catch (error) {
    await fs.rm(stage, { recursive: true, force: true });
    throw error;
  }
}
