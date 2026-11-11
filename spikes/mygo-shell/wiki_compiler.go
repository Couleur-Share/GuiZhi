package main

// 对应 ipc/wiki-compiler.ipc.ts、packages/db/src/wiki-compiler.ts、wiki-material.ts、wiki-candidates.ts
// 与 wiki-publish.ts 里的建议令牌 / 采用建议：wiki:compiler 一个频道、七个 action。
//
// 模型调用由渲染进程里的串行驱动器完成，主进程只维护「预览 → 开始 → 取分块 → 回执 → 发布」的状态机。
//
// 与 TS 的有意差异：
//   - 预览不做 5 秒缓存（TS 缓存只是性能优化，命中时结果与重算一致）；
//   - Go 的 Transaction 是一批固定语句，publishCompleted 里「先写贡献再按贡献建页」拆成两个事务：
//     中途崩溃时条目仍为 published=0，下一次 next() 会幂等重放；
//   - 采用建议前的 flushWikiInvalidations 单独提交，不与采用建议同事务；
//   - 分块硬切落在 UTF-16 代理对中间时，Go 得到 U+FFFD（JS 保留孤立代理项）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/plugins/sqlite"
)

const chWikiCompiler = "wiki:compiler"

// ── 分块（wiki-material.ts） ──

type wikiMaterial struct {
	ID, Title, Content string
	Transcript         string // null 视为空串（TS: item.transcript ?? ''）
	ReviewStatus       string
	Deleted            bool
}

type wikiBlock struct {
	Key, Hash, Field, Section string
	Start, End                int
	Text                      string
}

func materialHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func jsonStr(vals ...any) string {
	s, _ := jsStringify(jsArr(vals))
	return s
}

func wikiFingerprint(m wikiMaterial) string {
	return materialHash(jsonStr(m.Title, m.Content, m.Transcript))
}

func u16str(u []uint16) string { return string(utf16.Decode(u)) }

func wikiLineTerm(c uint16) bool {
	return c == '\n' || c == '\r' || c == 0x2028 || c == 0x2029
}

// jsSpaceUnit 对应正则 \s（BMP 字符）。
func jsSpaceUnit(c uint16) bool { return jsSpace(rune(c)) }

// wikiHeaderMatch 对应 raw.match(/^#{1,6}\s+(.+)$/m)?.[1]：逐位置模拟 JS 的多行锚点、\s 与 . 的语义。
// 注意 \s+ 可以跨行（匹配到后面一行的标题文字），.+ 不含 \n \r U+2028 U+2029。
func wikiHeaderMatch(raw []uint16) (string, bool) {
	n := len(raw)
	for p := 0; p < n; p++ {
		if p > 0 && !wikiLineTerm(raw[p-1]) {
			continue
		}
		h := p
		for h < n && raw[h] == '#' {
			h++
		}
		if k := h - p; k < 1 || k > 6 {
			continue
		}
		e := h
		for e < n && jsSpaceUnit(raw[e]) {
			e++
		}
		// \s+ 从最长往回退，.+ 取到行尾（至少一个非行终止符）
		for j := e - h; j >= 1; j-- {
			pos := h + j
			if pos < n && !wikiLineTerm(raw[pos]) {
				end := pos
				for end < n && !wikiLineTerm(raw[end]) {
					end++
				}
				return u16str(raw[pos:end]), true
			}
		}
	}
	return "", false
}

// wikiFenceLine 对应 /^ {0,3}(`{3,}|~{3,})([^\n]*)$/（行内不含 \n，$ 即行尾）。
func wikiFenceLine(line []uint16) (run, rest []uint16, ok bool) {
	s := 0
	for s < len(line) && s < 4 && line[s] == ' ' {
		s++
	}
	if s > 3 || s >= len(line) || (line[s] != '`' && line[s] != '~') {
		return nil, nil, false
	}
	e := s
	for e < len(line) && line[e] == line[s] {
		e++
	}
	if e-s < 3 {
		return nil, nil, false
	}
	return line[s:e], line[e:], true
}

func fenceMarker(open []uint16) []uint16 {
	i := 0
	for i < len(open) && (open[i] == '`' || open[i] == '~') {
		i++
	}
	return open[:i]
}

func headU16(u []uint16, n int) []uint16 {
	if len(u) > n {
		return u[:n]
	}
	return u
}

func lastIndexU16(u []uint16, c uint16, from int) int {
	if from >= len(u) {
		from = len(u) - 1
	}
	for i := from; i >= 0; i-- {
		if u[i] == c {
			return i
		}
	}
	return -1
}

