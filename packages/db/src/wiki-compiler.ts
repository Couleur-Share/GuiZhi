import { wikiCandidateRows } from './wiki-candidates';
import { randomUUID } from 'node:crypto';
import type Database from './adapter';
import { wikiBlocks, wikiFingerprint, type WikiMaterial } from './wiki-material';
import { publishWikiPages } from './wiki-publish';
import { WIKI_COMPILER_VERSION, type WikiBlock, type WikiCandidate, type WikiCompileJob, type WikiCompilePreview, type WikiCompileWork, type WikiContributions, type WikiJobStatus } from '@guizhi/shared/types/wiki-compiler';

/** 预览、统计、执行共享同一候选判定；模型调用由单个串行驱动器完成。 */
export class WikiCompilerDB {
  private previewCache = new Map<string, { revision: number; until: number; value: WikiCompilePreview }>();
  constructor(private readonly db: Database.Database) {}
  private material(id: string) { return this.db.get('SELECT id,title,content,transcript,review_status,deleted_at FROM knowledge_items WHERE id=?', id) as WikiMaterial | undefined; }
  /** 大库首次预览分批让出主进程，暖态直接复用同一候选结果。 */
  async previewAsync(model: string, ids?: string[]): Promise<WikiCompilePreview> {
    const revision = (this.db.get('SELECT total_changes() AS n') as { n: number }).n;
    const cached = this.previewCache.get(model);
    if (!ids && cached?.revision === revision && cached.until > Date.now()) return structuredClone(cached.value);
    const targets = ids ?? (this.db.all('SELECT id FROM knowledge_items WHERE deleted_at IS NULL ORDER BY updated_at DESC,id DESC') as { id: string }[]).map(row => row.id);
    const result: WikiCompilePreview = { entries: [], counts: { ready: 0, review: 0, waiting: 0, upgrade: 0, current: 0, excluded: 0 }, requests: { min: 0, max: 0 } };
    for (let offset = 0; offset < targets.length; offset += 100) {
      const part = this.preview(model, targets.slice(offset, offset + 100));
      result.entries.push(...part.entries);
      for (const key of Object.keys(result.counts) as (keyof WikiCompilePreview['counts'])[]) result.counts[key] += part.counts[key];
      result.requests.min += part.requests.min; result.requests.max += part.requests.max;
      await new Promise<void>(resolve => setImmediate(resolve));
    }
    if (!ids && (this.db.get('SELECT total_changes() AS n') as { n: number }).n === revision) {
      this.previewCache.clear(); this.previewCache.set(model, { revision, until: Date.now()+5000, value: result });
    }
    return result;
  }
  preview(model: string, ids?: string[]): WikiCompilePreview {
    const revision = (this.db.get("SELECT total_changes() AS n") as { n: number }).n;
    const cached = this.previewCache.get(model);
    if (!ids && cached?.revision === revision && cached.until > Date.now()) return structuredClone(cached.value);
    const targets = ids ?? (this.db.all('SELECT id FROM knowledge_items WHERE deleted_at IS NULL ORDER BY updated_at DESC,id DESC') as { id: string }[]).map(r => r.id);
    const counts: WikiCompilePreview['counts'] = { ready: 0, review: 0, waiting: 0, upgrade: 0, current: 0, excluded: 0 };
    const entries: WikiCandidate[] = [];
    for (const { item, old, unfinished: active, hashes } of wikiCandidateRows(this.db, targets, model)) {
      const id = item.id, fingerprint = wikiFingerprint(item), blocks = wikiBlocks(item);
      const unfinished = active.has(`${id}:${fingerprint}`);
      const state = item.deleted_at !== null || !blocks.length ? 'excluded' : item.review_status === 'needs_review' ? 'review'
        : !model || unfinished ? 'waiting'
        : old?.prompt_version && old.prompt_version !== WIKI_COMPILER_VERSION ? 'upgrade'
        : old?.content_hash === fingerprint && old.model === model && old.prompt_version === WIKI_COMPILER_VERSION ? 'current'
        : old?.content_hash === fingerprint && (old.failure_count >= 3 || (old.next_attempt_at ?? 0) > Date.now()) ? 'waiting' : 'ready';
      const reusable = blocks.filter(block => hashes.has(block.hash)).length;
      counts[state]++;
      entries.push({ id, title: item.title, fingerprint, state, blocks: blocks.length, reusable,
        reason: state === 'review' ? '需先完成复核' : state === 'waiting' ? '等待退避重试或检查模型配置' : state === 'upgrade' ? '历史覆盖需选择后升级' : undefined });
    }
    const requests = entries.filter(e => e.state === 'ready' || e.state === 'upgrade').reduce((sum, e) => sum + e.blocks - e.reusable, 0);
    const value = { entries, counts, requests: { min: requests, max: requests * 2 } };
    if (!ids) { this.previewCache.clear(); this.previewCache.set(model, { revision, until: Date.now() + 5000, value }); }
    return value;
  }
  start(model: string, selected: { id: string; fingerprint: string }[], allowUpgrade = false): { ok: boolean; job?: WikiCompileJob; preview?: WikiCompilePreview; error?: string } {
    if (!model || !selected.length || selected.length > 1000) return { ok: false, error: '请选择 1–1000 条资料及有效模型' };
    if (this.db.get("SELECT id FROM wiki_compile_jobs WHERE status='running' LIMIT 1")) return { ok: false, error: '已有 Wiki 编译正在执行' };
    const preview = this.preview(model, [...new Set(selected.map(s => s.id))]);
    if (preview.entries.length !== selected.length || preview.entries.some(e => selected.find(s => s.id === e.id)?.fingerprint !== e.fingerprint || !['ready', ...(allowUpgrade ? ['upgrade'] : [])].includes(e.state))) return { ok: false, preview, error: '资料或处理范围已经变化，请核对预览后再开始' };
    const id = randomUUID();
    this.db.transaction(() => {
      this.db.run("INSERT INTO wiki_compile_jobs(id,model,status,created_at) VALUES(?,?,'running',?)", id, model, Date.now());
      for (const entry of preview.entries) {
        this.db.run('INSERT INTO wiki_compile_items(job_id,item_id,title,fingerprint) VALUES(?,?,?,?)', id, entry.id, entry.title, entry.fingerprint);
        for (const block of wikiBlocks(this.material(entry.id)!)) {
          const cached = this.db.get('SELECT contributions_json FROM wiki_block_cache WHERE item_id=? AND block_hash=? AND model=?', entry.id, block.hash, model) as { contributions_json: string } | undefined;
          this.db.run('INSERT INTO wiki_compile_blocks(job_id,item_id,block_key,block_json,status,contributions_json) VALUES(?,?,?,?,?,?)', id, entry.id, block.key, JSON.stringify(block), cached ? 'done' : 'pending', cached?.contributions_json ?? null);
        }
      }
    })();
    return { ok: true, job: this.get(id)! };
  }
  get(id: string): WikiCompileJob | null {
    const job = this.db.get('SELECT * FROM wiki_compile_jobs WHERE id=?', id) as { id: string; model: string; status: WikiJobStatus; error: string | null; created_at: number } | undefined;
    if (!job) return null;
    const counts = this.db.get("SELECT COUNT(*) AS total,SUM(status='done') AS completed,SUM(status='failed') AS failed FROM wiki_compile_blocks WHERE job_id=?", id) as { total: number; completed: number; failed: number };
    return { id, model: job.model, status: job.status, error: job.error ?? undefined, createdAt: job.created_at, total: counts.total, completed: counts.completed ?? 0, failed: counts.failed ?? 0 };
  }
  list(): WikiCompileJob[] { return (this.db.all('SELECT id FROM wiki_compile_jobs ORDER BY created_at DESC LIMIT 100') as { id: string }[]).map(r => this.get(r.id)!); }
  interrupt(): void { this.db.run("UPDATE wiki_compile_jobs SET status='interrupted',error='上次编译被中断，可继续未完成的分块' WHERE status='running'"); this.db.run("UPDATE wiki_compile_blocks SET status='pending' WHERE status='running'"); }
  control(id: string, status: 'paused' | 'cancelled' | 'running'): WikiCompileJob {
    const job = this.get(id); if (!job || job.status === 'completed') throw new Error('任务不存在或已经完成');
    if (status === 'running') {
      if (this.db.get("SELECT id FROM wiki_compile_jobs WHERE status='running' AND id!=?", id)) throw new Error('已有其他编译正在执行');
      this.db.run("UPDATE wiki_compile_blocks SET status='pending',error=NULL WHERE job_id=? AND status IN ('failed','running')", id);
    }
    this.db.run('UPDATE wiki_compile_jobs SET status=?,error=NULL WHERE id=?', status, id); return this.get(id)!;
  }
  next(id: string): WikiCompileWork | null {
    const job = this.get(id); if (!job || job.status !== 'running') return null;
    if (this.db.get("SELECT 1 FROM wiki_compile_blocks WHERE job_id=? AND status='running'", id)) throw new Error('当前分块尚未返回');
    this.publishCompleted(id);
    if (this.get(id)?.status !== 'running') return null;
    const row = this.db.get(`SELECT b.item_id,b.block_json,i.title,i.fingerprint FROM wiki_compile_blocks b JOIN wiki_compile_items i ON i.job_id=b.job_id AND i.item_id=b.item_id
      WHERE b.job_id=? AND b.status='pending' ORDER BY i.rowid,b.rowid LIMIT 1`, id) as { item_id: string; block_json: string; title: string; fingerprint: string } | undefined;
    if (!row) {
      const failed = this.get(id)!.failed;
      this.db.run('UPDATE wiki_compile_jobs SET status=? WHERE id=?', failed ? 'failed' : 'completed', id); return null;
    }
    const block: WikiBlock = JSON.parse(row.block_json), material = this.material(row.item_id);
    if (!material || material.deleted_at != null || material.review_status === 'needs_review' || wikiFingerprint(material) !== row.fingerprint) {
      this.db.run("UPDATE wiki_compile_jobs SET status='failed',error='来源已改变，请重新预览处理范围' WHERE id=?", id); return null;
    }
    this.db.run("UPDATE wiki_compile_blocks SET status='running' WHERE job_id=? AND item_id=? AND block_key=?", id, row.item_id, block.key);
    return { jobId: id, itemId: row.item_id, title: row.title, fingerprint: row.fingerprint, block };
  }
  finish(work: WikiCompileWork, contributions?: WikiContributions, error?: string): void {
    const job = this.get(work.jobId); if (!job || ['cancelled','interrupted','completed'].includes(job.status)) return;
    const row = this.db.get("SELECT status,block_json FROM wiki_compile_blocks WHERE job_id=? AND item_id=? AND block_key=?", work.jobId, work.itemId, work.block.key) as { status: string; block_json: string } | undefined;
    if (row?.status !== 'running') throw new Error('分块回执不匹配');
    const block: WikiBlock = JSON.parse(row.block_json);
    if (contributions && (contributions.length > 4 || contributions.some(p => !p.title || !p.body || p.body.length > 16000 || !['topic','entity','concept'].includes(p.kind)))) throw new Error('分块贡献格式无效');
    this.db.transaction(() => {
      this.db.run('UPDATE wiki_compile_blocks SET status=?,contributions_json=?,error=?,attempts=attempts+1 WHERE job_id=? AND item_id=? AND block_key=?', error ? 'failed' : 'done', error ? null : JSON.stringify(contributions ?? []), error ?? null, work.jobId, work.itemId, block.key);
      if (!error) this.db.run("UPDATE wiki_compile_blocks SET status='done',contributions_json=?,error=NULL WHERE job_id=? AND item_id=? AND json_extract(block_json,'$.hash')=?", JSON.stringify(contributions ?? []), work.jobId, work.itemId, block.hash);
      if (!error && this.material(work.itemId)) this.db.run('INSERT OR REPLACE INTO wiki_block_cache(item_id,block_hash,model,contributions_json) VALUES(?,?,?,?)', work.itemId, block.hash, job.model, JSON.stringify(contributions ?? []));
      if (error) this.db.run(`INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,failure_count,next_attempt_at,updated_at) VALUES(?,?,?,'',1,?,?)
        ON CONFLICT(item_id) DO UPDATE SET content_hash=excluded.content_hash, failure_count=failure_count+1,next_attempt_at=excluded.next_attempt_at`, work.itemId, work.fingerprint, job.model, Date.now() + 30 * 60_000, Date.now());
    })();
  }
  private publishCompleted(id: string): void {
    const job = this.get(id)!;
    for (const row of this.db.all(`SELECT item_id,fingerprint FROM wiki_compile_items i WHERE job_id=? AND published=0
      AND NOT EXISTS(SELECT 1 FROM wiki_compile_blocks b WHERE b.job_id=i.job_id AND b.item_id=i.item_id AND b.status!='done')`, id) as { item_id: string; fingerprint: string }[]) {
      const item = this.material(row.item_id);
      if (!item || item.deleted_at != null || item.review_status === 'needs_review' || wikiFingerprint(item) !== row.fingerprint) { this.db.run("UPDATE wiki_compile_jobs SET status='failed',error='来源已改变，结果尚未发布' WHERE id=?", id); return; }
      this.db.transaction(() => {
        const affected = new Set((this.db.all('SELECT DISTINCT page_title FROM wiki_contributions WHERE item_id=?', item.id) as { page_title: string }[]).map(r => r.page_title));
        for (const r of this.db.all('SELECT p.normalized_title FROM wiki_page_sources s JOIN wiki_pages p ON p.id=s.page_id WHERE s.item_id=?', item.id) as { normalized_title: string }[]) affected.add(r.normalized_title);
        this.db.run('DELETE FROM wiki_contributions WHERE item_id=?', item.id);
        for (const block of this.db.all('SELECT block_key,contributions_json FROM wiki_compile_blocks WHERE job_id=? AND item_id=?', id, item.id) as { block_key: string; contributions_json: string }[]) {
          for (const page of JSON.parse(block.contributions_json) as WikiContributions) {
            affected.add(page.normalizedTitle);
            this.db.run('INSERT OR REPLACE INTO wiki_contributions(item_id,block_key,page_title,contribution_json) VALUES(?,?,?,?)', item.id, block.block_key, page.normalizedTitle, JSON.stringify(page));
          }
        }
        this.db.run(`INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,failure_count,next_attempt_at,updated_at) VALUES(?,?,?,?,0,NULL,?)
          ON CONFLICT(item_id) DO UPDATE SET content_hash=excluded.content_hash,model=excluded.model,prompt_version=excluded.prompt_version,failure_count=0,next_attempt_at=NULL,updated_at=excluded.updated_at`, item.id, row.fingerprint, job.model, WIKI_COMPILER_VERSION, Date.now());
        publishWikiPages(this.db, [...affected], job.model);
        this.db.run('UPDATE wiki_compile_items SET published=1 WHERE job_id=? AND item_id=?', id, item.id);
      })();
    }
  }
}
