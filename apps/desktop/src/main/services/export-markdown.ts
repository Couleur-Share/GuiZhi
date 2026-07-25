/**
 * 知识库 Markdown 批量导出。
 *
 * 每个条目导出为一个 .md 文件（YAML frontmatter + 正文），
 * 按集合建子目录，未分类条目放在导出根目录。
 * 条目数据本身不含机密（API Key 等配置绝不进入导出目录）。
 */
import fs from "fs";
import path from "path";
import Database from "../database/sqlite";

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
): string {
  const parts = [buildFrontmatter(row, tagNames, sourceUri), ""];
  parts.push(row.content ?? "");
  if (row.transcript) {
    parts.push("", "## 转写文本", "", row.transcript);
  }
  return `${parts.join("\n").replace(/\n+$/, "")}\n`;
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
  const claimed = new Set<string>();
  for (const row of rows) {
    if (!row.collection_id || dirs.has(row.collection_id)) {
      continue;
    }
    const base = sanitizeFileName(row.collection_name ?? "") || "collection";
    const dirName = claimed.has(base)
      ? `${base}-${row.collection_id.slice(0, 4)}`
      : base;
    claimed.add(dirName);
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
    );
    fs.writeFileSync(path.join(itemDir, fileName), document, "utf8");
    count += 1;
  }

  return { count };
}
