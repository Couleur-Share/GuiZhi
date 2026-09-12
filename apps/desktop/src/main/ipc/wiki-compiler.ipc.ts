import { ipcMain } from 'electron';
import type Database from '../database/sqlite';
import { IPC_CHANNELS } from '@guizhi/shared/constants';
import { WikiCompilerDB } from '@guizhi/db/wiki-compiler';
import { acceptWikiSuggestion, flushWikiInvalidations, wikiSuggestionToken } from '@guizhi/db/wiki-publish';
import type { WikiCompilerCommand } from '@guizhi/shared/types/wiki-compiler';
export function registerWikiCompilerIPC(db: Database.Database): void {
  const compiler = new WikiCompilerDB(db); compiler.interrupt();
  ipcMain.handle(IPC_CHANNELS.WIKI_COMPILER, async (_event, input: WikiCompilerCommand) => {
    try {
      switch (input.action) {
        case 'preview': return { ok: true, preview: await compiler.previewAsync(input.model, input.ids) };
        case 'start': return compiler.start(input.model, input.selected, input.allowUpgrade === true);
        case 'next': return { ok: true, work: compiler.next(input.id), job: compiler.get(input.id) };
        case 'finish': compiler.finish(input.work, input.contributions, input.error); return { ok: true };
        case 'control': return { ok: true, job: compiler.control(input.id, input.status) };
        case 'jobs': return { ok: true, jobs: compiler.list() };
        case 'suggestion': {
          if (input.accept) return { ok: Boolean(input.token) && acceptWikiSuggestion(db, input.pageId, input.token), error: "更新建议或页面已变化，请重新核对" };
          flushWikiInvalidations(db);
          const row = db.get('SELECT draft_json,reason FROM wiki_page_suggestions WHERE page_id=?', input.pageId) as { draft_json: string; reason: string } | undefined;
          return { ok: true, suggestion: row ? { ...JSON.parse(row.draft_json), reason: row.reason, token: wikiSuggestionToken(db, input.pageId, row.draft_json) } : null };
        }
        default: throw new Error('编译命令无效');
      }
    } catch (e) { return { ok: false, error: e instanceof Error ? e.message : String(e) }; }
  });
}