// wikiBlocks 对应 wikiBlocks：段落独立寻址，偏移用 UTF-16 码元（与 JS 字符串下标一致）。
func wikiBlocks(item wikiMaterial) []wikiBlock {
	var blocks []wikiBlock
	occurrences := map[string]int{}
	for _, field := range []string{"content", "transcript"} {
		src := item.Content
		if field == "transcript" {
			src = item.Transcript
		}
		text := utf16.Encode([]rune(strings.ReplaceAll(src, "\r\n", "\n")))
		section := ""
		var openFence []uint16
		n := len(text)
		for i := 0; i < n; {
			if text[i] == '\n' {
				i++
				continue
			}
			// 段落 = 连续的非空行：/[^\n]+(?:\n(?!\n)[^\n]+)*/g
			start, j := i, i
			for {
				for j < n && text[j] != '\n' {
					j++
				}
				if j+1 < n && text[j] == '\n' && text[j+1] != '\n' {
					j++
					continue
				}
				break
			}
			paragraph := text[start:j]
			i = j
			for offset := 0; offset < len(paragraph); {
				length := min(2600, len(paragraph)-offset)
				// 优先在完整行边界断开，长单行才按字符切；避免切碎围栏起止标记
				if offset+length < len(paragraph) {
					if nl := lastIndexU16(paragraph, '\n', offset+length-1); nl > offset {
						length = nl - offset + 1
					}
				}
				raw := paragraph[offset : offset+length]
				if len(openFence) == 0 {
					if h, ok := wikiHeaderMatch(raw); ok {
						section = h
					}
				}
				var prefix []uint16
				if len(openFence) > 0 {
					prefix = append(append([]uint16{}, openFence...), '\n')
				}
				lineStart := 0
				for k := 0; k <= len(raw); k++ {
					if k < len(raw) && raw[k] != '\n' {
						continue
					}
					if run, rest, ok := wikiFenceLine(raw[lineStart:k]); ok {
						marker := fenceMarker(openFence)
						if len(marker) == 0 {
							openFence = append(append([]uint16{}, headU16(run, 128)...), headU16(rest, 64)...)
						} else if run[0] == marker[0] && len(run) >= len(marker) && jsTrim(u16str(rest)) == "" {
							openFence = nil
						}
					}
					lineStart = k + 1
				}
				// 合成围栏仅保障输入语法，不计入来源覆盖偏移
				var suffix []uint16
				if len(openFence) > 0 {
					suffix = append([]uint16{'\n'}, fenceMarker(openFence)...)
				}
				if len(prefix)+len(raw)+len(suffix) > 3000 {
					prefix = headU16(prefix, 100)
				}
				body := u16str(append(append(append([]uint16{}, prefix...), raw...), suffix...))
				hash := materialHash(jsonStr(item.Title, field, section, body))
				occ := occurrences[hash]
				occurrences[hash] = occ + 1
				blocks = append(blocks, wikiBlock{Key: fmt.Sprintf("%s:%d", hash, occ), Hash: hash, Field: field, Section: section,
					Start: start + offset, End: start + offset + len(raw), Text: body})
				offset += len(raw)
			}
		}
	}
	if len(blocks) == 0 && jsTrim(item.Title) != "" {
		title := utf16.Encode([]rune(item.Title))
		for start := 0; start < len(title); start += 3000 {
			chunk := title[start:min(start+3000, len(title))]
			text := u16str(chunk)
			hash := materialHash(text)
			blocks = append(blocks, wikiBlock{Key: fmt.Sprintf("%s:%d", hash, start), Hash: hash, Field: "title",
				Start: start, End: start + len(chunk), Text: text})
		}
	}
	return blocks
}

func (b wikiBlock) toJS() *jsObj {
	o := newObj()
	o.set("key", b.Key)
	o.set("hash", b.Hash)
	o.set("field", b.Field)
	o.set("section", b.Section)
	o.set("start", float64(b.Start))
	o.set("end", float64(b.End))
	o.set("text", b.Text)
	return o
}

func (b wikiBlock) json() string {
	s, _ := jsStringify(b.toJS())
	return s
}

// ── 读取材料 ──

func wikiMaterialFromRow(r []any) wikiMaterial {
	s := func(v any) string { x, _ := v.(string); return x }
	return wikiMaterial{ID: s(r[0]), Title: s(r[1]), Content: s(r[2]), Transcript: s(r[3]), ReviewStatus: s(r[4]), Deleted: r[5] != nil}
}

func (l *Library) wikiMaterial(ctx context.Context, id any) (*wikiMaterial, error) {
	bound, err := wikiBind(id)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.Query(ctx, "SELECT id,title,content,transcript,review_status,deleted_at FROM knowledge_items WHERE id=?", bound)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	m := wikiMaterialFromRow(rows.Values[0])
	return &m, nil
}

// ── 预览（wiki-candidates.ts + preview） ──

type wikiCandidate struct {
	ID, Title, Fingerprint, State, Reason string
	Blocks, Reusable                      int
}

func (c wikiCandidate) toMap() map[string]any {
	m := map[string]any{"id": c.ID, "title": c.Title, "fingerprint": c.Fingerprint, "state": c.State, "blocks": c.Blocks, "reusable": c.Reusable}
	if c.Reason != "" {
		m["reason"] = c.Reason
	}
	return m
}

type wikiPreviewResult struct {
	Entries  []wikiCandidate
	Counts   map[string]int
	Min, Max int
}

func (p wikiPreviewResult) toMap() map[string]any {
	entries := make([]any, 0, len(p.Entries))
	for _, e := range p.Entries {
		entries = append(entries, e.toMap())
	}
	counts := map[string]any{}
	for k, v := range p.Counts {
		counts[k] = v
	}
	return map[string]any{"entries": entries, "counts": counts, "requests": map[string]any{"min": p.Min, "max": p.Max}}
}

func wikiPlaceholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// wikiPreview ids 为 nil 表示全库（未删除，按更新时间倒序）。
func (l *Library) wikiPreview(ctx context.Context, model any, ids []string) (*wikiPreviewResult, error) {
	modelStr, _ := model.(string)
	modelTruthy := vTruthy(model)
	if ids == nil {
		rows, err := l.db.Query(ctx, "SELECT id FROM knowledge_items WHERE deleted_at IS NULL ORDER BY updated_at DESC,id DESC")
		if err != nil {
			return nil, err
		}
		ids = []string{}
		for _, r := range rows.Values {
			ids = append(ids, r[0].(string))
		}
	}
	res := &wikiPreviewResult{Counts: map[string]int{"ready": 0, "review": 0, "waiting": 0, "upgrade": 0, "current": 0, "excluded": 0}}
	now := nowMs()
	for offset := 0; offset < len(ids); offset += 500 {
		batch := ids[offset:min(offset+500, len(ids))]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		ph := wikiPlaceholders(len(batch))
		rows, err := l.db.Query(ctx, `SELECT i.id,i.title,i.content,i.transcript,i.review_status,i.deleted_at,
      w.item_id AS ingested_id,w.content_hash,w.prompt_version,w.model,w.failure_count,w.next_attempt_at
      FROM knowledge_items i LEFT JOIN wiki_ingestions w ON w.item_id=i.id WHERE i.id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		byID := map[string][]any{}
		for _, r := range rows.Values {
			byID[r[0].(string)] = r
		}
		un, err := l.db.Query(ctx, `SELECT i.item_id,i.fingerprint FROM wiki_compile_items i JOIN wiki_compile_jobs j ON j.id=i.job_id
      WHERE i.item_id IN (`+ph+`) AND i.published=0 AND j.status IN ('running','paused','cancelled','interrupted')`, args...)
		if err != nil {
			return nil, err
		}
		unfinished := map[string]bool{}
		for _, r := range un.Values {
			unfinished[r[0].(string)+":"+r[1].(string)] = true
		}
		mb, err := wikiBindN(model, "")
		if err != nil {
			return nil, err
		}
		cargs := append([]any{mb[0]}, args...)
		cr, err := l.db.Query(ctx, `SELECT item_id,block_hash FROM wiki_block_cache WHERE model=? AND item_id IN (`+ph+`)`, cargs...)
		if err != nil {
			return nil, err
		}
		cached := map[string]map[string]bool{}
		for _, r := range cr.Values {
			id := r[0].(string)
			if cached[id] == nil {
				cached[id] = map[string]bool{}
			}
			cached[id][r[1].(string)] = true
		}
		for _, id := range batch {
			row, ok := byID[id]
			if !ok {
				continue
			}
			item := wikiMaterialFromRow(row)
			fingerprint := wikiFingerprint(item)
			blocks := wikiBlocks(item)
			isUnfinished := unfinished[id+":"+fingerprint]
			hasOld := row[6] != nil
			oldPrompt, _ := row[8].(string)
			oldModel, _ := row[9].(string)
			oldHash, _ := row[7].(string)
			var failures, nextAttempt int64
			if row[10] != nil {
				failures = asInt(row[10])
			}
			if row[11] != nil {
				nextAttempt = asInt(row[11])
			}
			var state string
			switch {
			case item.Deleted || len(blocks) == 0:
				state = "excluded"
			case item.ReviewStatus == "needs_review":
				state = "review"
			case !modelTruthy || isUnfinished:
				state = "waiting"
			case hasOld && oldPrompt != "" && oldPrompt != wikiCompilerVer:
				state = "upgrade"
			case hasOld && oldHash == fingerprint && oldModel == modelStr && oldPrompt == wikiCompilerVer:
				state = "current"
			case hasOld && oldHash == fingerprint && (failures >= 3 || nextAttempt > now):
				state = "waiting"
			default:
				state = "ready"
			}
			reusable := 0
			for _, b := range blocks {
				if cached[id][b.Hash] {
					reusable++
				}
			}
			res.Counts[state]++
			e := wikiCandidate{ID: id, Title: item.Title, Fingerprint: fingerprint, State: state, Blocks: len(blocks), Reusable: reusable}
			switch state {
			case "review":
				e.Reason = "需先完成复核"
			case "waiting":
				e.Reason = "等待退避重试或检查模型配置"
			case "upgrade":
				e.Reason = "历史覆盖需选择后升级"
			}
			res.Entries = append(res.Entries, e)
		}
	}
	requests := 0
	for _, e := range res.Entries {
		if e.State == "ready" || e.State == "upgrade" {
			requests += e.Blocks - e.Reusable
		}
	}
	res.Min, res.Max = requests, requests*2
	return res, nil
}

// ── 任务读取 ──

func (l *Library) wikiJobGet(ctx context.Context, id any) (map[string]any, error) {
	bound, err := wikiBind(id)
	if err != nil {
		return nil, err
	}
	rows, err := l.db.Query(ctx, "SELECT id,model,status,error,created_at FROM wiki_compile_jobs WHERE id=?", bound)
	if err != nil || len(rows.Values) == 0 {
		return nil, err
	}
	r := rows.Values[0]
	cr, err := l.db.Query(ctx, "SELECT COUNT(*) AS total,SUM(status='done') AS completed,SUM(status='failed') AS failed FROM wiki_compile_blocks WHERE job_id=?", bound)
	if err != nil {
		return nil, err
	}
	c := cr.Values[0]
	job := map[string]any{"id": r[0], "model": r[1], "status": r[2], "createdAt": asInt(r[4]),
		"total": asInt(c[0]), "completed": int64(0), "failed": int64(0)}
	if c[1] != nil {
		job["completed"] = asInt(c[1])
	}
	if c[2] != nil {
		job["failed"] = asInt(c[2])
	}
	if s, ok := r[3].(string); ok {
		job["error"] = s
	}
	return job, nil
}

func (l *Library) wikiJobList(ctx context.Context) ([]any, error) {
	rows, err := l.db.Query(ctx, "SELECT id FROM wiki_compile_jobs ORDER BY created_at DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, r := range rows.Values {
		job, err := l.wikiJobGet(ctx, r[0])
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

func (l *Library) wikiInterrupt(ctx context.Context) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	_, err := l.db.Transaction(ctx, []sqlite.Statement{
		{SQL: "UPDATE wiki_compile_jobs SET status='interrupted',error='上次编译被中断，可继续未完成的分块' WHERE status='running'"},
		{SQL: "UPDATE wiki_compile_blocks SET status='pending' WHERE status='running'"},
	})
	return err
}

// ── 开始 ──

func (l *Library) wikiStart(ctx context.Context, model any, selected any, allowUpgrade bool) (map[string]any, error) {
	selLen, _ := jsLength(selected) // 无 length 时 TS 里是 undefined：!undefined 为真
	if !vTruthy(model) || selLen == 0 || selLen > 1000 {
		return map[string]any{"ok": false, "error": "请选择 1–1000 条资料及有效模型"}, nil
	}
	running, err := l.db.Query(ctx, "SELECT id FROM wiki_compile_jobs WHERE status='running' LIMIT 1")
	if err != nil {
		return nil, err
	}
	if len(running.Values) > 0 {
		return map[string]any{"ok": false, "error": "已有 Wiki 编译正在执行"}, nil
	}
	sel, isArr := selected.(jsArr)
	if !isArr {
		return nil, errors.New("selected.map is not a function")
	}
	// selected.map(s => s.id)：元素为 null 时 TS 抛 TypeError
	type pick struct{ id, fingerprint any }
	picks := make([]pick, len(sel))
	for i, s := range sel {
		if isNullish(s) {
			return nil, fmt.Errorf("Cannot read properties of %s (reading 'id')", jsToString(s))
		}
		picks[i] = pick{jsGet(s, "id"), jsGet(s, "fingerprint")}
	}
	var ids []string
	seen := map[string]bool{}
	for _, p := range picks {
		if s, ok := p.id.(string); ok && !seen[s] {
			seen[s] = true
			ids = append(ids, s)
		}
	}
	if ids == nil {
		ids = []string{}
	}
	preview, err := l.wikiPreview(ctx, model, ids)
	if err != nil {
		return nil, err
	}
	bad := len(preview.Entries) != len(picks)
	for _, e := range preview.Entries {
		if bad {
			break
		}
		var fp any = jsUndefined
		for _, p := range picks {
			if p.id == e.ID {
				fp = p.fingerprint
				break
			}
		}
		if fp != e.Fingerprint || !(e.State == "ready" || (allowUpgrade && e.State == "upgrade")) {
			bad = true
		}
	}
	if bad {
		return map[string]any{"ok": false, "preview": preview.toMap(), "error": "资料或处理范围已经变化，请核对预览后再开始"}, nil
	}
	modelStr, _ := model.(string)
	jobID := newUUID()
	stmts := []sqlite.Statement{{SQL: "INSERT INTO wiki_compile_jobs(id,model,status,created_at) VALUES(?,?,'running',?)", Args: []any{jobID, modelStr, nowMs()}}}
	for _, e := range preview.Entries {
		stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO wiki_compile_items(job_id,item_id,title,fingerprint) VALUES(?,?,?,?)", Args: []any{jobID, e.ID, e.Title, e.Fingerprint}})
		m, err := l.wikiMaterial(ctx, e.ID)
		if err != nil {
			return nil, err
		}
		if m == nil {
			return nil, errors.New("Cannot read properties of undefined (reading 'id')")
		}
		for _, b := range wikiBlocks(*m) {
			cr, err := l.db.Query(ctx, "SELECT contributions_json FROM wiki_block_cache WHERE item_id=? AND block_hash=? AND model=?", e.ID, b.Hash, modelStr)
			if err != nil {
				return nil, err
			}
			status, contrib := "pending", any(nil)
			if len(cr.Values) > 0 {
				status, contrib = "done", cr.Values[0][0]
			}
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO wiki_compile_blocks(job_id,item_id,block_key,block_json,status,contributions_json) VALUES(?,?,?,?,?,?)",
				Args: []any{jobID, e.ID, b.Key, b.json(), status, contrib}})
		}
	}
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	job, err := l.wikiJobGet(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "job": job}, nil
}

// ── 控制 / 取分块 / 回执 ──

func (l *Library) wikiControl(ctx context.Context, id, status any) (map[string]any, error) {
	job, err := l.wikiJobGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if job == nil || job["status"] == "completed" {
		return nil, errors.New("任务不存在或已经完成")
	}
	bound, err := wikiBindN(status, id) // 与 TS 一致：多参数绑定里的 undefined 会抛错
	if err != nil {
		return nil, err
	}
	bstatus, bid := bound[0], bound[1]
	var stmts []sqlite.Statement
	if status == "running" {
		other, err := l.db.Query(ctx, "SELECT id FROM wiki_compile_jobs WHERE status='running' AND id!=?", bid)
		if err != nil {
			return nil, err
		}
		if len(other.Values) > 0 {
			return nil, errors.New("已有其他编译正在执行")
		}
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE wiki_compile_blocks SET status='pending',error=NULL WHERE job_id=? AND status IN ('failed','running')", Args: []any{bid}})
	}
	stmts = append(stmts, sqlite.Statement{SQL: "UPDATE wiki_compile_jobs SET status=?,error=NULL WHERE id=?", Args: []any{bstatus, bid}})
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return nil, err
	}
	return l.wikiJobGet(ctx, id)
}

func (l *Library) wikiFailJob(ctx context.Context, id string, msg string) error {
	_, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "UPDATE wiki_compile_jobs SET status='failed',error=? WHERE id=?", Args: []any{msg, id}}})
	return err
}

func (l *Library) wikiNext(ctx context.Context, id any) (map[string]any, error) {
	job, err := l.wikiJobGet(ctx, id)
	if err != nil || job == nil || job["status"] != "running" {
		return nil, err
	}
	jobID := id.(string)
	run, err := l.db.Query(ctx, "SELECT 1 FROM wiki_compile_blocks WHERE job_id=? AND status='running'", jobID)
	if err != nil {
		return nil, err
	}
	if len(run.Values) > 0 {
		return nil, errors.New("当前分块尚未返回")
	}
	if err := l.wikiPublishCompleted(ctx, jobID); err != nil {
		return nil, err
	}
	again, err := l.wikiJobGet(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if again == nil || again["status"] != "running" {
		return nil, nil
	}
	rows, err := l.db.Query(ctx, `SELECT b.item_id,b.block_json,i.title,i.fingerprint FROM wiki_compile_blocks b JOIN wiki_compile_items i ON i.job_id=b.job_id AND i.item_id=b.item_id
      WHERE b.job_id=? AND b.status='pending' ORDER BY i.rowid,b.rowid LIMIT 1`, jobID)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		failed := again["failed"].(int64)
		status := "completed"
		if failed > 0 {
			status = "failed"
		}
		_, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "UPDATE wiki_compile_jobs SET status=? WHERE id=?", Args: []any{status, jobID}}})
		return nil, err
	}
	r := rows.Values[0]
	itemID, blockJSON, title, fingerprint := r[0].(string), r[1].(string), r[2].(string), r[3].(string)
	blockVal, err := jsParse(blockJSON)
	if err != nil {
		return nil, errors.New("Unexpected token in JSON: " + err.Error())
	}
	m, err := l.wikiMaterial(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.Deleted || m.ReviewStatus == "needs_review" || wikiFingerprint(*m) != fingerprint {
		return nil, l.wikiFailJob(ctx, jobID, "来源已改变，请重新预览处理范围")
	}
	key := jsGet(blockVal, "key")
	if _, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "UPDATE wiki_compile_blocks SET status='running' WHERE job_id=? AND item_id=? AND block_key=?", Args: []any{jobID, itemID, key}}}); err != nil {
		return nil, err
	}
	return map[string]any{"jobId": jobID, "itemId": itemID, "title": title, "fingerprint": fingerprint, "block": jsToPlain(blockVal)}, nil
}

// wikiBindN 复刻多参数绑定（适配器把多个参数合成数组逐个绑定）：undefined 与非 null 对象会抛错，
// 与单参数时 undefined 视为未绑定不同。
func wikiBindN(vals ...any) ([]any, error) {
	out := make([]any, len(vals))
	for i, v := range vals {
		switch x := v.(type) {
		case string:
			out[i] = x
		case float64, bool:
			b, _ := wikiBind(x)
			out[i] = b
		case jsNullT, nil:
			out[i] = nil
		case jsUndefinedT:
			return nil, errors.New("Unsupported type for binding: \"undefined\"")
		default:
			return nil, errors.New("Unsupported type for binding: \"object\"")
		}
	}
	return out, nil
}

// wikiGetStrict 对应 v.key：v 为 null/undefined 时抛 Node 风格的 TypeError。
func wikiGetStrict(v any, key string) (any, error) {
	if isNullish(v) {
		return nil, fmt.Errorf("Cannot read properties of %s (reading '%s')", jsToString(v), key)
	}
	return jsGet(v, key), nil
}

func jsLength(v any) (float64, bool) {
	switch x := v.(type) {
	case string:
		return float64(utf16Len(x)), true
	case jsArr:
		return float64(len(x)), true
	case *jsObj:
		n, ok := x.get("length").(float64)
		return n, ok
	}
	return 0, false
}

// wikiValidateContributions 对应 finish 里 contributions 的格式校验（短路求值与 TypeError 文案保持一致）。
func wikiValidateContributions(c any) error {
	invalid := errors.New("分块贡献格式无效")
	if n, ok := jsLength(c); ok && n > 4 {
		return invalid
	}
	arr, ok := c.(jsArr)
	if !ok {
		return errors.New("contributions.some is not a function")
	}
	for _, p := range arr {
		title, err := wikiGetStrict(p, "title")
		if err != nil {
			return err
		}
		if !vTruthy(title) {
			return invalid
		}
		body, _ := wikiGetStrict(p, "body")
		if !vTruthy(body) {
			return invalid
		}
		if n, ok := jsLength(body); ok && n > 16000 {
			return invalid
		}
		switch jsGet(p, "kind") {
		case "topic", "entity", "concept":
		default:
			return invalid
		}
	}
	return nil
}

func (l *Library) wikiFinish(ctx context.Context, work, contributions, errVal any) error {
	jobIDv, err := wikiGetStrict(work, "jobId")
	if err != nil {
		return err
	}
	job, err := l.wikiJobGet(ctx, jobIDv)
	if err != nil {
		return err
	}
	if job == nil {
		return nil
	}
	switch job["status"] {
	case "cancelled", "interrupted", "completed":
		return nil
	}
	itemID := jsGet(work, "itemId")
	blockVal, err := wikiGetStrict(jsGet(work, "block"), "key")
	if err != nil {
		return err
	}
	sel, err := wikiBindN(jobIDv, itemID, blockVal)
	if err != nil {
		return err
	}
	bj, bi := sel[0], sel[1]
	rows, err := l.db.Query(ctx, "SELECT status,block_json FROM wiki_compile_blocks WHERE job_id=? AND item_id=? AND block_key=?", sel...)
	if err != nil {
		return err
	}
	if len(rows.Values) == 0 || rows.Values[0][0] != "running" {
		return errors.New("分块回执不匹配")
	}
	block, err := jsParse(rows.Values[0][1].(string))
	if err != nil {
		return errors.New("Unexpected token in JSON: " + err.Error())
	}
	if vTruthy(contributions) {
		if err := wikiValidateContributions(contributions); err != nil {
			return err
		}
	}
	failed := vTruthy(errVal)
	var contribJSON any
	if !failed {
		var c any = jsArr{}
		if !isNullish(contributions) {
			c = contributions
		}
		s, _ := jsStringify(c)
		contribJSON = s
	}
	var errCol any
	if !isNullish(errVal) {
		if b, err := wikiBindN(errVal, ""); err != nil {
			return err
		} else {
			errCol = b[0]
		}
	}
	status := "done"
	if failed {
		status = "failed"
	}
	blockKey, blockHash := jsGet(block, "key"), jsGet(block, "hash")
	stmts := []sqlite.Statement{{SQL: "UPDATE wiki_compile_blocks SET status=?,contributions_json=?,error=?,attempts=attempts+1 WHERE job_id=? AND item_id=? AND block_key=?",
		Args: []any{status, contribJSON, errCol, bj, bi, blockKey}}}
	if !failed {
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE wiki_compile_blocks SET status='done',contributions_json=?,error=NULL WHERE job_id=? AND item_id=? AND json_extract(block_json,'$.hash')=?",
			Args: []any{contribJSON, bj, bi, blockHash}})
		if m, err := l.wikiMaterial(ctx, itemID); err != nil {
			return err
		} else if m != nil {
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR REPLACE INTO wiki_block_cache(item_id,block_hash,model,contributions_json) VALUES(?,?,?,?)",
				Args: []any{bi, blockHash, job["model"], contribJSON}})
		}
	} else {
		fpv, err := wikiBindN(jsGet(work, "fingerprint"), "")
		if err != nil {
			return err
		}
		fp := fpv[0]
		now := nowMs()
		stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,failure_count,next_attempt_at,updated_at) VALUES(?,?,?,'',1,?,?)
        ON CONFLICT(item_id) DO UPDATE SET content_hash=excluded.content_hash, failure_count=failure_count+1,next_attempt_at=excluded.next_attempt_at`,
			Args: []any{bi, fp, job["model"], now + 30*60_000, now}})
	}
	_, err = l.db.Transaction(ctx, stmts)
	return err
}

