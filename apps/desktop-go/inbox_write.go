package main

// 对应 apps/desktop/src/main/ipc/inbox.ipc.ts 与 services/inbox.ts 的写入类频道：
// inbox:organize / markReviewed / acknowledgeImportWarning / aiClassificationSources / applyAiClassification。
// 由 write_test.go 用真实的 inbox.ipc.ts 处理函数（electron 换成桩）生成的基准对拍。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/plugins/sqlite"
	"golang.org/x/text/unicode/norm"
)

const (
	inboxAIMaxItems          = 100
	inboxAIMaxNewCollections = 8
	inboxAINameMaxLength     = 20
	classificationExcerptLen = 1600
)

var reservedNewCollectionNames = map[string]bool{
	"全部": true, "未分类": true, "未归知识库": true, "收藏": true, "归档": true, "回收站": true,
}

// jsCollapseSpaces 对应 JS 的 `.replace(/\s+/g, " ")`。
func jsCollapseSpaces(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if jsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

func normalizeAiCollectionName(s string) string {
	return jsTrim(jsCollapseSpaces(norm.NFKC.String(s)))
}

// jsLower 对应 JS 的 toLowerCase / toLocaleLowerCase（非土耳其语区域）：
// 与 Go 的 strings.ToLower 有两处差异——İ（U+0130）展开为 i + U+0307，词尾 Σ 变 ς。
func jsLower(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		switch {
		case r == 0x130:
			b.WriteString("i\u0307")
		case r == 0x3a3 && isFinalSigma(rs, i):
			b.WriteRune(0x3c2)
		default:
			b.WriteString(strings.ToLower(string(r)))
		}
	}
	return b.String()
}

// isFinalSigma 实现 Unicode 的 Final_Sigma 条件：前面有字母（可隔着 case-ignorable），后面没有字母。
func isFinalSigma(rs []rune, i int) bool {
	isCased := func(r rune) bool {
		return strings.ToLower(string(r)) != string(r) || strings.ToUpper(string(r)) != string(r)
	}
	isIgnorable := func(r rune) bool {
		switch r {
		case '\'', '.', ':', '^', '`', '\u00a8', '\u00ad', '\u00af', '\u00b4', '\u00b7', '\u00b8', '\u2019':
			return true
		}
		return r >= 0x300 && r <= 0x36f
	}
	j := i - 1
	for j >= 0 && isIgnorable(rs[j]) {
		j--
	}
	if j < 0 || !isCased(rs[j]) {
		return false
	}
	k := i + 1
	for k < len(rs) && isIgnorable(rs[k]) {
		k++
	}
	return !(k < len(rs) && isCased(rs[k]))
}

func aiCollectionNameKey(s string) string { return jsLower(normalizeAiCollectionName(s)) }

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func isValidAiCollectionName(s string) bool {
	n := normalizeAiCollectionName(s)
	l := utf16Len(n)
	return l > 0 && l <= inboxAINameMaxLength && !strings.ContainsAny(n, "\r\n\t")
}

// utf16Slice 对应 JS 的 s.slice(0, n)（按 UTF-16 码元）。被截断的代理对在 Go 字符串里无法表示，用 U+FFFD。
func utf16Slice(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if len(u) <= n {
		return s
	}
	return string(utf16.Decode(u[:n]))
}

