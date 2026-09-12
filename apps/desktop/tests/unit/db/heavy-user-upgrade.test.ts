import { expect, it } from 'vitest';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES } from '@guizhi/db/schema';
import { MIGRATIONS, runMigrations, getSchemaVersion, SCHEMA_VERSION } from '@guizhi/db/migrations';
import { AskSessionDB } from '@guizhi/db/ask-session';
import { WikiCompilerDB } from '@guizhi/db/wiki-compiler';

it('实施前 schema 的文件备份可重复恢复升级，旧会话与旧 Wiki 保留且不启动历史任务', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'guizhi-before-heavy-'));
  let db: Database.Database | undefined;
  try {
    const legacyPath = path.join(root, 'legacy.db'); db = new Database(legacyPath); db.exec(SCHEMA_TABLES);
    db.exec(`DROP TRIGGER IF EXISTS wiki_source_changed; DROP TRIGGER IF EXISTS wiki_source_deleted;
      DROP TRIGGER IF EXISTS wiki_checkpoint_source_deleted; DROP TRIGGER IF EXISTS ask_history_deleted;
      DROP TABLE wiki_compile_blocks; DROP TABLE wiki_compile_items; DROP TABLE wiki_compile_jobs;
      DROP TABLE wiki_block_cache; DROP TABLE wiki_contributions; DROP TABLE wiki_page_suggestions; DROP TABLE wiki_invalidated_pages;
      DROP TABLE ask_session_fts; DROP TABLE ask_session_meta; DROP TABLE ask_evidence_clearances;
      DROP TABLE source_capture_revisions; DROP TABLE knowledge_selection_sets; DROP TABLE knowledge_batch_results;
      CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,applied_at INTEGER NOT NULL);`);
    const oldMigrations = MIGRATIONS.slice(0, MIGRATIONS.findIndex(m => m.name === '0034-ask-evidence-snapshots'));
    for (const m of oldMigrations) db.run('INSERT INTO schema_migrations VALUES(?,1)', m.name);
    db.exec(`PRAGMA user_version=${oldMigrations.length}`);
    db.run("INSERT INTO knowledge_items(id,title,content,created_at,updated_at) VALUES('old','旧资料','完整历史正文',1,1)");
    db.run("INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,updated_at) VALUES('old','v1','m','legacy',1)");
    db.run("INSERT INTO ask_sessions(id,title,messages_json,created_at,updated_at) VALUES('s','旧问答',?,1,1)", JSON.stringify([{ question: '旧问题', answer: '旧回答的独特事实', sources: [] }]));
    db.close(); db = undefined;
    for (let attempt = 0; attempt < 2; attempt++) {
      const restored = path.join(root, `restored-${attempt}.db`); fs.copyFileSync(legacyPath, restored); db = new Database(restored); db.pragma('foreign_keys=ON');
      expect(runMigrations(db)[0]).toBe('0034-ask-evidence-snapshots'); expect(getSchemaVersion(db)).toBe(SCHEMA_VERSION); expect(runMigrations(db)).toEqual([]);
      expect(new AskSessionDB(db).query({search:'独特事实'}).entries[0].id).toBe('s');
      expect(new WikiCompilerDB(db).preview('m').entries[0].state).toBe('upgrade');
      expect(db.all('SELECT * FROM wiki_compile_jobs')).toEqual([]); expect(db.pragma('foreign_key_check')).toEqual([]);
      db.close(); db = undefined;
    }
  } finally { db?.close(); fs.rmSync(root, {recursive:true,force:true}); }
});