// wikiPublishCompleted 把已全部完成的条目写入贡献并建页（见文件头说明：拆成两个事务）。
func (l *Library) wikiPublishCompleted(ctx context.Context, id string) error {
	job, err := l.wikiJobGet(ctx, id)
	if err != nil || job == nil {
		return err
	}
	model, _ := job["model"].(string)
	rows, err := l.db.Query(ctx, `SELECT item_id,fingerprint FROM wiki_compile_items i WHERE job_id=? AND published=0
      AND NOT EXISTS(SELECT 1 FROM wiki_compile_blocks b WHERE b.job_id=i.job_id AND b.item_id=i.item_id AND b.status!='done')`, id)
	if err != nil {
		return err
	}
	for _, row := range rows.Values {
		itemID, fingerprint := row[0].(string), row[1].(string)
		m, err := l.wikiMaterial(ctx, itemID)
		if err != nil {
			return err
		}
		if m == nil || m.Deleted || m.ReviewStatus == "needs_review" || wikiFingerprint(*m) != fingerprint {
			return l.wikiFailJob(ctx, id, "来源已改变，结果尚未发布")
		}
		affected := []string{}
		add := func(t string) {
			for _, e := range affected {
				if e == t {
					return
				}
			}
			affected = append(affected, t)
		}
		ar, err := l.db.Query(ctx, "SELECT DISTINCT page_title FROM wiki_contributions WHERE item_id=?", itemID)
		if err != nil {
			return err
		}
		for _, r := range ar.Values {
			add(r[0].(string))
		}
		sr, err := l.db.Query(ctx, "SELECT p.normalized_title FROM wiki_page_sources s JOIN wiki_pages p ON p.id=s.page_id WHERE s.item_id=?", itemID)
		if err != nil {
			return err
		}
		for _, r := range sr.Values {
			add(r[0].(string))
		}
		stmts := []sqlite.Statement{{SQL: "DELETE FROM wiki_contributions WHERE item_id=?", Args: []any{itemID}}}
		br, err := l.db.Query(ctx, "SELECT block_key,contributions_json FROM wiki_compile_blocks WHERE job_id=? AND item_id=?", id, itemID)
		if err != nil {
			return err
		}
		for _, b := range br.Values {
			cj, _ := b[1].(string)
			parsed, err := jsParse(cj)
			if err != nil {
				return errors.New("Unexpected token in JSON: " + err.Error())
			}
			pages, ok := parsed.(jsArr)
			if !ok {
				return errors.New("JSON.parse(...) is not iterable")
			}
			for _, page := range pages {
				nt, err := wikiGetStrict(page, "normalizedTitle")
				if err != nil {
					return err
				}
				if s, ok := nt.(string); ok {
					add(s)
				}
				bound, err := wikiBind(nt)
				if err != nil {
					return err
				}
				pageJSON, _ := jsStringify(page)
				stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR REPLACE INTO wiki_contributions(item_id,block_key,page_title,contribution_json) VALUES(?,?,?,?)",
					Args: []any{itemID, b[0], bound, pageJSON}})
			}
		}
		now := nowMs()
		stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO wiki_ingestions(item_id,content_hash,model,prompt_version,failure_count,next_attempt_at,updated_at) VALUES(?,?,?,?,0,NULL,?)
          ON CONFLICT(item_id) DO UPDATE SET content_hash=excluded.content_hash,model=excluded.model,prompt_version=excluded.prompt_version,failure_count=0,next_attempt_at=NULL,updated_at=excluded.updated_at`,
			Args: []any{itemID, fingerprint, model, wikiCompilerVer, now}})
		if _, err := l.db.Transaction(ctx, stmts); err != nil {
			return err
		}
		pub, err := l.publishWikiPages(ctx, affected, model)
		if err != nil {
			return err
		}
		pub = append(pub, sqlite.Statement{SQL: "UPDATE wiki_compile_items SET published=1 WHERE job_id=? AND item_id=?", Args: []any{id, itemID}})
		if _, err := l.db.Transaction(ctx, pub); err != nil {
			return err
		}
	}
	return nil
}

// ── 更新建议（wiki-publish.ts） ──

// wikiSuggestionToken 对应 wikiSuggestionToken：建议内容 + 页面当前快照的哈希，页面变化后令牌失效。
func (l *Library) wikiSuggestionToken(ctx context.Context, pageID any, draftJSON string) (string, error) {
	bound, err := wikiBind(pageID)
	if err != nil {
		return "", err
	}
	rows, err := l.db.Query(ctx, "SELECT title,summary,body,aliases_json,kind,updated_at FROM wiki_pages WHERE id=?", bound)
	if err != nil {
		return "", err
	}
	var page any = jsUndefined
	if len(rows.Values) > 0 {
		r := rows.Values[0]
		o := newObj()
		for i, k := range []string{"title", "summary", "body", "aliases_json", "kind", "updated_at"} {
			switch v := r[i].(type) {
			case nil:
				o.set(k, jsNull)
			case int64:
				o.set(k, float64(v))
			default:
				o.set(k, v)
			}
		}
		page = o
	}
	return materialHash(jsonStr(draftJSON, page)), nil
}

func (l *Library) wikiAcceptSuggestion(ctx context.Context, pageID, token any) (bool, error) {
	if err := l.flushWikiInvalidations(ctx); err != nil {
		return false, err
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	bound, err := wikiBind(pageID)
	if err != nil {
		return false, err
	}
	rows, err := l.db.Query(ctx, "SELECT draft_json FROM wiki_page_suggestions WHERE page_id=?", bound)
	if err != nil || len(rows.Values) == 0 {
		return false, err
	}
	draftJSON := rows.Values[0][0].(string)
	if token != jsUndefined {
		want, err := l.wikiSuggestionToken(ctx, pageID, draftJSON)
		if err != nil {
			return false, err
		}
		if token != want {
			return false, nil
		}
	}
	value, err := jsParse(draftJSON)
	if err != nil {
		return false, errors.New("Unexpected token in JSON: " + err.Error())
	}
	draft, model := jsGet(value, "draft"), jsGet(value, "model")
	id := pageID.(string)
	now := nowMs()
	bind := func(v any) any { b, _ := wikiBind(v); return b }
	title, summary := jsGet(draft, "title"), jsGet(draft, "summary")
	body := jsToString(jsGet(draft, "body"))
	stmts := append([]sqlite.Statement{}, wikiSaveRevisionStmts(id, now)...)
	stmts = append(stmts,
		sqlite.Statement{SQL: "UPDATE wiki_pages SET title=?,kind=?,summary=?,body=?,aliases_json=?,model=?,prompt_version=?,generated_at=?,updated_at=? WHERE id=?",
			Args: []any{bind(title), bind(jsGet(draft, "kind")), bind(summary), body, bind(jsGet(draft, "aliasesJson")), bind(model), wikiCompilerVer, now, now, id}},
		sqlite.Statement{SQL: "UPDATE wiki_pages SET manual_edited_at=? WHERE id=?", Args: []any{now, id}})
	stmts = append(stmts, wikiFtsStmts(id, jsToString(title), jsToString(summary), body)...)
	stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM wiki_page_links WHERE from_page_id=?", Args: []any{id}})
	targets, ok := jsGet(draft, "linkTargets").(jsArr)
	if !ok {
		return false, errors.New("draft.linkTargets is not iterable")
	}
	for _, t := range targets {
		tr, err := l.db.Query(ctx, "SELECT id FROM wiki_pages WHERE normalized_title=?", bind(t))
		if err != nil {
			return false, err
		}
		if len(tr.Values) > 0 && tr.Values[0][0] != id {
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR IGNORE INTO wiki_page_links(from_page_id,to_page_id,created_at) VALUES(?,?,?)", Args: []any{id, tr.Values[0][0], now}})
		}
	}
	stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM wiki_page_sources WHERE page_id=?", Args: []any{id}})
	if sources, ok := jsGet(value, "sourceIds").(jsArr); ok {
		for _, sid := range sources {
			ir, err := l.db.Query(ctx, "SELECT id FROM knowledge_items WHERE id=? AND deleted_at IS NULL", bind(sid))
			if err != nil {
				return false, err
			}
			if len(ir.Values) > 0 {
				stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO wiki_page_sources(page_id,item_id,created_at) VALUES(?,?,?)", Args: []any{id, sid, nowMs()}})
			}
		}
	} else {
		return false, errors.New("value.sourceIds is not iterable")
	}
	stmts = append(stmts, sqlite.Statement{SQL: "DELETE FROM wiki_page_suggestions WHERE page_id=?", Args: []any{id}})
	if _, err := l.db.Transaction(ctx, stmts); err != nil {
		return false, err
	}
	return true, nil
}

// ── 入口 ──

func (l *Library) wikiCompiler(ctx context.Context, input any) (any, error) {
	act, err := wikiGetStrict(input, "action")
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	fail := func(err error) (any, error) { return map[string]any{"ok": false, "error": err.Error()}, nil }
	switch act {
	case "preview":
		var ids []string
		if raw := jsGet(input, "ids"); !isNullish(raw) {
			ids = []string{}
			if arr, ok := raw.(jsArr); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						ids = append(ids, s)
					}
				}
			} else if n, _ := jsLength(raw); n > 0 {
				// 非数组但有长度：字符串在 batch.map 处抛错，对象在 targets.slice 处抛错；其余等价于空列表
				if _, isStr := raw.(string); isStr {
					return fail(errors.New("batch.map is not a function"))
				}
				return fail(errors.New("targets.slice is not a function"))
			}
		}
		p, err := l.wikiPreview(ctx, jsGet(input, "model"), ids)
		if err != nil {
			return fail(err)
		}
		return map[string]any{"ok": true, "preview": p.toMap()}, nil
	case "start":
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		res, err := l.wikiStart(ctx, jsGet(input, "model"), jsGet(input, "selected"), jsGet(input, "allowUpgrade") == true)
		if err != nil {
			return fail(err)
		}
		return res, nil
	case "next":
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		id := jsGet(input, "id")
		work, err := l.wikiNext(ctx, id)
		if err != nil {
			return fail(err)
		}
		job, err := l.wikiJobGet(ctx, id)
		if err != nil {
			return fail(err)
		}
		return map[string]any{"ok": true, "work": work, "job": job}, nil
	case "finish":
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		if err := l.wikiFinish(ctx, jsGet(input, "work"), jsGet(input, "contributions"), jsGet(input, "error")); err != nil {
			return fail(err)
		}
		return map[string]any{"ok": true}, nil
	case "control":
		l.writeMu.Lock()
		defer l.writeMu.Unlock()
		job, err := l.wikiControl(ctx, jsGet(input, "id"), jsGet(input, "status"))
		if err != nil {
			return fail(err)
		}
		return map[string]any{"ok": true, "job": job}, nil
	case "jobs":
		jobs, err := l.wikiJobList(ctx)
		if err != nil {
			return fail(err)
		}
		return map[string]any{"ok": true, "jobs": jobs}, nil
	case "suggestion":
		pageID := jsGet(input, "pageId")
		if vTruthy(jsGet(input, "accept")) {
			token := jsGet(input, "token")
			ok := false
			if vTruthy(token) {
				if ok, err = l.wikiAcceptSuggestion(ctx, pageID, token); err != nil {
					return fail(err)
				}
			}
			return map[string]any{"ok": ok, "error": "更新建议或页面已变化，请重新核对"}, nil
		}
		if err := l.flushWikiInvalidations(ctx); err != nil {
			return fail(err)
		}
		bound, err := wikiBind(pageID)
		if err != nil {
			return fail(err)
		}
		rows, err := l.db.Query(ctx, "SELECT draft_json,reason FROM wiki_page_suggestions WHERE page_id=?", bound)
		if err != nil {
			return fail(err)
		}
		if len(rows.Values) == 0 {
			return map[string]any{"ok": true, "suggestion": nil}, nil
		}
		draftJSON, reason := rows.Values[0][0].(string), rows.Values[0][1]
		parsed, err := jsParse(draftJSON)
		if err != nil {
			return fail(errors.New("Unexpected token in JSON: " + err.Error()))
		}
		token, err := l.wikiSuggestionToken(ctx, pageID, draftJSON)
		if err != nil {
			return fail(err)
		}
		// {...JSON.parse(draft_json), reason, token}：展开对象后追加两个键
		out := map[string]any{}
		if o, ok := parsed.(*jsObj); ok {
			for _, k := range o.keys() {
				out[k] = jsToPlain(o.get(k))
			}
		}
		out["reason"], out["token"] = reason, token
		return map[string]any{"ok": true, "suggestion": out}, nil
	}
	return fail(errors.New("编译命令无效"))
}

func (l *Library) registerWikiCompiler(b *Bridge) {
	if l.db != nil { // 对应 new WikiCompilerDB(db).interrupt()：上次未结束的编译标记为中断
		if err := l.wikiInterrupt(context.Background()); err != nil {
			fmt.Printf("[wiki] 标记中断的编译失败: %v\n", err)
		}
	}
	b.handlers[chWikiCompiler] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.wikiCompiler(ctx, jsArg(a, 0))
	}
}
