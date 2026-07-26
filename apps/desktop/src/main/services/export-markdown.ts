/**
 * 知识库 Markdown 批量导出。
 *
 * 每个条目导出为一个 .md 文件（YAML frontmatter + 正文），
 * 按集合建子目录，未分类条目放在导出根目录。
 * 条目数据本身不含机密（API Key 等配置绝不进入导出目录）。
 */
import fs from "fs";
import path from "path";
import { extractAllLocalAssetRefs } from "@guizhi/shared/utils/media-refs";
import Database from "../database/sqlite";
import { getImagesDir, getVideosDir } from "../runtime-paths";

/** 导出目录内存放资产副本的子目录名 */
const EXPORT_ASSETS_DIR = "assets";

/** Windows 保留设备名：拿它建目录会抛 EINVAL，把整次导出中断在半路 */
const RESERVED_DEVICE_NAME = /^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i;

interface ExportItemRow {
  id: string;
  title: string;
  content: string;
  summary: string | null;
  transcript: string | null;
  item_type: string;
  status: string;
  collection_id: string | null;
  collection_name: string | null;
  is_favorite: number;
  created_at: number;
  updated_at: number;
}

export interface ExportMarkdownStats {
  count: number;
  /** 一并拷进导出目录的资产文件数 */
  assetCount: number;
}