type inboxAISource struct {
	ItemID  string `json:"itemId"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}

func uniqueClassificationIDs(ids []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		t := jsTrim(id)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > inboxAIMaxItems {
		return nil, fmt.Errorf("一次最多智能归类 %d 条内容", inboxAIMaxItems)
	}
	return out, nil
}

func (l *Library) inboxAISources(ctx context.Context, ids []string) ([]inboxAISource, error) {
	uniq, err := uniqueClassificationIDs(ids)
	if err != nil {
		return nil, err
	}
	out := []inboxAISource{}
	if len(uniq) == 0 {
		return out, nil
	}
	args := append([]any{int64(classificationExcerptLen)}, anySlice(uniq)...)
	rows, err := l.db.Query(ctx, `SELECT id, title,
            SUBSTR(CASE WHEN TRIM(COALESCE(summary, '')) != '' THEN summary
                        ELSE COALESCE(NULLIF(content, ''), transcript, '') END,
                   1, ?) AS excerpt
     FROM knowledge_items
     WHERE id IN (`+placeholders(len(uniq), ",")+`)
       AND deleted_at IS NULL AND status = 'active' AND collection_id IS NULL
       AND review_status != 'needs_review'`, args...)
	if err != nil {
		return nil, err
	}
	byID := map[string][]any{}
	for _, r := range rows.Values {
		byID[r[0].(string)] = r
	}
	for _, id := range uniq {
		r, ok := byID[id]
		if !ok {
			continue
		}
		excerpt, _ := r[2].(string)
		title, _ := r[1].(string)
		out = append(out, inboxAISource{
			ItemID: id, Title: title,
			Excerpt: utf16Slice(jsTrim(jsCollapseSpaces(excerpt)), classificationExcerptLen),
		})
	}
	return out, nil
}

type aiAssignment struct {
	ItemID, CollectionName string
}

// inboxApplyAI 对应 applyInboxAiClassification：同一事务里新建缺失集合并分组归入。
func (l *Library) inboxApplyAI(ctx context.Context, raw []aiAssignment) (any, error) {
	if len(raw) > inboxAIMaxItems {
		return nil, fmt.Errorf("一次最多智能归类 %d 条内容", inboxAIMaxItems)
	}
	seen := map[string]bool{}
	var assignments []aiAssignment
	for _, a := range raw {
		itemID := jsTrim(a.ItemID)
		name := normalizeAiCollectionName(a.CollectionName)
		if itemID == "" || seen[itemID] {
			return nil, errors.New("AI 归类计划包含缺失或重复的条目")
		}
		if !isValidAiCollectionName(name) {
			if name == "" {
				name = "空名称"
			}
			return nil, fmt.Errorf("AI 生成了无效的合集名称：%s", name)
		}
		seen[itemID] = true
		assignments = append(assignments, aiAssignment{itemID, name})
	}
	created := []string{}
	if len(assignments) == 0 {
		return map[string]any{"classified": int64(0), "skipped": int64(0), "createdCollectionNames": created}, nil
	}

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	ids := make([]string, len(assignments))
	for i, a := range assignments {
		ids[i] = a.ItemID
	}
	sources, err := l.inboxAISources(ctx, ids)
	if err != nil {
		return nil, err
	}
	eligible := map[string]bool{}
	for _, s := range sources {
		eligible[s.ItemID] = true
	}
	var applicable []aiAssignment
	for _, a := range assignments {
		if eligible[a.ItemID] {
			applicable = append(applicable, a)
		}
	}
	if len(applicable) == 0 {
		return map[string]any{"classified": int64(0), "skipped": int64(len(assignments)), "createdCollectionNames": created}, nil
	}

	rows, err := l.db.Query(ctx, `SELECT id, name, sort_order FROM collections ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, err
	}
	collectionIDByKey := map[string]string{}
	for _, r := range rows.Values {
		collectionIDByKey[aiCollectionNameKey(r[1].(string))] = r[0].(string) // 同键后者覆盖，与 Map 构造一致
	}
	// 对应 `new Map(entries).values()`：顺序取键第一次出现的位置，值取最后一次出现的写法
	var missingKeys []string
	missingName := map[string]string{}
	for _, a := range applicable {
		k := aiCollectionNameKey(a.CollectionName)
		if _, ok := collectionIDByKey[k]; ok {
			continue
		}
		if _, seen := missingName[k]; !seen {
			missingKeys = append(missingKeys, k)
		}
		missingName[k] = a.CollectionName
	}
	missing := make([]string, 0, len(missingKeys))
	for _, k := range missingKeys {
		missing = append(missing, missingName[k])
	}
	if reservedHit := firstReserved(missing); reservedHit != "" {
		return nil, fmt.Errorf("AI 不能创建保留的导航名称：%s", reservedHit)
	}
	if len(missing) > inboxAIMaxNewCollections {
		return nil, fmt.Errorf("AI 计划新建 %d 个合集，超过安全上限 %d", len(missing), inboxAIMaxNewCollections)
	}

	maxRows, err := l.db.Query(ctx, `SELECT COALESCE(MAX(sort_order), -1) FROM collections`)
	if err != nil {
		return nil, err
	}
	nextOrder := asInt(maxRows.Values[0][0]) + 1
	now := nowMs()
	var stmts []sqlite.Statement
	for _, n := range missing {
		id := newUUID()
		stmts = append(stmts, sqlite.Statement{
			SQL:  "INSERT INTO collections (id, name, icon, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			Args: []any{id, n, nil, nextOrder, now, now},
		})
		nextOrder++
		collectionIDByKey[aiCollectionNameKey(n)] = id
		created = append(created, n)
	}
	var order []string
	byCollection := map[string][]string{}
	for _, a := range applicable {
		cid := collectionIDByKey[aiCollectionNameKey(a.CollectionName)]
		if _, ok := byCollection[cid]; !ok {
			order = append(order, cid)
		}
		byCollection[cid] = append(byCollection[cid], a.ItemID)
	}
	updateIdx := []int{}
	for _, cid := range order {
		ids := byCollection[cid]
		for start := 0; start < len(ids); start += bulkWriteChunkSize {
			batch := ids[start:min(start+bulkWriteChunkSize, len(ids))]
			updateIdx = append(updateIdx, len(stmts))
			stmts = append(stmts, sqlite.Statement{
				SQL:  "UPDATE knowledge_items SET updated_at = ?, collection_id = ? WHERE id IN (" + placeholders(len(batch), ", ") + ")",
				Args: append([]any{now, cid}, anySlice(batch)...),
			})
		}
	}
	results, err := l.db.Transaction(ctx, stmts)
	if err != nil {
		return nil, err
	}
	var classified int64
	for _, i := range updateIdx {
		classified += results[i].Changes
	}
	return map[string]any{
		"classified": classified, "skipped": int64(len(assignments) - len(applicable)), "createdCollectionNames": created,
	}, nil
}

