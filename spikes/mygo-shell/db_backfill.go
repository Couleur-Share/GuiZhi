package main

// 启动时的全文索引补齐，对应 apps/desktop/src/main/database/index.ts 在 initDatabase 之后做的两步：
// KnowledgeItemDB.backfillMissingFtsRows 与 WikiDB.backfillMissingFtsRows。

import (
	"context"
	"log"

	"github.com/egoist/mygo/plugins/sqlite"
)

// backfillMissingKnowledgeFts 补齐缺失的条目 FTS 行，返回补写条数。
// v0.4.1 之前软删会把条目移出索引，那些库里的回收站条目没有索引行；全量重建对大库太贵，只补缺失的。
func (l *Library) backfillMissingKnowledgeFts(ctx context.Context) (int, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	// 先把 rowid 映射补齐：映射表是后加的，老库里已有的索引行一条都没登记，不补的话写路径会一直走「按 item_id 全表扫」
	if _, err := l.db.Execute(ctx, "INSERT OR IGNORE INTO knowledge_fts_map (item_id, fts_rowid) SELECT item_id, rowid FROM knowledge_fts"); err != nil {
		return 0, err
	}
	// 缺失以索引本身为准而不是映射表：映射可能指向一条已经不在的索引行
	rows, err := l.db.Query(ctx, `SELECT id, title, content FROM knowledge_items i
       WHERE NOT EXISTS (SELECT 1 FROM knowledge_fts WHERE item_id = i.id)`)
	if err != nil {
		return 0, err
	}
	if len(rows.Values) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(rows.Values))
	for _, r := range rows.Values {
		ids = append(ids, str(r[0]))
	}
	tagsByItem, err := l.loadTagsFor(ctx, ids)
	if err != nil {
		return 0, err
	}
	var stmts []sqlite.Statement
	for _, r := range rows.Values {
		id := str(r[0])
		var names []string
		for _, t := range tagsByItem[id] {
			names = append(names, str(t["name"]))
		}
		s, err := l.writeFtsStmts(ctx, id, str(r[1]), str(r[2]), names)
		if err != nil {
			return 0, err
		}
		stmts = append(stmts, s...)
	}
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return 0, err
	}
	return len(rows.Values), nil
}

// backfillMissingWikiFts 补齐缺失的 Wiki 页面 FTS 行（本表是 v0.8.0 新加的，老库里一条都没有）。
func (l *Library) backfillMissingWikiFts(ctx context.Context) (int, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	rows, err := l.db.Query(ctx, `SELECT id, title, summary, body FROM wiki_pages p
       WHERE NOT EXISTS (SELECT 1 FROM wiki_fts WHERE page_id = p.id)`)
	if err != nil {
		return 0, err
	}
	if len(rows.Values) == 0 {
		return 0, nil
	}
	var stmts []sqlite.Statement
	for _, r := range rows.Values {
		stmts = append(stmts, wikiFtsStmts(str(r[0]), str(r[1]), str(r[2]), str(r[3]))...)
	}
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return 0, err
	}
	return len(rows.Values), nil
}

// backfillMissingFts 启动时调用；失败只记日志（索引缺失不影响数据本身，下次启动还会再补）。
func (l *Library) backfillMissingFts(ctx context.Context) {
	if n, err := l.backfillMissingKnowledgeFts(ctx); err != nil {
		logAppErrorf("db", "补齐条目全文索引", "%v", err)
	} else if n > 0 {
		log.Printf("[db] 补齐 %d 条缺失的全文索引", n)
	}
	if n, err := l.backfillMissingWikiFts(ctx); err != nil {
		logAppErrorf("db", "补齐 Wiki 全文索引", "%v", err)
	} else if n > 0 {
		log.Printf("[db] 补齐 %d 个 Wiki 页面的全文索引", n)
	}
}
