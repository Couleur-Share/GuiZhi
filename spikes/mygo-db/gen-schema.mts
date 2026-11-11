// 把 TS 的建表/索引/迁移 SQL 常量原样导出成 Go 源文件，避免手抄出错。
// 用法（仓库根目录）：pnpm dlx tsx spikes/mygo-db/gen-schema.mts
import fs from "node:fs";

const { SCHEMA_TABLES, SCHEMA_INDEXES } = await import("../../packages/db/src/schema.ts");
const { KNOWLEDGE_BATCH_SCHEMA } = await import("../../packages/db/src/knowledge-batch-log.ts");
const { SOURCE_REVISIONS_SCHEMA } = await import("../../packages/db/src/source-revisions.ts");
const { ASK_HISTORY_SCHEMA, ASK_HISTORY_TRIGGERS } = await import("../../packages/db/src/ask-history.ts");
const { WIKI_COMPILER_SCHEMA, WIKI_INVALIDATION_TRIGGERS } = await import("../../packages/db/src/wiki-compiler-schema.ts");
const { ASK_EVIDENCE_SCHEMA } = await import("../../packages/db/src/ask-evidence.ts");
const { THEMED_READING_SCHEMA } = await import("../../packages/db/src/themed-reading-schema.ts");
const { MOBILE_CAPTURE_SCHEMA } = await import("../../packages/db/src/mobile-capture-schema.ts");
const { WEB_SNAPSHOT_SCHEMA, WEB_CAPTURE_SCHEMA } = await import("../../packages/db/src/web-capture-schema.ts");
const { RESEARCH_EVIDENCE_SCHEMA, RESEARCH_DOCUMENT_SCHEMA, RESEARCH_SERIES_SCHEMA } = await import("../../packages/db/src/research-workflow-schema.ts");
const { MIGRATIONS } = await import("../../packages/db/src/migrations.ts");

const consts: Record<string, string> = {
  schemaTables: SCHEMA_TABLES,
  schemaIndexes: SCHEMA_INDEXES,
  knowledgeBatchSchema: KNOWLEDGE_BATCH_SCHEMA,
  sourceRevisionsSchema: SOURCE_REVISIONS_SCHEMA,
  askHistorySchema: ASK_HISTORY_SCHEMA,
  askHistoryTriggers: ASK_HISTORY_TRIGGERS,
  wikiCompilerSchema: WIKI_COMPILER_SCHEMA,
  wikiInvalidationTriggers: WIKI_INVALIDATION_TRIGGERS,
  askEvidenceSchema: ASK_EVIDENCE_SCHEMA,
  themedReadingSchema: THEMED_READING_SCHEMA,
  mobileCaptureSchema: MOBILE_CAPTURE_SCHEMA,
  webSnapshotSchema: WEB_SNAPSHOT_SCHEMA,
  webCaptureSchema: WEB_CAPTURE_SCHEMA,
  researchEvidenceSchema: RESEARCH_EVIDENCE_SCHEMA,
  researchDocumentSchema: RESEARCH_DOCUMENT_SCHEMA,
  researchSeriesSchema: RESEARCH_SERIES_SCHEMA,
};

const goRaw = (s: string) => "`" + s.replace(/\r\n/g, "\n").replace(/`/g, "` + \"`\" + `") + "`";
let out = `package main

// 由 spikes/mygo-db/gen-schema.mts 自动生成，请勿手改：建表/索引/迁移用到的 SQL 常量，原文取自 packages/db/src。
// 重新生成：pnpm dlx tsx spikes/mygo-db/gen-schema.mts

`;
for (const [name, value] of Object.entries(consts)) {
  if (typeof value !== "string") throw new Error(`${name} 不是字符串`);
  out += `const ${name} = ${goRaw(value)}\n\n`;
}
out += `// migrationNames 是 TS 迁移清单的名称与顺序（schema_version = 条数）。\nvar migrationNames = []string{\n${MIGRATIONS.map((m: any) => `\t${JSON.stringify(m.name)},`).join("\n")}\n}\n`;
fs.writeFileSync(new URL("../mygo-shell/schema_gen.go", import.meta.url), out);
console.log(`schema_gen.go: ${Object.keys(consts).length} 个常量，${MIGRATIONS.length} 条迁移`);
