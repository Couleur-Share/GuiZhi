package main

// 对应 knowledge.ipc.ts 的 knowledge:deleteForever / knowledge:emptyTrash 与 services/asset-cleanup.ts、
// packages/db 的 listAssetRefs / listReferencedAssets / clearAskEvidence。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/sqlite"
)

var (
	assetLeaseMu sync.Mutex
	assetLeases  = map[string]int{}
)

// retainAssetFiles 保护落盘到引用提交之间的资产；同一资源可被多个任务或备份同时租用。
func retainAssetFiles(files []string) (func(), error) {
	var names []string
	seen := map[string]bool{}
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			names = append(names, f)
		}
	}
	for _, n := range names {
		if !isSafeAssetFileName(n) {
			return nil, errors.New("资产租约包含无效文件名")
		}
	}
	assetLeaseMu.Lock()
	for _, n := range names {
		assetLeases[n]++
	}
	assetLeaseMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			assetLeaseMu.Lock()
			defer assetLeaseMu.Unlock()
			for _, n := range names {
				if assetLeases[n] <= 1 {
					delete(assetLeases, n)
				} else {
					assetLeases[n]--
				}
			}
		})
	}, nil
}

// resolveAssetPath 资产文件名 → 磁盘路径；越界或非法命名返回空串。
func resolveAssetPath(fileName string, directories []string) string {
	if !isSafeAssetFileName(fileName) {
		return ""
	}
	for _, dir := range directories {
		candidate := filepath.Join(dir, fileName)
		rel, err := filepath.Rel(dir, candidate)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func (l *Library) tableExists(ctx context.Context, name string) (bool, error) {
	rows, err := l.db.Query(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", name)
	if err != nil {
		return false, err
	}
	return len(rows.Values) > 0, nil
}

// payloadAssetRefs 网页版本/抓取页 payload 里的资产名：正文里的 local-* 引用与快照资产文件。
func payloadAssetRefs(version any, markdownToo bool, add func(string)) error {
	if markdownToo {
		md := jsMemberOpt(version, "markdown")
		switch m := md.(type) {
		case jsUndefinedT, jsNullT:
		case string:
			for _, r := range extractAllLocalAssetRefs(m) {
				add(r)
			}
		default:
			if vTruthy(m) {
				return errors.New("content.matchAll is not a function")
			}
		}
	}
	assets := jsOptGet(jsOptGet(version, "snapshot"), "assets")
	switch list := assets.(type) {
	case jsUndefinedT, jsNullT:
	case jsArr:
		for _, a := range list {
			if isNullish(a) {
				return errors.New("Cannot read properties of null (reading 'fileName')")
			}
			if s, ok := jsGet(a, "fileName").(string); ok {
				add(s)
			}
		}
	default:
		return errors.New("assets is not iterable")
	}
	return nil
}

// jsMemberOpt 对应 `v?.key`，非对象返回 undefined。
func jsMemberOpt(v any, key string) any { return jsOptGet(v, key) }

func parsePayload(raw any) (any, error) {
	s, _ := raw.(string)
	return jsParse(s)
}

// listAssetRefs 这些条目正文里引用的资产文件名（彻底删除前取，用于清理磁盘）。
func (l *Library) listAssetRefs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	ph := placeholders(len(ids), ", ")
	args := append(anySlice(ids), anySlice(ids)...)
	rows, err := l.db.Query(ctx, "SELECT content FROM knowledge_items WHERE id IN ("+ph+") UNION ALL SELECT content FROM source_capture_revisions WHERE item_id IN ("+ph+")", args...)
	if err != nil {
		return nil, err
	}
	var refs []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			refs = append(refs, s)
		}
	}
	for _, r := range rows.Values {
		c, _ := r[0].(string)
		for _, ref := range extractAllLocalAssetRefs(c) {
			add(ref)
		}
	}
	web, err := l.db.Query(ctx, "SELECT payload FROM web_source_versions WHERE item_id IN ("+ph+")", anySlice(ids)...)
	if err != nil {
		return nil, err
	}
	for _, r := range web.Values {
		v, err := parsePayload(r[0])
		if err != nil {
			return nil, err
		}
		if err := payloadAssetRefs(v, true, add); err != nil {
			return nil, err
		}
	}
	themed, err := l.themedReadingAssetFiles(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, n := range themed {
		add(n)
	}
	return refs, nil
}

// themedReadingAssetFiles 对应 listThemedReadingAssetFiles：ids 为 nil 表示全库。
func (l *Library) themedReadingAssetFiles(ctx context.Context, ids []string) ([]string, error) {
	ok, err := l.tableExists(ctx, "themed_reading_versions")
	if err != nil || !ok {
		return nil, err
	}
	sql, args := `SELECT DISTINCT a.file_name FROM themed_reading_assets a
    JOIN themed_reading_versions v ON v.id=a.version_id`, []any{}
	if ids != nil {
		if len(ids) == 0 {
			return nil, nil
		}
		sql += " WHERE v.item_id IN (" + placeholders(len(ids), ",") + ")"
		args = anySlice(ids)
	}
	rows, err := l.db.Query(ctx, sql+" ORDER BY a.file_name", args...)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows.Values {
		if s, ok := r[0].(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// listReferencedAssets 全库仍在引用的资产文件名。
func (l *Library) listReferencedAssets(ctx context.Context) (map[string]bool, error) {
	rows, err := l.db.Query(ctx, `SELECT content FROM knowledge_items
       WHERE content LIKE '%local-image://%' OR content LIKE '%local-video://%'
       UNION ALL SELECT content FROM source_capture_revisions
       WHERE content LIKE '%local-image://%' OR content LIKE '%local-video://%'`)
	if err != nil {
		return nil, err
	}
	ref := map[string]bool{}
	add := func(s string) { ref[s] = true }
	for _, r := range rows.Values {
		c, _ := r[0].(string)
		for _, name := range extractAllLocalAssetRefs(c) {
			add(name)
		}
	}
	web, err := l.db.Query(ctx, "SELECT payload FROM web_source_versions")
	if err != nil {
		return nil, err
	}
	for _, r := range web.Values {
		v, err := parsePayload(r[0])
		if err != nil {
			return nil, err
		}
		if err := payloadAssetRefs(v, true, add); err != nil {
			return nil, err
		}
	}
	crawl, err := l.db.Query(ctx, "SELECT payload FROM crawl_pages")
	if err != nil {
		return nil, err
	}
	for _, r := range crawl.Values {
		v, err := parsePayload(r[0])
		if err != nil {
			return nil, err
		}
		if err := payloadAssetRefs(jsOptGet(v, "result"), false, add); err != nil {
			return nil, err
		}
	}
	themed, err := l.themedReadingAssetFiles(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, n := range themed {
		add(n)
	}
	return ref, nil
}

// cleanupOrphanAssets 删除不再被任何条目引用的资产文件，返回实际删除的数量。
// 调用方须在删除条目之前收集 candidates（那时正文还在），删除之后再调用本函数。
func (l *Library) cleanupOrphanAssets(ctx context.Context, candidates []string) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}
	dirs := []string{l.paths.ImagesDir(), l.paths.VideosDir()}
	referenced, err := l.listReferencedAssets(ctx)
	if err != nil {
		return 0, err
	}
	if l.leasedSnapshotAssets != nil {
		for _, n := range l.leasedSnapshotAssets() {
			referenced[n] = true
		}
	}
	assetLeaseMu.Lock()
	for n := range assetLeases {
		referenced[n] = true
	}
	assetLeaseMu.Unlock()
	removed := 0
	for _, name := range candidates {
		if referenced[name] { // 同一份资产可能被复制粘贴到别的条目，仍被引用就不能删
			continue
		}
		path := resolveAssetPath(name, dirs)
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("[assets] 清理资产失败 %s: %v", name, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		log.Printf("[assets] 已清理 %d 个不再被引用的资产文件", removed)
	}
	return removed, nil
}

// clearAskEvidenceStmts 对应 clearAskEvidence：登记清除记录，并把所有问答会话里对这些条目的证据抹掉。
// 调用方须持有 writeMu，并把返回的语句与其他写入放进同一个事务。
func (l *Library) clearAskEvidenceStmts(ctx context.Context, ids []string) ([]sqlite.Statement, error) {
	now := nowMs()
	var stmts []sqlite.Statement
	for _, id := range ids {
		stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR REPLACE INTO ask_evidence_clearances(item_id,cleared_at) VALUES(?,?)", Args: []any{id, now}})
	}
	cleared, err := l.clearedSet(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		cleared[id] = true
	}
	inIDs := map[string]bool{}
	for _, id := range ids {
		inIDs[id] = true
	}
	rows, err := l.db.Query(ctx, "SELECT id,messages_json,options_json,item_id FROM ask_sessions")
	if err != nil {
		return nil, err
	}
	for _, r := range rows.Values {
		messages, _ := r[1].(string)
		sanitized, err := sanitizeEvidenceJSON(messages, cleared)
		if err != nil {
			return nil, err
		}
		optionsRaw, _ := r[2].(string)
		if optionsRaw == "" {
			optionsRaw = "{}"
		}
		options, err := jsParse(optionsRaw)
		if err != nil {
			return nil, err
		}
		if itemID, ok := r[3].(string); ok && inIDs[itemID] {
			if isNullish(options) {
				return nil, fmt.Errorf("Cannot read properties of %s (reading 'target')", jsToString(options))
			}
			if target := jsGet(options, "target"); vTruthy(target) {
				t := newObj()
				t.set("itemId", itemID)
				t.set("view", jsGet(target, "view"))
				if o, ok := options.(*jsObj); ok {
					o.set("target", t)
				}
			}
		}
		optionsOut, ok := jsStringify(options)
		if !ok {
			return nil, errors.New("options 无法序列化")
		}
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE ask_sessions SET messages_json=?,options_json=? WHERE id=?", Args: []any{sanitized, optionsOut, r[0]}})
	}
	return stmts, nil
}

// deleteForeverWithCleanup 对应 knowledge:deleteForever 的整体流程：取资产引用 → 事务（可选清证据 + 删除）→ 回收磁盘资产。
func (l *Library) deleteForeverWithCleanup(ctx context.Context, ids []string, clearEvidence bool) (any, error) {
	if l.cancelThemedReading != nil {
		l.cancelThemedReading(ids)
	}
	refs, err := l.listAssetRefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	changed, err := l.deleteForeverTx(ctx, ids, clearEvidence)
	if err != nil {
		return nil, err
	}
	if _, err := l.cleanupOrphanAssets(ctx, refs); err != nil {
		return nil, err
	}
	return changed, nil
}

func (l *Library) deleteForeverTx(ctx context.Context, ids []string, clearEvidence bool) (int64, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	var stmts []sqlite.Statement
	if clearEvidence {
		s, err := l.clearAskEvidenceStmts(ctx, ids)
		if err != nil {
			return 0, err
		}
		stmts = s
	}
	del, deletes, err := l.deleteForeverStmts(ctx, ids)
	if err != nil {
		return 0, err
	}
	base := len(stmts)
	stmts = append(stmts, del...)
	if len(stmts) == 0 {
		return 0, nil
	}
	results, err := l.db.Transaction(ctx, stmts)
	if err != nil {
		return 0, err
	}
	var changed int64
	for _, i := range deletes {
		changed += results[base+i].Changes
	}
	return changed, nil
}

func (l *Library) registerKnowledgeCleanup(b *Bridge) {
	b.handlers["knowledge:deleteForever"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.deleteForeverWithCleanup(ctx, normalizeIDs(argAt(a, 0)), jsOptGet(jsArg(a, 1), "clearEvidence") == true)
	}
	b.handlers["knowledge:emptyTrash"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		rows, err := l.db.Query(ctx, "SELECT id FROM knowledge_items WHERE deleted_at IS NOT NULL")
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(rows.Values))
		for _, r := range rows.Values {
			ids = append(ids, r[0].(string))
		}
		return l.deleteForeverWithCleanup(ctx, ids, jsOptGet(jsArg(a, 0), "clearEvidence") == true)
	}
}
