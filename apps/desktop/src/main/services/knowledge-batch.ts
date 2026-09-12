import { recordKnowledgeBatch } from "@guizhi/db/knowledge-batch-log";
import { logAppError } from "../diagnostic-log";
import { clearAskEvidence } from "@guizhi/db/ask-evidence";
import { cleanupOrphanAssets } from "./asset-cleanup";
import type Database from '../database/sqlite';
import { KnowledgeItemDB } from '@guizhi/db';
import type { KnowledgeBatchAction, KnowledgeBatchResult } from '@guizhi/shared/types/knowledge-batch';
/** 每条独立记账，分批让出主进程；只对调用瞬间冻结的 ID 操作。 */
export async function executeKnowledgeBatch(db: Database.Database, ids: string[], command: KnowledgeBatchAction, runId?: string): Promise<KnowledgeBatchResult[]> {
  const items = new KnowledgeItemDB(db), results: KnowledgeBatchResult[] = [];
  for (const id of [...new Set(ids)]) {
    let assets: string[] = [], preserveReceipt = false;
    try {
      db.transaction(() => {
      const previous = runId ? db.get('SELECT command_json,result_json FROM knowledge_batch_results WHERE run_id=? AND item_id=?', runId, id) as { command_json: string; result_json: string } | undefined : undefined;
      if (previous) {
        if (previous.command_json !== JSON.stringify(command)) { preserveReceipt = true; throw new Error('同一批次不能更换操作，请重新发起'); }
        // IPC 回执丢失时重试同一批次，复用已提交结果，尤其不能重做彻底删除。
        if ((JSON.parse(previous.result_json) as KnowledgeBatchResult).ok) return;
      }
      const item = items.get(id); if (!item) throw new Error('条目已删除');
      if (!['restore','delete'].includes(command.kind) && item.deletedAt != null) throw new Error('条目已在回收站');
      if (command.kind === 'update') items.bulkUpdate([id], command.patch);
      else if (command.kind === 'status') items.setStatus([id], command.status);
      else if (command.kind === 'trash') items.moveToTrash([id]);
      else if (command.kind === 'restore') items.restore([id]);
      else if (command.kind === "delete") {
        if (item.deletedAt == null) throw new Error('只能彻底删除回收站中的条目');
        assets = items.listAssetRefs([id]);
        if (command.clearEvidence) clearAskEvidence(db, [id]);
        items.deleteForever([id]);
      } else throw new Error('批量操作类型无效');
      // 回执写失败时回滚本条修改，不能让界面把已执行条目当成失败重试。
      if (runId) recordKnowledgeBatch(db, runId, command, [{ id, ok: true }]);
      })();
      results.push({ id, ok: true });
    } catch (e) {
      assets = [];
      const result = { id, ok: false, error: e instanceof Error ? e.message : String(e) };
      results.push(result);
      if (runId && !preserveReceipt) try { recordKnowledgeBatch(db, runId, command, [result]); }
      catch (error) { logAppError({ scope: 'knowledge.batch', action: '保存失败回执', message: String(error) }); }
    }
    // 磁盘清理放在提交之后；清理失败不能把已经提交的条目反报为失败。
    if (assets.length) try { cleanupOrphanAssets(items, assets); }
    catch (error) { logAppError({ scope: 'knowledge.batch', action: '清理已删除条目资产', message: String(error) }); }
    if (results.length % 50 === 0) await new Promise(resolve => setImmediate(resolve));
  }
  return results;
}
