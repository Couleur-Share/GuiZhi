package main

// 对应 ipc/wiki.ipc.ts、packages/db/src/wiki.ts 与 wiki-publish.ts 里 flushWikiInvalidations：
// Wiki 目录、详情、搜索、图谱、状态、手动编辑、删除与版本回滚（wiki:compiler 另行迁移）。
//
// 事务说明：TS 的 db.transaction 里夹着读，Go 的 Transaction 是一批固定语句。
// 凡是事务里「读到的是本事务稍早写入的数据」的地方，这里用内存状态模拟（见 flushWikiInvalidations）。

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

const (
	wikiRevisionKeep   = 10
	wikiCompilerVer    = "fulltext-v2"
	wikiCatalogColumns = "id, title, normalized_title, kind, summary, aliases_json, manual_edited_at, updated_at"
)

// wikiBind 复刻 node-sqlite3-wasm 单参数绑定：undefined 当作未绑定（NULL），整数用 int64，非整数用 double。
func wikiBind(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) {
			return int64(x), nil
		}
		return x, nil
	case bool:
		if x {
			return int64(1), nil
		}
		return int64(0), nil
	case jsNullT, jsUndefinedT, nil:
		return nil, nil
	case jsArr:
		// 适配器把单个数组参数当作位置参数列表：空数组等于未绑定，一个元素按元素绑定，多于一个会越界
		switch len(x) {
		case 0:
			return nil, nil
		case 1:
			return wikiBind(x[0])
		}
		return nil, errors.New("RangeError: 绑定参数多于占位符")
	}
	return nil, errors.New("不支持的绑定参数类型")
}

func rowStrOrNil(v any) any {
	if s, ok := v.(string); ok {
		return s
	}
	return nil
}

func rowIntOrNil(v any) any {
	if v == nil {
		return nil
	}
	return asInt(v)
}

func catalogEntry(r []any) map[string]any {
	return map[string]any{
		"id": r[0], "title": r[1], "normalizedTitle": r[2], "kind": r[3], "summary": r[4],
		"aliasesJson": rowStrOrNil(r[5]), "manualEditedAt": rowIntOrNil(r[6]), "updatedAt": asInt(r[7]),
	}
}

func revisionEntry(r []any) map[string]any {
	return map[string]any{
		"id": r[0], "pageId": r[1], "title": r[2], "kind": r[3], "summary": r[4], "body": r[5],
		"aliasesJson": rowStrOrNil(r[6]), "model": r[7], "promptVersion": r[8], "createdAt": asInt(r[9]),
	}
}

// ftsStmts 对应 writeFts：先删后插。
func wikiFtsStmts(pageID, title, summary, body string) []sqlite.Statement {
	return []sqlite.Statement{
		{SQL: "DELETE FROM wiki_fts WHERE page_id = ?", Args: []any{pageID}},
		{SQL: "INSERT INTO wiki_fts (page_id, title, summary, body) VALUES (?, ?, ?, ?)",
			Args: []any{pageID, SegmentTextForFts(title), SegmentTextForFts(summary), SegmentTextForFts(body)}},
	}
}

// wikiSaveRevisionStmts 对应 saveRevision：覆盖前留快照，每页只保留最近 10 份。
func wikiSaveRevisionStmts(pageID string, now int64) []sqlite.Statement {
	return []sqlite.Statement{
		{SQL: `INSERT INTO wiki_page_revisions
         (id, page_id, title, kind, summary, body, aliases_json, model, prompt_version, created_at)
       SELECT ?, id, title, kind, summary, body, aliases_json, model, prompt_version, ? FROM wiki_pages WHERE id = ?`,
			Args: []any{newUUID(), now, pageID}},
		{SQL: `DELETE FROM wiki_page_revisions
       WHERE page_id = ? AND id NOT IN (
         SELECT id FROM wiki_page_revisions
         WHERE page_id = ? ORDER BY created_at DESC LIMIT ?
       )`, Args: []any{pageID, pageID, int64(wikiRevisionKeep)}},
	}
}

