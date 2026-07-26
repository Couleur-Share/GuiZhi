/**
 * 主进程阻塞时长实测：把知识库灌到不同规模，量各类操作占用主线程多久。
 * 用途是给「SQLite 要不要移出主进程」这个决策提供数字，不参与构建。
 *
 *   pnpm exec tsx scripts/bench-sqlite-block.ts
 */
import fs from "fs";
import os from "os";
import path from "path";
import DatabaseAdapter from "../packages/db/src/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "../packages/db/src/schema";
import { KnowledgeItemDB } from "../packages/db/src/knowledge";
import { SemanticIndexDB, vectorToBlob } from "../packages/db/src/semantic";
import { searchSemanticByVector } from "../apps/desktop/src/main/services/semantic";

const SCALES = [1_000, 5_000, 20_000];
const BODY_CHARS = 2_000;
const EMBEDDING_DIM = 1024;
const CHUNKS_PER_ITEM = 3;

function makeBody(seed: number): string {
  const words = [
    "架构",
    "检索",
    "嵌入",
    "主进程",
    "渲染",
    "索引",
    "备份",
    "迁移",
    "编译",
    "缓存",
  ];
  let text = "";
  let index = seed;
  while (text.length < BODY_CHARS) {
    index = (index * 1103515245 + 12345) & 0x7fffffff;
    text += `${words[index % words.length]}${index % 997} `;
  }
  return text.slice(0, BODY_CHARS);
}

function makeVector(seed: number): Float32Array {
  const vector = new Float32Array(EMBEDDING_DIM);
  let state = seed || 1;
  let norm = 0;
  for (let at = 0; at < EMBEDDING_DIM; at++) {
    state = (state * 1103515245 + 12345) & 0x7fffffff;
    const value = (state / 0x7fffffff) * 2 - 1;
    vector[at] = value;
    norm += value * value;
  }
  norm = Math.sqrt(norm) || 1;
  for (let at = 0; at < EMBEDDING_DIM; at++) {
    vector[at] /= norm;
  }
  return vector;
}

function time(label: string, run: () => void): number {
  const start = performance.now();
  run();
  const elapsed = performance.now() - start;
  console.log(`    ${label.padEnd(34)} ${elapsed.toFixed(1)} ms`);
  return elapsed;
}

async function timeAsync(
  label: string,
  run: () => Promise<unknown>,
): Promise<number> {
  const start = performance.now();
  await run();
  const elapsed = performance.now() - start;
  console.log(`    ${label.padEnd(34)} ${elapsed.toFixed(1)} ms`);
  return elapsed;
}

function seed(db: DatabaseAdapter.Database, count: number): void {
  const now = Date.now();
  db.exec("BEGIN");
  for (let index = 0; index < count; index++) {
    const id = `item-${index}`;
    db.run(
      `INSERT INTO knowledge_items
         (id, title, content, item_type, status, is_favorite, is_pinned,
          created_at, updated_at)
       VALUES (?, ?, ?, 'note', 'active', 0, ?, ?, ?)`,
      id,
      `条目标题 ${index} 架构检索`,
      makeBody(index + 1),
      index % 50 === 0 ? 1 : 0,
      now - index * 1000,
      now - index * 1000,
    );
  }
  db.exec("COMMIT");
}

function seedEmbeddings(db: DatabaseAdapter.Database, count: number): void {
  const now = Date.now();
  db.exec("BEGIN");
  for (let index = 0; index < count; index++) {
    for (let chunk = 0; chunk < CHUNKS_PER_ITEM; chunk++) {
      const vector = makeVector(index * CHUNKS_PER_ITEM + chunk + 1);
      db.run(
        `INSERT INTO knowledge_embeddings
           (item_id, chunk_index, chunk_text, content_hash, model, dims,
            vector, updated_at)
         VALUES (?, ?, ?, ?, 'bench', ?, ?, ?)`,
        `item-${index}`,
        chunk,
        makeBody(index + chunk).slice(0, 400),
        `hash-${index}-${chunk}`,
        EMBEDDING_DIM,
        vectorToBlob(vector),
        now,
      );
    }
  }
  db.exec("COMMIT");
}

async function main(): Promise<void> {
  const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-bench-"));
  console.log(`工作目录 ${workDir}\n`);

  for (const scale of SCALES) {
    const dbPath = path.join(workDir, `bench-${scale}.db`);
    const db = new DatabaseAdapter(dbPath);
    db.pragma("foreign_keys = ON");
    db.exec(SCHEMA_TABLES);
    db.exec(SCHEMA_INDEXES);

    console.log(`── ${scale} 条条目（正文 ${BODY_CHARS} 字）`);
    time("灌库 + FTS 建索引", () => seed(db, scale));
    time(`灌入 ${scale * CHUNKS_PER_ITEM} 个向量块`, () =>
      seedEmbeddings(db, scale),
    );

    const size = fs.statSync(dbPath).size / 1024 / 1024;
    console.log(`    库体积 ${size.toFixed(1)} MB`);

    const items = new KnowledgeItemDB(db);
    const semantic = new SemanticIndexDB(db);

    // initDatabase 会在统计信息缺失或过期时 ANALYZE，这里复现同样的前提
    time("ANALYZE（初始化时执行）", () => db.exec("ANALYZE"));

    time("列表首屏（50 条 + 标签）", () => {
      items.list({ scope: "all", page: 1, pageSize: 50 });
    });
    time("列表翻到第 20 页", () => {
      items.list({ scope: "all", page: 20, pageSize: 50 });
    });
    time("收藏夹首屏", () => {
      items.list({ scope: "favorites", page: 1, pageSize: 50 });
    });
    time("回收站首屏", () => {
      items.list({ scope: "trash", page: 1, pageSize: 50 });
    });
    time("全文搜索（FTS + 分页）", () => {
      items.list({ scope: "all", search: "架构", page: 1, pageSize: 50 });
    });
    time("单条读取", () => {
      items.get("item-0");
    });
    time("单条保存", () => {
      items.update("item-0", { title: `改过的标题 ${Date.now()}` });
    });

    const query = makeVector(42);
    // 主进程实现分批取用并在批间让出事件循环，测的是总耗时
    const searchMs = await timeAsync("语义检索（全量余弦 top-10）", () =>
      searchSemanticByVector(db, "bench", query, 10),
    );
    // 单批阻塞时长：一次连续占用主线程多久（批间会让出事件循环）
    time("　└ 首批向量加载（1000 块）", () => {
      semantic.loadVectorsForSearch("bench", 1000, 0);
    });
    // 末批应与首批同量级；若退回 LIMIT/OFFSET 翻页，这里会随规模劣化
    const total = scale * CHUNKS_PER_ITEM;
    time("　└ 末批向量加载（1000 块）", () => {
      semantic.loadVectorsForSearch("bench", 1000, total - 1000);
    });

    const backupPath = path.join(workDir, `backup-${scale}.db`);
    time("备份 VACUUM INTO", () => {
      db.exec(`VACUUM INTO '${backupPath.replace(/'/g, "''")}'`);
    });
    time("启动完整性检查", () => {
      db.pragma("integrity_check");
    });

    // 语义检索是唯一随规模线性膨胀且量级最大的一项，单独标注
    if (searchMs > 100) {
      console.log(`    ⚠ 语义检索已超过 100ms，界面会有可感知卡顿`);
    }
    console.log("");
    db.close();
  }

  fs.rmSync(workDir, { recursive: true, force: true });
}

void main();
