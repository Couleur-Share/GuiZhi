package main

// 对应 packages/db/src/{ask-session,ask-history}.ts 与 ipc/ask.ipc.ts：
// askSession:query / askSession:meta / ask:listSessions / ask:getSession / ask:saveSession / ask:deleteSession。
// 消息体 messages_json、选项 options_json 与分页游标都经过 JS 值模型（jsvalue.go），保证与 TS 的字节一致。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

// bindArg 把 JS 值转成 SQLite 绑定参数。对象和数组在 TS 的 SQLite 绑定里会报错，这里同样返回错误。
func bindArg(v any) (any, error) {
	switch x := v.(type) {
	case jsUndefinedT, jsNullT:
		return nil, nil
	case string:
		return x, nil
	case bool:
		if x {
			return int64(1), nil
		}
		return int64(0), nil
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return int64(x), nil
		}
		return x, nil
	}
	return nil, errors.New("无法绑定该类型的参数")
}

func jsStrictEqual(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case jsNullT:
		_, ok := b.(jsNullT)
		return ok
	case jsUndefinedT:
		_, ok := b.(jsUndefinedT)
		return ok
	}
	return false
}

// decodeBase64URLLoose 复刻 Node 的 Buffer.from(s, 'base64url')：两套字母表都收，忽略非法字符，遇到 '=' 停止，丢弃残余位。
func decodeBase64URLLoose(s string) []byte {
	var out []byte
	var acc uint32
	bits := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		var v int
		switch {
		case c >= 'A' && c <= 'Z':
			v = int(c - 'A')
		case c >= 'a' && c <= 'z':
			v = int(c-'a') + 26
		case c >= '0' && c <= '9':
			v = int(c-'0') + 52
		case c == '-' || c == '+':
			v = 62
		case c == '_' || c == '/':
			v = 63
		case c == '=':
			i = len(s)
			continue
		default:
			continue
		}
		acc = acc<<6 | uint32(v)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>uint(bits)))
			acc &= (1 << uint(bits)) - 1
		}
	}
	return out
}

