import { expect, it } from 'vitest';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { WikiCompilerDB } from '@guizhi/db/wiki-compiler';
import { ExactSemanticSearchBackend } from '../../src/main/services/semantic-search-backend';
const percentile = (values: number[]) => [...values].sort((a,b) => a-b)[Math.ceil(values.length * 0.95)-1];
it('固定合成数据：1k/10k/50k 首屏、全文候选与 50k 向量事件循环预算', async () => {
  const results: object[] = [];
  for (const count of [1000, 10000, 50000]) {
    const db = new Database(':memory:');
    try {
      db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
      db.transaction(() => {
        for (let n = 0; n < count; n++) db.run('INSERT INTO knowledge_items(id,title,content,transcript,review_status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)', `fixture-${n}`, `合成资料 ${n}`, `开头事实 ${n}\n\n中间事实 ${n}\n\n结尾事实 ${n}`, n%3 ? null : `独立文字稿 ${n}`, n%10 ? 'clear' : 'needs_review', n, n);
      })();
      const items = new KnowledgeItemDB(db), compiler = new WikiCompilerDB(db);
      const begin = performance.now(); items.list({ scope: 'all', limit: 20 }); const coldList = performance.now()-begin;
      const start = performance.now(); compiler.preview('fixture-model'); const coldCandidates = performance.now()-start;
      const list: number[] = [], candidates: number[] = [];
      for (let i = 0; i < 20; i++) {
        let time = performance.now(); items.list({ scope: 'all', limit: 20 }); list.push(performance.now()-time);
        time = performance.now(); const value = compiler.preview('fixture-model'); candidates.push(performance.now()-time);
        expect(value.entries).toHaveLength(count); expect(value.entries[0]).not.toHaveProperty('content');
      }
      results.push({ count, coldList, coldCandidates, listP95: percentile(list), candidatesP95: percentile(candidates) });
      if (count === 10000) { expect(percentile(list)).toBeLessThanOrEqual(500); expect(percentile(candidates)).toBeLessThanOrEqual(500); }
    } finally { db.close(); }
  }
  const count = 50000, dims = 64, vectors = new Float32Array(count*dims); vectors.fill(1/Math.sqrt(dims));
  const backend = new ExactSemanticSearchBackend({ model: 'fixture', dims, vectors, itemIds: Array.from({ length: count }, (_,n) => `v-${n}`), chunkIndexes: new Array(count).fill(0) });
  const delays: number[] = []; let last = performance.now();
  const timer = setInterval(() => { const now = performance.now(); delays.push(now-last); last = now; }, 1);
  const start = performance.now(); await backend.search(vectors.slice(0,dims), 20); clearInterval(timer);
  const vectorMs = performance.now()-start, eventLoopP95 = percentile(delays);
  results.push({ vectors: count, dims, vectorMs, eventLoopP95 });
  // 默认只写临时目录；显式输出路径用于保存本次验收，避免覆盖机器相关的旧报告。
  const reportFile = process.env.GUIZHI_PERFORMANCE_REPORT || path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-performance-')), 'performance.json');
  fs.mkdirSync(path.dirname(reportFile), { recursive: true });
  console.info('[performance] 报告路径:', reportFile);
  fs.writeFileSync(reportFile, JSON.stringify({ date: new Date().toISOString(), cpu: os.cpus()[0].model, memory: os.totalmem(), runtime: process.version, results }, null, 2));
  expect(eventLoopP95).toBeLessThanOrEqual(100);
}, 180000);
