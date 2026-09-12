import { expect, it } from 'vitest';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES, SCHEMA_INDEXES } from '@guizhi/db/schema';
import { runMigrations } from '@guizhi/db/migrations';
import { KnowledgeItemDB } from '@guizhi/db/knowledge';
import { AskSessionDB } from '@guizhi/db/ask-session';
import { WikiCompilerDB } from '@guizhi/db/wiki-compiler';
import { saveSourceRevision, listSourceRevisions } from '@guizhi/db/source-revisions';
import { recordKnowledgeBatch, readKnowledgeBatch, freezeKnowledgeSelection } from '@guizhi/db/knowledge-batch-log';

it('新增证据、来源版本、批量结果和分块检查点随整库备份恢复，重复迁移无损', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-heavy-restore-'));
  let db: Database.Database | null = null;
  try {
    db = new Database(path.join(dir, 'knowledge.db')); db.pragma('foreign_keys=ON'); db.exec(SCHEMA_TABLES); runMigrations(db); db.exec(SCHEMA_INDEXES);
    const item = new KnowledgeItemDB(db).create({ title: '人工标题', content: '第一段\n\n第二段' });
    const compiler = new WikiCompilerDB(db), job = compiler.start('model', compiler.preview('model').entries).job!;
    const work = compiler.next(job.id)!; compiler.finish(work, []);
    saveSourceRevision(db, item.id, { title: '新采集标题', content: '待采用正文', reasons: ['文字稿不完整'] });
    const selection = freezeKnowledgeSelection(db, [item.id]);
    recordKnowledgeBatch(db, 'batch', { kind: 'status', status: 'archived' }, [{ id: item.id, ok: false, error: '注入失败' }]);
    new AskSessionDB(db).save({ id: 'answer', title: '旧回答', messagesJson: JSON.stringify([{ id: 'm', question: '当时是什么', answer: '当时的回答', sources: [{ ordinal: 1, evidence: { version: 1, kind: 'item', sourceId: item.id, title: item.title, text: '当时片段', capturedAt: 1, fingerprint: 'old' } }] }]) });
    const snapshot = path.join(dir, 'snapshot.db'); db.exec(`VACUUM INTO '${snapshot.replace(/'/g, "''")}'`); db.close(); db = null;
    fs.copyFileSync(snapshot, path.join(dir, 'restored.db')); db = new Database(path.join(dir, 'restored.db')); db.pragma('foreign_keys=ON');
    expect(runMigrations(db)).toEqual([]); expect(new KnowledgeItemDB(db).get(item.id)?.content).toBe('第一段\n\n第二段');
    const restored = new WikiCompilerDB(db); restored.interrupt(); expect(restored.get(job.id)?.completed).toBe(1); expect(restored.get(job.id)?.status).toBe('interrupted');
    restored.control(job.id, 'running'); expect(restored.next(job.id)?.block.text).toBe('第二段');
    expect(listSourceRevisions(db, item.id)[0].content).toBe('待采用正文');
    expect(readKnowledgeBatch(db, 'batch').results[0].error).toBe('注入失败');
    expect(db.get('SELECT ids_json FROM knowledge_selection_sets WHERE id=?', selection)).toBeTruthy();
    expect(new AskSessionDB(db).get('answer')?.messagesJson).toContain('当时片段');
    expect(db.all('PRAGMA foreign_key_check')).toEqual([]);
  } finally { db?.close(); fs.rmSync(dir, { recursive: true, force: true }); }
});