func rowNum(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

func rowStr(v any) any {
	if s, ok := v.(string); ok {
		return s
	}
	return nil
}

// ── ask-history.ts ──

func (l *Library) askSessionQuery(ctx context.Context, input any) (any, error) {
	if _, isStr := input.(string); isStr {
		// 字符串自带 String.prototype.search 方法，`input.search?.trim` 会得到 undefined 再被调用而抛错
		return nil, errors.New("input.search?.trim is not a function")
	}
	var searchTrim string
	if s := jsGet(input, "search"); !isNullish(s) {
		str, ok := s.(string)
		if !ok {
			return nil, errors.New("input.search.trim is not a function")
		}
		searchTrim = jsTrim(str)
	}
	scope, itemID := jsGet(input, "scope"), jsGet(input, "itemId")
	from, to := jsGet(input, "from"), jsGet(input, "to")
	cursorIn := jsGet(input, "cursor")

	fpArr := jsArr{scope, itemID, searchTrim, from, to}
	fingerprint, _ := jsStringify(fpArr)

	var match string
	hasMatch := false
	if searchTrim != "" {
		match, hasMatch = BuildFtsMatchQuery(jsGet(input, "search").(string), FtsRecall)
		if !hasMatch {
			return map[string]any{"entries": []any{}, "nextCursor": nil}, nil
		}
	}
	conditions := []string{"1=1"}
	var params []any
	if hasMatch {
		params = append(params, match)
	}
	add := func(cond string, v ...any) error {
		conditions = append(conditions, cond)
		for _, x := range v {
			b, err := bindArg(x)
			if err != nil {
				return err
			}
			params = append(params, b)
		}
		return nil
	}
	if vTruthy(scope) {
		if err := add("s.scope=?", scope); err != nil {
			return nil, err
		}
	}
	if vTruthy(itemID) {
		if err := add("s.item_id=?", itemID); err != nil {
			return nil, err
		}
	}
	if _, undef := from.(jsUndefinedT); !undef {
		if err := add("s.updated_at>=?", from); err != nil {
			return nil, err
		}
	}
	if _, undef := to.(jsUndefinedT); !undef {
		if err := add("s.updated_at<=?", to); err != nil {
			return nil, err
		}
	}
	if vTruthy(cursorIn) {
		cs, ok := cursorIn.(string)
		if !ok {
			return nil, errors.New("cursor 必须是字符串")
		}
		parsed, err := jsParse(string(decodeBase64URLLoose(cs)))
		if err != nil {
			return nil, err
		}
		if isNullish(parsed) {
			return nil, errors.New("Cannot read properties of null")
		}
		curFP, curID := jsGet(parsed, "fingerprint"), jsGet(parsed, "id")
		if !jsStrictEqual(curFP, fingerprint) {
			return nil, errors.New("会话筛选已变化，请从第一页重新加载")
		}
		if _, isStr := curID.(string); !isStr {
			return nil, errors.New("会话筛选已变化，请从第一页重新加载")
		}
		if hasMatch {
			if err := add("(f.rank>? OR (f.rank=? AND (s.updated_at,s.id)<(?,?)))",
				jsGet(parsed, "rank"), jsGet(parsed, "rank"), jsGet(parsed, "updatedAt"), curID); err != nil {
				return nil, err
			}
		} else {
			if err := add("(COALESCE(m.pinned,0),s.updated_at,s.id)<(?,?,?)",
				jsGet(parsed, "pinned"), jsGet(parsed, "updatedAt"), curID); err != nil {
				return nil, err
			}
		}
	}
	limit := 50.0
	if lv := jsGet(input, "limit"); !isNullish(lv) {
		limit = jsToNumber(lv) // Math.max(1, "3") 会把字符串转成数字
	}
	if math.IsNaN(limit) {
		return nil, errors.New("datatype mismatch") // LIMIT NaN
	}
	limit = math.Min(500, math.Max(1, limit))

	rankCol, join, order := "0", "", "COALESCE(m.pinned,0) DESC"
	if hasMatch {
		rankCol = "f.rank"
		join = "JOIN (SELECT session_id,bm25(ask_session_fts,0,5,2,1) AS rank FROM ask_session_fts WHERE ask_session_fts MATCH ?) f ON f.session_id=s.id"
		order = "f.rank ASC"
	}
	lim, _ := bindArg(limit + 1)
	params = append(params, lim)
	rows, err := l.db.Query(ctx, `SELECT s.id,COALESCE(m.custom_title,s.title) AS title,COALESCE(m.pinned,0) AS pinned,s.created_at,s.updated_at,s.scope,s.item_id,s.article_title,s.options_json,`+rankCol+` AS rank
    FROM ask_sessions s LEFT JOIN ask_session_meta m ON m.id=s.id
    `+join+`
    WHERE `+strings.Join(conditions, " AND ")+` ORDER BY `+order+`,s.updated_at DESC,s.id DESC LIMIT ?`, params...)
	if err != nil {
		return nil, err
	}
	data := rows.Values
	more := float64(len(data)) > limit
	if more {
		data = data[:len(data)-1]
	}
	entries := make([]any, 0, len(data))
	for _, r := range data {
		e := newObj()
		e.set("id", r[0])
		e.set("title", r[1])
		e.set("pinned", rowNum(r[2]) == 1)
		e.set("createdAt", rowNum(r[3]))
		e.set("updatedAt", rowNum(r[4]))
		if r[5] == "article" {
			e.set("scope", "article")
			e.set("itemId", orUndefined(r[6]))
			e.set("articleTitle", orUndefined(r[7]))
			opts := "{}"
			if s, ok := r[8].(string); ok && s != "" {
				opts = s
			}
			parsedOpts, err := jsParse(opts)
			if err != nil {
				return nil, err
			}
			spreadInto(e, parsedOpts)
		}
		entries = append(entries, jsToPlain(e))
	}
	result := map[string]any{"entries": entries}
	if !vTruthy(cursorIn) {
		arows, err := l.db.Query(ctx, `SELECT s.item_id AS id,s.article_title AS title FROM ask_sessions s
    WHERE s.scope='article' AND s.item_id IS NOT NULL AND NOT EXISTS
      (SELECT 1 FROM ask_sessions newer WHERE newer.scope='article' AND newer.item_id=s.item_id AND (newer.updated_at,newer.id)>(s.updated_at,s.id))
    ORDER BY s.article_title,s.item_id`)
		if err != nil {
			return nil, err
		}
		articles := make([]any, 0, len(arows.Values))
		for _, r := range arows.Values {
			articles = append(articles, map[string]any{"id": r[0], "title": r[1]})
		}
		result["articles"] = articles
	}
	result["nextCursor"] = nil
	if more && len(data) > 0 {
		last := data[len(data)-1]
		c := newObj()
		c.set("fingerprint", fingerprint)
		c.set("rank", rowNum(last[9]))
		c.set("pinned", rowNum(last[2]))
		c.set("updatedAt", rowNum(last[4]))
		c.set("id", last[0])
		s, _ := jsStringify(c)
		result["nextCursor"] = base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	return result, nil
}

func orUndefined(v any) any {
	if v == nil {
		return jsUndefined
	}
	return v
}

// jsToPlain 把 JS 值模型转成可交给 encoding/json 的普通值（undefined 属性丢弃；属性顺序对 JSON 消费者无意义）。
func jsToPlain(v any) any {
	switch x := v.(type) {
	case *jsObj:
		m := map[string]any{}
		for _, k := range x.keys() {
			if _, undef := x.m[k].(jsUndefinedT); undef {
				continue
			}
			m[k] = jsToPlain(x.m[k])
		}
		return m
	case jsArr:
		out := make([]any, len(x))
		for i, e := range x {
			if _, undef := e.(jsUndefinedT); undef {
				out[i] = nil
			} else {
				out[i] = jsToPlain(e)
			}
		}
		return out
	case jsNullT:
		return nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return x
	}
	return v
}

// ── ask-session.ts ──

func (l *Library) askSessionGet(ctx context.Context, id string) (any, error) {
	rows, err := l.db.Query(ctx, `SELECT id,title,messages_json,created_at,updated_at,scope,item_id,article_title,options_json FROM ask_sessions WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	r := rows.Values[0]
	meta, err := l.db.Query(ctx, "SELECT custom_title,pinned FROM ask_session_meta WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	title := r[1]
	pinned := false
	if len(meta.Values) > 0 {
		if meta.Values[0][0] != nil {
			title = meta.Values[0][0]
		}
		pinned = rowNum(meta.Values[0][1]) == 1
	}
	out := newObj()
	out.set("id", r[0])
	out.set("title", title)
	out.set("pinned", pinned)
	out.set("messagesJson", r[2])
	out.set("createdAt", rowNum(r[3]))
	out.set("updatedAt", rowNum(r[4]))
	if r[5] == "article" {
		opts := "{}"
		if s, ok := r[8].(string); ok && s != "" {
			opts = s
		}
		parsed, err := jsParse(opts)
		if err != nil {
			return nil, err
		}
		if isNullish(parsed) {
			return nil, errors.New("Cannot read properties of null (reading 'webEnabled')")
		}
		out.set("scope", "article")
		out.set("itemId", orUndefined(r[6]))
		out.set("articleTitle", orUndefined(r[7]))
		// webEnabled: options.webEnabled !== false
		out.set("webEnabled", !jsStrictEqual(jsGet(parsed, "webEnabled"), false))
		out.set("target", jsGet(parsed, "target"))
	}
	return jsToPlain(out), nil
}

func (l *Library) clearedSet(ctx context.Context) (map[string]bool, error) {
	rows, err := l.db.Query(ctx, "SELECT item_id FROM ask_evidence_clearances")
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, r := range rows.Values {
		if s, ok := r[0].(string); ok {
			m[s] = true
		}
	}
	return m, nil
}

func (l *Library) askSessionSave(ctx context.Context, input any) (any, error) {
	id, ok1 := jsGet(input, "id").(string)
	title, ok2 := jsGet(input, "title").(string)
	messagesJSON, ok3 := jsGet(input, "messagesJson").(string)
	if !vTruthy(input) || !ok1 || !ok2 || !ok3 {
		return nil, errors.New("ask:saveSession 需要 { id, title, messagesJson }")
	}
	scope, itemID := jsGet(input, "scope"), jsGet(input, "itemId")
	target := jsGet(input, "target")
	if vTruthy(scope) {
		if s, ok := scope.(string); !ok || (s != "article" && s != "knowledge") {
			return nil, errors.New("会话范围无效")
		}
	}
	if s, _ := scope.(string); s == "article" {
		if !vTruthy(itemID) || !jsStrictEqual(jsOptGet(target, "itemId"), itemID) {
			return nil, errors.New("本文会话缺少有效文章关联")
		}
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	now := nowMs()
	cleared, err := l.clearedSet(ctx)
	if err != nil {
		return nil, err
	}
	sanitized, err := sanitizeEvidenceJSON(messagesJSON, cleared)
	if err != nil {
		return nil, err
	}
	var targetOut any = target
	if vTruthy(target) {
		tid, err := bindArg(jsGet(target, "itemId"))
		if err != nil {
			return nil, err
		}
		hit, err := l.db.Query(ctx, "SELECT item_id FROM ask_evidence_clearances WHERE item_id=?", tid)
		if err != nil {
			return nil, err
		}
		if len(hit.Values) > 0 {
			t := newObj()
			t.set("itemId", jsGet(target, "itemId"))
			t.set("view", jsGet(target, "view"))
			targetOut = t
		} else if targetOut, err = sanitizeEvidenceTarget(target); err != nil {
			return nil, err
		}
	}
	opt := newObj()
	opt.set("webEnabled", jsGet(input, "webEnabled"))
	opt.set("target", targetOut)
	optionsJSON, _ := jsStringify(opt)
	scopeVal, itemVal, artVal := any("knowledge"), any(nil), any(nil)
	if !isNullish(scope) {
		if scopeVal, err = bindArg(scope); err != nil {
			return nil, err
		}
	}
	if !isNullish(itemID) {
		if itemVal, err = bindArg(itemID); err != nil {
			return nil, err
		}
	}
	if at := jsGet(input, "articleTitle"); !isNullish(at) {
		if artVal, err = bindArg(at); err != nil {
			return nil, err
		}
	}
	// 写入后才能重建索引（索引读取的是落库后的行），所以分两步事务，与 TS 同一事务的可见效果一致：失败时都不留痕
	stmts := []sqlite.Statement{{
		SQL: `INSERT INTO ask_sessions (id, title, messages_json, created_at, updated_at, scope, item_id, article_title, options_json)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         title = excluded.title,
         messages_json = excluded.messages_json,
         updated_at = excluded.updated_at, scope = excluded.scope, item_id = excluded.item_id,
         article_title = excluded.article_title, options_json = excluded.options_json`,
		Args: []any{id, title, sanitized, now, now, scopeVal, itemVal, artVal, optionsJSON},
	}}
	// 索引行直接用将要写入的 title 与消息计算：meta 的自定义标题优先
	meta, err := l.db.Query(ctx, "SELECT custom_title FROM ask_session_meta WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	effective := title
	if len(meta.Values) > 0 {
		if s, ok := meta.Values[0][0].(string); ok {
			effective = s
		}
	}
	stmts = append(stmts, historyStmts(id, effective, sanitized)...)
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return l.askSessionGet(ctx, id)
}

// historyStmts 对应 writeAskHistory：按「生效标题 = meta 自定义标题 ?? 会话标题」与消息重建全文索引行。
func historyStmts(id, effectiveTitle, messagesJSON string) []sqlite.Statement {
	var messages jsArr
	if v, err := jsParse(messagesJSON); err == nil {
		if arr, ok := v.(jsArr); ok {
			messages = arr
		}
	}
	field := func(key string) string {
		parts := make([]string, len(messages))
		for i, m := range messages {
			if v := jsOptGet(m, key); !isNullish(v) {
				parts[i] = jsToString(v)
			}
		}
		return strings.Join(parts, "\n")
	}
	return []sqlite.Statement{
		{SQL: "DELETE FROM ask_session_fts WHERE session_id=?", Args: []any{id}},
		{SQL: "INSERT INTO ask_session_fts(session_id,title,questions,answers) VALUES(?,?,?,?)",
			Args: []any{id, SegmentTextForFts(effectiveTitle), SegmentTextForFts(field("question")), SegmentTextForFts(field("answer"))}},
	}
}
func (l *Library) askSessionUpdateMeta(ctx context.Context, id string, patch any) (any, error) {
	l.writeMu.Lock()
	cur, err := l.askSessionGet(ctx, id)
	if err != nil {
		l.writeMu.Unlock()
		return nil, err
	}
	if cur == nil {
		l.writeMu.Unlock()
		return nil, errors.New("会话不存在")
	}
	var stmts []sqlite.Statement
	stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR IGNORE INTO ask_session_meta(id) VALUES(?)", Args: []any{id}})
	newTitle := ""
	titleSet := false
	if t := jsGet(patch, "title"); !jsIsUndefined(t) {
		s, ok := t.(string)
		if !ok {
			l.writeMu.Unlock()
			return nil, errors.New("patch.title.trim is not a function")
		}
		if jsTrim(s) == "" || utf16Len(s) > 200 {
			l.writeMu.Unlock()
			return nil, errors.New("会话标题应为 1–200 字")
		}
		newTitle, titleSet = jsTrim(s), true
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE ask_session_meta SET custom_title=? WHERE id=?", Args: []any{newTitle, id}})
	}
	if p := jsGet(patch, "pinned"); !jsIsUndefined(p) {
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE ask_session_meta SET pinned=? WHERE id=?", Args: []any{b2i(vTruthy(p)), id}})
	}
	// 重建索引：标题用（新的自定义标题 > 旧的自定义标题 > 会话标题），消息取库里现有的
	row, err := l.db.Query(ctx, "SELECT title, messages_json FROM ask_sessions WHERE id=?", id)
	if err != nil {
		l.writeMu.Unlock()
		return nil, err
	}
	sessionTitle, _ := row.Values[0][0].(string)
	messagesJSON, _ := row.Values[0][1].(string)
	effective := sessionTitle
	if old, ok := cur.(map[string]any)["title"].(string); ok {
		effective = old
	}
	if titleSet {
		effective = newTitle
	}
	stmts = append(stmts, historyStmts(id, effective, messagesJSON)...)

	_, err = l.db.Transaction(ctx, stmts)
	l.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	return l.askSessionGet(ctx, id)
}

func jsIsUndefined(v any) bool { _, ok := v.(jsUndefinedT); return ok }

func (l *Library) askSessionDelete(ctx context.Context, id string) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	res, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "DELETE FROM ask_sessions WHERE id = ?", Args: []any{id}}})
	if err != nil {
		return nil, err
	}
	return res[0].Changes > 0, nil
}

func (l *Library) registerAskSessions(b *Bridge) {
	// 参数先解析成 JS 值；解析失败（不会发生：参数来自 JSON.stringify）按 undefined 处理
	arg := func(a []json.RawMessage, i int) any {
		raw := argAt(a, i)
		if len(raw) == 0 {
			return jsUndefined
		}
		v, err := jsParse(string(raw))
		if err != nil {
			return jsUndefined
		}
		return v
	}
	idOf := func(v any) string { s, _ := v.(string); return s }
	b.handlers["askSession:query"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		in := arg(a, 0)
		if isNullish(in) {
			in = newObj() // input ?? {}
		}
		return l.askSessionQuery(ctx, in)
	}
	b.handlers["askSession:meta"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		patch := arg(a, 1)
		if isNullish(patch) {
			patch = newObj()
		}
		return l.askSessionUpdateMeta(ctx, idOf(arg(a, 0)), patch)
	}
	b.handlers["ask:listSessions"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		filter := newObj()
		spreadInto(filter, arg(a, 0)) // {...filter}
		filter.set("limit", float64(100))
		res, err := l.askSessionQuery(ctx, filter)
		if err != nil {
			return nil, err
		}
		return res.(map[string]any)["entries"], nil
	}
	b.handlers["ask:getSession"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.askSessionGet(ctx, idOf(arg(a, 0)))
	}
	b.handlers["ask:saveSession"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.askSessionSave(ctx, arg(a, 0))
	}
	b.handlers["ask:deleteSession"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.askSessionDelete(ctx, idOf(arg(a, 0)))
	}
}

var _ = fmt.Sprintf