// flushWikiInvalidations 对应 wiki-publish.ts：来源条目变化后，按仍有效的分块贡献重建受影响的页面。
func (l *Library) flushWikiInvalidations(ctx context.Context) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	inv, err := l.db.Query(ctx, "SELECT page_title FROM wiki_invalidated_pages")
	if err != nil {
		return err
	}
	if len(inv.Values) == 0 {
		return nil
	}
	var titles []string
	for _, r := range inv.Values {
		titles = append(titles, r[0].(string))
	}
	stmts, err := l.publishWikiPages(ctx, titles, "来源更新")
	if err != nil {
		return err
	}
	stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM wiki_invalidated_pages"})
	_, err = l.db.Transaction(ctx, stmts)
	return err
}

func (l *Library) publishWikiPages(ctx context.Context, titles []string, model string) ([]sqlite.Statement, error) {
	pageRows, err := l.db.Query(ctx, "SELECT id, normalized_title FROM wiki_pages")
	if err != nil {
		return nil, err
	}
	pageByTitle := map[string]string{} // 随处理进度加入本批新建的页面
	for _, r := range pageRows.Values {
		pageByTitle[r[1].(string)] = r[0].(string)
	}
	var stmts []sqlite.Statement
	forward := []struct {
		id      string
		targets []string
	}{}
	for _, title := range uniqueStrings(titles) {
		stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM wiki_invalidated_pages WHERE page_title=?", Args: []any{title}})
		cRows, err := l.db.Query(ctx, `SELECT c.item_id,c.block_key,c.contribution_json,i.title AS source_title FROM wiki_contributions c
      JOIN knowledge_items i ON i.id=c.item_id WHERE c.page_title=? AND i.deleted_at IS NULL
      ORDER BY c.item_id,c.block_key`, title)
		if err != nil {
			return nil, err
		}
		eRows, err := l.db.Query(ctx, "SELECT id,manual_edited_at,prompt_version,body FROM wiki_pages WHERE normalized_title=?", title)
		if err != nil {
			return nil, err
		}
		exists := len(eRows.Values) > 0
		if !exists && len(cRows.Values) == 0 {
			continue
		}
		var drafts []any
		var itemIDs, sourceTitles []string
		for _, c := range cRows.Values {
			d, err := jsParse(c[2].(string))
			if err != nil {
				return nil, errors.New("Unexpected token in JSON: " + err.Error())
			}
			drafts = append(drafts, d)
			itemIDs = append(itemIDs, c[0].(string))
			sourceTitles = append(sourceTitles, c[3].(string))
		}
		var first any = jsUndefined
		if len(drafts) > 0 {
			first = drafts[0]
		}
		orDefault := func(v any, def any) any { // `v ?? def`
			if isNullish(v) {
				return def
			}
			return v
		}
		dTitle := orDefault(jsOptGet(first, "title"), title)
		dKind := orDefault(jsOptGet(first, "kind"), "topic")
		dSummary := orDefault(jsOptGet(first, "summary"), "自动来源已移除或等待复核")
		dAliases := orDefault(jsOptGet(first, "aliasesJson"), jsNull)
		var dBody string
		if len(drafts) > 0 {
			parts := make([]string, len(drafts))
			for i, d := range drafts {
				parts[i] = jsToString(jsGet(d, "body")) + "\n\n> 来源：《" + sourceTitles[i] + "》"
			}
			dBody = strings.Join(parts, "\n\n---\n\n")
		} else {
			dBody = "自动来源已移除或等待复核，目前没有可发布的知识贡献。"
		}
		var targets []string
		var targetVals jsArr
		for _, d := range drafts {
			lt := jsGet(d, "linkTargets")
			if arr, ok := lt.(jsArr); ok {
				for _, e := range arr {
					if s, ok := e.(string); ok {
						targets = append(targets, s)
					}
				}
			}
		}
		targets = uniqueStrings(targets)
		for _, t := range targets {
			targetVals = append(targetVals, t)
		}

		id := newUUID()
		var manual, prompt any
		if exists {
			r := eRows.Values[0]
			id, _ = r[0].(string)
			manual, prompt = r[1], r[2]
		}
		legacy := false
		if exists && prompt != wikiCompilerVer {
			lr, err := l.db.Query(ctx, `SELECT 1 FROM wiki_page_sources s
      LEFT JOIN wiki_ingestions w ON w.item_id=s.item_id WHERE s.page_id=? AND COALESCE(w.prompt_version,'')!=? LIMIT 1`, id, wikiCompilerVer)
			if err != nil {
				return nil, err
			}
			legacy = len(lr.Values) > 0
		}
		bind := func(v any) any { b, _ := wikiBind(v); return b }
		now := nowMs()
		if (exists && manual != nil) || legacy {
			draft := newObj()
			draft.set("title", dTitle)
			draft.set("normalizedTitle", title)
			draft.set("kind", dKind)
			draft.set("summary", dSummary)
			draft.set("aliasesJson", dAliases)
			draft.set("body", dBody)
			draft.set("linkTargets", targetVals)
			ids := jsArr{}
			for _, s := range uniqueStrings(itemIDs) {
				ids = append(ids, s)
			}
			payload := newObj()
			payload.set("draft", draft)
			payload.set("model", model)
			payload.set("sourceIds", ids)
			draftJSON, _ := jsStringify(payload)
			reason := "人工编辑内容保持不变，需审阅更新建议"
			if legacy {
				reason = "旧页面仍有未升级来源，保留原内容，需审阅更新建议"
			}
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR REPLACE INTO wiki_page_suggestions(page_id,draft_json,reason,created_at) VALUES(?,?,?,?)",
				Args: []any{id, draftJSON, reason, now}})
			continue
		}
		// savePage
		if exists {
			stmts = append(stmts,
				sqlite.Statement{SQL: `INSERT INTO wiki_page_revisions(id,page_id,title,kind,summary,body,aliases_json,model,prompt_version,created_at)
      SELECT ?,id,title,kind,summary,body,aliases_json,model,prompt_version,? FROM wiki_pages WHERE id=?`, Args: []any{newUUID(), now, id}},
				sqlite.Statement{SQL: "DELETE FROM wiki_page_revisions WHERE page_id=? AND id NOT IN (SELECT id FROM wiki_page_revisions WHERE page_id=? ORDER BY created_at DESC,rowid DESC LIMIT 10)", Args: []any{id, id}},
				sqlite.Statement{SQL: "UPDATE wiki_pages SET title=?,kind=?,summary=?,body=?,aliases_json=?,model=?,prompt_version=?,generated_at=?,updated_at=? WHERE id=?",
					Args: []any{bind(dTitle), bind(dKind), bind(dSummary), dBody, bind(dAliases), model, wikiCompilerVer, now, now, id}})
		} else {
			stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO wiki_pages(id,title,normalized_title,kind,summary,body,aliases_json,provider,model,prompt_version,generated_at,created_at,updated_at)
      VALUES(?,?,?,?,?,?,?,'guizhi',?,?,?,?,?)`,
				Args: []any{id, bind(dTitle), title, bind(dKind), bind(dSummary), dBody, bind(dAliases), model, wikiCompilerVer, now, now, now}})
			pageByTitle[title] = id
		}
		stmts = append(stmts, wikiFtsStmts(id, jsToString(dTitle), jsToString(dSummary), dBody)...)
		stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM wiki_page_links WHERE from_page_id=?", Args: []any{id}})
		for _, t := range targets {
			if tid, ok := pageByTitle[t]; ok && tid != id {
				stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR IGNORE INTO wiki_page_links(from_page_id,to_page_id,created_at) VALUES(?,?,?)", Args: []any{id, tid, now}})
			}
		}
		forward = append(forward, struct {
			id      string
			targets []string
		}{id, targets})
		stmts = append(stmts,
			sqlite.Statement{SQL: "DELETE FROM wiki_page_suggestions WHERE page_id=?", Args: []any{id}},
			sqlite.Statement{SQL: "DELETE FROM wiki_page_sources WHERE page_id=?", Args: []any{id}})
		for _, s := range uniqueStrings(itemIDs) {
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO wiki_page_sources(page_id,item_id,created_at) VALUES(?,?,?)", Args: []any{id, s, nowMs()}})
		}
	}
	// 本批所有新页创建完成后再补齐前向出链
	for _, f := range forward {
		for _, t := range f.targets {
			if tid, ok := pageByTitle[t]; ok && tid != f.id {
				stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR IGNORE INTO wiki_page_links(from_page_id,to_page_id,created_at) VALUES(?,?,?)", Args: []any{f.id, tid, nowMs()}})
			}
		}
	}
	return stmts, nil
}

func (l *Library) wikiGetPage(ctx context.Context, id any) (any, error) {
	if err := l.flushWikiInvalidations(ctx); err != nil {
		return nil, err
	}
	bound, err := wikiBind(id)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.Query(ctx, "SELECT * FROM wiki_pages WHERE id = ?", bound)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	col := map[string]any{}
	for i, name := range rows.Columns {
		col[name] = rows.Values[0][i]
	}
	page := map[string]any{
		"id": col["id"], "title": col["title"], "normalizedTitle": col["normalized_title"], "kind": col["kind"], "summary": col["summary"],
		"body": col["body"], "aliasesJson": rowStrOrNil(col["aliases_json"]), "provider": col["provider"], "model": col["model"],
		"promptVersion": col["prompt_version"], "generatedAt": asInt(col["generated_at"]), "manualEditedAt": rowIntOrNil(col["manual_edited_at"]),
		"createdAt": asInt(col["created_at"]), "updatedAt": asInt(col["updated_at"]),
	}
	qualified := []string{}
	for _, c := range strings.Split(wikiCatalogColumns, ", ") {
		qualified = append(qualified, "p."+c)
	}
	bl, err := l.db.Query(ctx, `SELECT `+strings.Join(qualified, ", ")+`
       FROM wiki_page_links l
       JOIN wiki_pages p ON p.id = l.from_page_id
       WHERE l.to_page_id = ?
       ORDER BY p.updated_at DESC`, bound)
	if err != nil {
		return nil, err
	}
	backlinks := []any{}
	for _, r := range bl.Values {
		backlinks = append(backlinks, catalogEntry(r))
	}
	sr, err := l.db.Query(ctx, `SELECT i.id AS item_id, i.title AS title, i.review_status, i.review_reasons
       FROM wiki_page_sources s
       JOIN knowledge_items i ON i.id = s.item_id
       WHERE s.page_id = ? AND i.deleted_at IS NULL
       ORDER BY s.created_at ASC`, bound)
	if err != nil {
		return nil, err
	}
	sources := jsArr{}
	for _, r := range sr.Values {
		reasonsText, _ := r[3].(string)
		if reasonsText == "" {
			reasonsText = "[]"
		}
		reasons, err := jsParse(reasonsText)
		if err != nil {
			return nil, errors.New("Unexpected token in JSON: " + err.Error())
		}
		title, _ := r[1].(string)
		if title == "" {
			title = "无标题"
		}
		o := newObj()
		o.set("itemId", r[0])
		o.set("reviewStatus", orNull(r[2]))
		o.set("reviewReasons", reasons)
		o.set("title", title)
		sources = append(sources, o)
	}
	srcJSON, _ := jsStringify(sources)
	return map[string]any{"page": page, "backlinks": backlinks, "sources": json.RawMessage(srcJSON)}, nil
}

func orNull(v any) any {
	if v == nil {
		return jsNull
	}
	return v
}

func (l *Library) wikiUpdatePage(ctx context.Context, input any) (bool, error) {
	str := func(v any) string { // String(v ?? "")
		if isNullish(v) {
			return ""
		}
		return jsToString(v)
	}
	pageID := str(jsOptGet(input, "pageId"))
	body := str(jsOptGet(input, "body"))
	var targets []string
	if arr, ok := jsOptGet(input, "linkTargets").(jsArr); ok {
		for _, e := range arr {
			if s, ok := e.(string); ok {
				targets = append(targets, s)
			}
		}
	}
	release := jsStrictEqual(jsOptGet(input, "releaseToAuto"), true)

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	ex, err := l.db.Query(ctx, "SELECT title, summary FROM wiki_pages WHERE id = ?", pageID)
	if err != nil {
		return false, err
	}
	if len(ex.Values) == 0 {
		return false, nil
	}
	title, summary := ex.Values[0][0].(string), ex.Values[0][1].(string)
	now := nowMs()
	stmts := wikiSaveRevisionStmts(pageID, now)
	var manual any = now
	if release {
		manual = nil
	}
	stmts = append(stmts,
		sqlite.Statement{SQL: "UPDATE wiki_pages SET body = ?, manual_edited_at = ?, updated_at = ? WHERE id = ?", Args: []any{body, manual, now, pageID}},
		sqlite.Statement{SQL: "DELETE FROM wiki_page_links WHERE from_page_id = ?", Args: []any{pageID}})
	for _, t := range uniqueStrings(targets) {
		tr, err := l.db.Query(ctx, "SELECT id FROM wiki_pages WHERE normalized_title = ?", t)
		if err != nil {
			return false, err
		}
		if len(tr.Values) > 0 && tr.Values[0][0].(string) != pageID {
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR IGNORE INTO wiki_page_links (from_page_id, to_page_id, created_at) VALUES (?, ?, ?)",
				Args: []any{pageID, tr.Values[0][0].(string), now}})
		}
	}
	stmts = append(stmts, wikiFtsStmts(pageID, title, summary, body)...)
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return false, err
	}
	return true, nil
}

func (l *Library) wikiRestoreRevision(ctx context.Context, id any) (bool, error) {
	bound, err := wikiBind(id)
	if err != nil {
		return false, err
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	rows, err := l.db.Query(ctx, `SELECT id, page_id, title, kind, summary, body, aliases_json, model, prompt_version, created_at
       FROM wiki_page_revisions WHERE id = ?`, bound)
	if err != nil {
		return false, err
	}
	if len(rows.Values) == 0 {
		return false, nil
	}
	r := rows.Values[0]
	pageID := r[1].(string)
	now := nowMs()
	stmts := wikiSaveRevisionStmts(pageID, now)
	stmts = append(stmts, sqlite.Statement{SQL: `UPDATE wiki_pages SET
           title = ?, kind = ?, summary = ?, body = ?, aliases_json = ?, updated_at = ?, manual_edited_at = ?
         WHERE id = ?`, Args: []any{r[2], r[3], r[4], r[5], r[6], now, now, pageID}})
	stmts = append(stmts, wikiFtsStmts(pageID, r[2].(string), r[4].(string), r[5].(string))...)
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return false, err
	}
	return true, nil
}

func (l *Library) registerWiki(b *Bridge) {
	b.handlers["wiki:catalog"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		if err := l.flushWikiInvalidations(ctx); err != nil {
			return nil, err
		}
		rows, err := l.db.Query(ctx, "SELECT "+wikiCatalogColumns+" FROM wiki_pages ORDER BY updated_at DESC")
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, r := range rows.Values {
			out = append(out, catalogEntry(r))
		}
		return out, nil
	}
	b.handlers["wiki:backlinkCounts"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		rows, err := l.db.Query(ctx, `SELECT to_page_id AS page_id, COUNT(*) AS n FROM wiki_page_links GROUP BY to_page_id`)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for _, r := range rows.Values {
			out[r[0].(string)] = asInt(r[1])
		}
		return out, nil
	}
	b.handlers["wiki:search"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		q := jsArg(a, 0)
		query := ""
		if !isNullish(q) {
			query = jsToString(q)
		}
		limit := numOr(jsArg(a, 1), 5)
		if err := l.flushWikiInvalidations(ctx); err != nil {
			return nil, err
		}
		match, ok := BuildFtsMatchQuery(query, FtsRecall)
		if !ok {
			return []any{}, nil
		}
		bound, _ := wikiBind(math.Max(1, limit))
		rows, err := l.db.Query(ctx, `SELECT p.id, p.title, p.kind, p.summary
       FROM (
         SELECT page_id, bm25(wiki_fts, 0.0, 10.0, 3.0, 1.0) AS rank
         FROM wiki_fts WHERE wiki_fts MATCH ?
       ) f
       JOIN wiki_pages p ON p.id = f.page_id
       ORDER BY f.rank ASC
       LIMIT ?`, match, bound)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, r := range rows.Values {
			out = append(out, map[string]any{"id": r[0], "title": r[1], "kind": r[2], "summary": r[3]})
		}
		return out, nil
	}
	b.handlers["wiki:getPage"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.wikiGetPage(ctx, jsArg(a, 0))
	}
	b.handlers["wiki:updatePage"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.wikiUpdatePage(ctx, jsArg(a, 0))
	}
	b.handlers["wiki:deletePage"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		v := jsArg(a, 0)
		id := ""
		if !isNullish(v) {
			id = jsToString(v)
		}
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		res, err := l.db.Transaction(ctx, []sqlite.Statement{
			{SQL: "DELETE FROM wiki_fts WHERE page_id = ?", Args: []any{id}},
			{SQL: "DELETE FROM wiki_pages WHERE id = ?", Args: []any{id}},
		})
		if err != nil {
			return nil, err
		}
		return res[1].Changes > 0, nil
	}
	b.handlers["wiki:status"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		count := func(sql string) (int64, error) {
			r, err := l.db.Query(ctx, sql)
			if err != nil {
				return 0, err
			}
			return asInt(r.Values[0][0]), nil
		}
		pages, err := count("SELECT COUNT(*) AS c FROM wiki_pages")
		if err != nil {
			return nil, err
		}
		compiled, err := count(`SELECT COUNT(*) AS c FROM wiki_ingestions w
           JOIN knowledge_items i ON i.id = w.item_id AND i.deleted_at IS NULL
           WHERE w.prompt_version != ''`)
		if err != nil {
			return nil, err
		}
		eligible, err := count("SELECT COUNT(*) AS c FROM knowledge_items WHERE deleted_at IS NULL AND review_status != 'needs_review' AND content != ''")
		if err != nil {
			return nil, err
		}
		return map[string]any{"pageCount": pages, "compiledItemCount": compiled, "eligibleItemCount": eligible}, nil
	}
	b.handlers["wiki:graph"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		total, err := l.db.Query(ctx, "SELECT COUNT(*) AS c FROM wiki_pages")
		if err != nil {
			return nil, err
		}
		nodeRows, err := l.db.Query(ctx, `SELECT p.id, p.title, p.kind
       FROM wiki_pages p
       LEFT JOIN (
         SELECT from_page_id AS id, COUNT(*) AS n FROM wiki_page_links GROUP BY from_page_id
         UNION ALL
         SELECT to_page_id AS id, COUNT(*) AS n FROM wiki_page_links GROUP BY to_page_id
       ) d ON d.id = p.id
       GROUP BY p.id
       ORDER BY COALESCE(SUM(d.n), 0) DESC, p.updated_at DESC
       LIMIT ?`, int64(300))
		if err != nil {
			return nil, err
		}
		visible := map[string]bool{}
		nodes := []any{}
		for _, r := range nodeRows.Values {
			visible[r[0].(string)] = true
			nodes = append(nodes, map[string]any{"id": r[0], "title": r[1], "kind": r[2]})
		}
		linkRows, err := l.db.Query(ctx, "SELECT from_page_id, to_page_id FROM wiki_page_links")
		if err != nil {
			return nil, err
		}
		links := []any{}
		for _, r := range linkRows.Values {
			if visible[r[0].(string)] && visible[r[1].(string)] {
				links = append(links, map[string]any{"source": r[0], "target": r[1]})
			}
		}
		return map[string]any{"nodes": nodes, "links": links, "totalNodes": asInt(total.Values[0][0])}, nil
	}
	b.handlers["wiki:listRevisions"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		bound, err := wikiBind(jsArg(a, 0))
		if err != nil {
			return nil, err
		}
		rows, err := l.db.Query(ctx, `SELECT id, page_id, title, kind, summary, body, aliases_json, model, prompt_version, created_at
       FROM wiki_page_revisions WHERE page_id = ? ORDER BY created_at DESC`, bound)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, r := range rows.Values {
			out = append(out, revisionEntry(r))
		}
		return out, nil
	}
	b.handlers["wiki:restoreRevision"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.wikiRestoreRevision(ctx, jsArg(a, 0))
	}
}