func firstReserved(names []string) string {
	for _, n := range names {
		if reservedNewCollectionNames[normalizeAiCollectionName(n)] {
			return n
		}
	}
	return ""
}

func (l *Library) inboxAcknowledgeWarning(ctx context.Context, taskID string) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	now := nowMs()
	res, err := l.db.Transaction(ctx, []sqlite.Statement{{
		SQL: `UPDATE import_tasks
         SET warning_acknowledged_at = ?, updated_at = ?
         WHERE id = ? AND status = 'completed' AND warning IS NOT NULL
           AND warning_acknowledged_at IS NULL`,
		Args: []any{now, now, taskID},
	}})
	if err != nil {
		return nil, err
	}
	if res[0].Changes > 0 {
		return int64(1), nil
	}
	return int64(0), nil
}

// inboxIDs 对应 inbox.ipc.ts 的 ids()：非数组得空；只留非空字符串并去重（不 trim）。
func inboxIDs(raw json.RawMessage) []string {
	ids := normalizeIDs(raw)
	return uniqueStrings(ids)
}

func (l *Library) registerInboxWrite(b *Bridge) {
	b.handlers["inbox:organize"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var in struct {
			ItemIDs json.RawMessage `json:"itemIds"`
			BulkUpdateInput
		}
		if raw := argAt(a, 0); string(raw) != "null" {
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
		}
		// 只把 collectionId 与 addTagNames 交给批量更新，其余字段忽略（与 organizeInboxItems 一致）
		pass := BulkUpdateInput{CollectionID: in.CollectionID, AddTagNames: in.AddTagNames}
		return l.knowledgeBulkUpdate(ctx, inboxIDs(in.ItemIDs), pass)
	}
	b.handlers["inbox:markReviewed"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		in := BulkUpdateInput{ReviewStatus: optString{Set: true, Val: "clear"}, ReviewReasons: optStrings{Set: true}}
		return l.knowledgeBulkUpdate(ctx, inboxIDs(argAt(a, 0)), in)
	}
	b.handlers["inbox:acknowledgeImportWarning"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var s string
		_ = json.Unmarshal(argAt(a, 0), &s)
		return l.inboxAcknowledgeWarning(ctx, jsTrim(s))
	}
	b.handlers["inbox:aiClassificationSources"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.inboxAISources(ctx, inboxIDs(argAt(a, 0)))
	}
	b.handlers["inbox:applyAiClassification"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		// 对应 classificationInput()：非数组按空；元素不是对象按 {}；字段不是字符串按 ""
		var in struct {
			Assignments []json.RawMessage `json:"assignments"`
		}
		_ = json.Unmarshal(argAt(a, 0), &in)
		list := make([]aiAssignment, 0, len(in.Assignments))
		for _, el := range in.Assignments {
			var m map[string]json.RawMessage
			_ = json.Unmarshal(el, &m)
			str := func(k string) string {
				var s string
				if json.Unmarshal(m[k], &s) != nil {
					return ""
				}
				return s
			}
			list = append(list, aiAssignment{str("itemId"), str("collectionName")})
		}
		return l.inboxApplyAI(ctx, list)
	}
}