/** Windows/macOS 通用的文件名清洗；空结果由调用方兜底 */
export function sanitizeFileName(name: string, maxLength = 60): string {
  return name
    .replace(/[\\/:*?"<>|]/g, " ")
    .replace(/[\u0000-\u001f]/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, maxLength)
    .trim()
    .replace(/[. ]+$/, "");
}

/** YAML 标量统一走 JSON 转义（JSON 字符串是合法 YAML） */
function yamlScalar(value: string): string {
  return JSON.stringify(value);
}

function buildFrontmatter(
  row: ExportItemRow,
  tagNames: string[],
  sourceUri: string | null,
): string {
  const lines = ["---"];
  lines.push(`title: ${yamlScalar(row.title || "")}`);
  lines.push(`type: ${row.item_type}`);
  lines.push(`status: ${row.status}`);
  if (tagNames.length > 0) {
    lines.push(`tags: ${JSON.stringify(tagNames)}`);
  }
  if (row.collection_name) {
    lines.push(`collection: ${yamlScalar(row.collection_name)}`);
  }
  if (sourceUri) {
    lines.push(`source: ${yamlScalar(sourceUri)}`);
  }
  if (row.is_favorite === 1) {
    lines.push("favorite: true");
  }
  if (row.summary) {
    lines.push(`summary: ${yamlScalar(row.summary)}`);
  }
  lines.push(`created: ${new Date(row.created_at).toISOString()}`);
  lines.push(`updated: ${new Date(row.updated_at).toISOString()}`);
  lines.push("---");
  return lines.join("\n");
}

function buildDocument(
  row: ExportItemRow,
  tagNames: string[],
  sourceUri: string | null,
  content: string,
): string {
  const parts = [buildFrontmatter(row, tagNames, sourceUri), ""];
  parts.push(content);
  if (row.transcript) {
    parts.push("", "## 转写文本", "", row.transcript);
  }
  return `${parts.join("\n").replace(/\n+$/, "")}\n`;
}

/**
 * 把条目引用到的本地资产拷进导出目录，返回成功拷贝的文件名集合。
 *
 * 不拷的话正文里全是 `local-image://` 死链——这个协议只有归知自己认，
 * 导出的 Markdown 在 Obsidian / Typora 里打开就是满屏破图。
 */
function copyReferencedAssets(
  contents: string[],
  targetDir: string,
): Set<string> {
  const refs = new Set<string>();
  for (const content of contents) {
    for (const ref of extractAllLocalAssetRefs(content)) {
      refs.add(ref);
    }
  }

  const resolved = new Map<string, string>();
  for (const ref of refs) {
    const source = [getImagesDir(), getVideosDir()]
      .map((dir) => path.join(dir, ref))
      .find((candidate) => fs.existsSync(candidate));
    if (source) {
      resolved.set(ref, source);
    }
  }

  const copied = new Set<string>();
  if (resolved.size === 0) {
    return copied;
  }
  const assetsDir = path.join(targetDir, EXPORT_ASSETS_DIR);
  fs.mkdirSync(assetsDir, { recursive: true });
  for (const [ref, source] of resolved) {
    fs.copyFileSync(source, path.join(assetsDir, ref));
    copied.add(ref);
  }
  return copied;
}

/** 把自定义协议引用改写成导出目录内的相对路径；没拷到的资产保持原样 */
export function rewriteAssetLinks(
  content: string,
  copied: Set<string>,
  relativePrefix: string,
): string {
  if (!content || copied.size === 0) {
    return content;
  }
  return content.replace(
    /local-(?:image|video):\/\/([A-Za-z0-9_.-]+)/g,
    (match, name: string) =>
      copied.has(name) ? `${relativePrefix}${name}` : match,
  );
}

function loadTagNamesByItem(db: Database.Database): Map<string, string[]> {
  const rows = db.all(
    `SELECT kit.item_id AS item_id, t.name AS name
     FROM knowledge_item_tags kit
     JOIN tags t ON t.id = kit.tag_id
     ORDER BY t.name`,
  ) as Array<{ item_id: string; name: string }>;
  const result = new Map<string, string[]>();
  for (const row of rows) {
    const list = result.get(row.item_id) ?? [];
    list.push(row.name);
    result.set(row.item_id, list);
  }
  return result;
}

function loadLatestSourceUriByItem(
  db: Database.Database,
): Map<string, string> {
  const rows = db.all(
    `SELECT item_id, source_uri
     FROM source_records
     WHERE source_uri IS NOT NULL
     ORDER BY captured_at ASC`,
  ) as Array<{ item_id: string; source_uri: string }>;
  // 按 captured_at 升序遍历，后写覆盖先写 → 留下最新来源
  const result = new Map<string, string>();
  for (const row of rows) {
    result.set(row.item_id, row.source_uri);
  }
  return result;
}

/** 集合目录名：同名集合冲突时追加 id 前缀区分 */
function resolveCollectionDirs(
  rows: ExportItemRow[],
): Map<string, string> {
  const dirs = new Map<string, string>();
  // Windows / macOS 的文件系统大小写不敏感，「Work」与「work」是同一个目录，
  // 占位表按小写记，否则两个集合会被写进同一处
  const claimed = new Set<string>();
  for (const row of rows) {
    if (!row.collection_id || dirs.has(row.collection_id)) {
      continue;
    }
    let base = sanitizeFileName(row.collection_name ?? "") || "collection";
    if (RESERVED_DEVICE_NAME.test(base)) {
      base = `${base}-dir`;
    }
    const dirName = claimed.has(base.toLowerCase())
      ? `${base}-${row.collection_id.slice(0, 8)}`
      : base;
    claimed.add(dirName.toLowerCase());
    dirs.set(row.collection_id, dirName);
  }
  return dirs;
}

/**
 * 把全部未删除条目导出到 targetDir（目录须已存在）。
 */
export function exportKnowledgeToMarkdown(
  db: Database.Database,
  targetDir: string,
): ExportMarkdownStats {
  const rows = db.all(
    `SELECT i.id, i.title, i.content, i.summary, i.transcript,
            i.item_type, i.status, i.collection_id, i.is_favorite,
            i.created_at, i.updated_at,
            c.name AS collection_name
     FROM knowledge_items i
     LEFT JOIN collections c ON c.id = i.collection_id
     WHERE i.deleted_at IS NULL
     ORDER BY i.created_at ASC`,
  ) as ExportItemRow[];

  const tagNamesByItem = loadTagNamesByItem(db);
  const sourceUriByItem = loadLatestSourceUriByItem(db);
  const collectionDirs = resolveCollectionDirs(rows);
  const copiedAssets = copyReferencedAssets(
    rows.map((row) => row.content ?? ""),
    targetDir,
  );

  let count = 0;
  for (const row of rows) {
    const dirName = row.collection_id
      ? collectionDirs.get(row.collection_id)
      : null;
    const itemDir = dirName ? path.join(targetDir, dirName) : targetDir;
    fs.mkdirSync(itemDir, { recursive: true });

    const baseName = sanitizeFileName(row.title) || "untitled";
    const fileName = `${baseName}-${row.id.slice(0, 8)}.md`;
    const document = buildDocument(
      row,
      tagNamesByItem.get(row.id) ?? [],
      sourceUriByItem.get(row.id) ?? null,
      rewriteAssetLinks(
        row.content ?? "",
        copiedAssets,
        dirName ? `../${EXPORT_ASSETS_DIR}/` : `./${EXPORT_ASSETS_DIR}/`,
      ),
    );
    fs.writeFileSync(path.join(itemDir, fileName), document, "utf8");
    count += 1;
  }

  return { count, assetCount: copiedAssets.size };
}
