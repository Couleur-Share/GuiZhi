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
	// 缺失以索引本身为准而不是映射表：映射可能指向一条已经不在的索引行。
	// 注意：item_id 是 FTS5 的 UNINDEXED 列，对它做相关子查询（NOT EXISTS ... WHERE item_id = i.id）会让每个条目都全表扫一遍虚表，
	// 是 O(n²)（实测 1.2 万条 23 s、3 万条 / 750 MB 超过 10 分钟，窗口出现前一直卡住）。
	// 改用 NOT IN 非相关子查询：SQLite 把它物化成一次性的临时索引，整体 O(n log n)；排除 NULL 以免 NOT IN 的空值语义吞掉所有行。
	rows, err := l.db.Query(ctx, `SELECT id, title, content FROM knowledge_items
       WHERE id NOT IN (SELECT item_id FROM knowledge_fts WHERE item_id IS NOT NULL)`)
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
	// 同上：page_id 也是 UNINDEXED 列，用非相关子查询避免逐页全表扫描虚表。
	rows, err := l.db.Query(ctx, `SELECT id, title, summary, body FROM wiki_pages
       WHERE id NOT IN (SELECT page_id FROM wiki_fts WHERE page_id IS NOT NULL)`)
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
