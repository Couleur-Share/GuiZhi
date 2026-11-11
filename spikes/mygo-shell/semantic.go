package main

// 对应 ipc/semantic.ipc.ts、services/semantic.ts、semantic-search-backend.ts、semantic-vector-cache.ts
// 与 packages/db/src/semantic.ts：语义索引的状态、待索引批次、向量落库、余弦检索与清空。
//
// 与 TS 一致的细节：
//   - 向量以小端 Float32 BLOB 存储；打分用 float64 累加 float32 的乘积（JS 里也是 double）。
//   - 进程内向量缓存按模型缓存；写入时只失效「本次写入的模型」，清空时全部失效（包括 TS 的这个特点：
//     replaceItemChunks 会删掉条目在其他模型下的行，但其他模型的缓存不会失效）。
//   - 已知差异：同分并列时 TS 用 localeCompare 比 itemId，这里按字节比；条目 id 都是 uuid 时两者等价。
//   - 已知差异：requestId 取消不处理（Go 侧检索是同步的，取消点没有意义）。

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/sqlite"
)

const (
	semanticPendingBatchMax = 50
	semanticSearchLimitMax  = 20
	semanticSnippetMax      = 160
)

type semanticCache struct {
	dims        int
	vectors     []float32
	itemIDs     []string
	chunkIndexs []int64
}

type semanticState struct {
	mu          sync.Mutex
	caches      map[string]*semanticCache
	lastMs      any // nil 或 int64
	lastScanned any
	lastHit     any
}

func (s *semanticState) invalidate(model string, all bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if all {
		s.caches = nil
		return
	}
	delete(s.caches, model)
}

// nodeUTF8 对应 Buffer.from(str, "utf8")：孤立代理项变成一个 U+FFFD。
func nodeUTF8(s string) []byte {
	var out []byte
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			if _, ok := wtf8Surrogate(s[i:]); ok {
				out = append(out, 0xEF, 0xBF, 0xBD)
				i += 3
				continue
			}
		}
		out = append(out, s[i:i+size]...)
		i += size
	}
	return out
}

// computeContentHash 对应 services/import/content-hash.ts。
func computeContentHash(content string) string {
	s := strings.ReplaceAll(content, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	// 只有 \t \r \n 与空格是单字节，逐字节处理不会破坏多字节序列（包括孤立代理项的 WTF-8 字节）
	var b []byte
	prevBlank, prevNL := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\t', ' ':
			if !prevBlank {
				b = append(b, ' ')
			}
			prevBlank, prevNL = true, false
		case '\n':
			if !prevNL {
				b = append(b, '\n')
			}
			prevBlank, prevNL = false, true
		default:
			b = append(b, c)
			prevBlank, prevNL = false, false
		}
	}
	normalized := jsLower(jsTrim(string(b)))
	sum := sha256.Sum256(nodeUTF8(normalized))
	return hex.EncodeToString(sum[:])
}

// buildSemanticSourceText 对应 services/semantic.ts 的同名函数。
func buildSemanticSourceText(title, content string, transcript *string) string {
	parts := []string{jsTrim(title), jsTrim(content)}
	if transcript != nil && jsTrim(*transcript) != "" {
		parts = append(parts, jsTrim(*transcript))
	}
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n")
}

const semanticEligible = `
  i.deleted_at IS NULL
  AND (
    TRIM(i.title) != ''
    OR TRIM(i.content) != ''
    OR TRIM(COALESCE(i.transcript, '')) != ''
  )`

type pendingItem struct {
	ID, Title, Content string
	Transcript         *string
	ContentHash        string
}

func nullableString(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// resolvePending 对应 resolvePendingItems；limit<0 表示不限。
func (l *Library) resolvePending(ctx context.Context, model string, limit float64) ([]pendingItem, error) {
	stateRows, err := l.db.Query(ctx, `SELECT item_id, content_hash, model FROM knowledge_embeddings WHERE chunk_index = 0`)
	if err != nil {
		return nil, err
	}
	type state struct{ hash, model string }
	states := map[string]state{}
	for _, r := range stateRows.Values {
		states[r[0].(string)] = state{r[1].(string), r[2].(string)}
	}
	candRows, err := l.db.Query(ctx, `SELECT i.id
     FROM knowledge_items i
     LEFT JOIN (
       SELECT item_id, MIN(model) AS model, MAX(updated_at) AS updated_at
       FROM knowledge_embeddings GROUP BY item_id
     ) e ON e.item_id = i.id
     WHERE `+semanticEligible+`
       AND (e.item_id IS NULL OR e.model != ? OR i.updated_at >= e.updated_at)
     ORDER BY i.updated_at DESC`, model)
	if err != nil {
		return nil, err
	}
	var pending []pendingItem
	var upToDate []string
	for _, c := range candRows.Values {
		id := c[0].(string)
		rows, err := l.db.Query(ctx, "SELECT id, title, content, transcript FROM knowledge_items WHERE id = ?", id)
		if err != nil {
			return nil, err
		}
		if len(rows.Values) == 0 {
			continue
		}
		r := rows.Values[0]
		title, _ := r[1].(string)
		content, _ := r[2].(string)
		transcript := nullableString(r[3])
		hash := computeContentHash(buildSemanticSourceText(title, content, transcript))
		if st, ok := states[id]; ok && st.hash == hash && st.model == model {
			upToDate = append(upToDate, id)
			continue
		}
		pending = append(pending, pendingItem{id, title, content, transcript, hash})
		if limit >= 0 && float64(len(pending)) >= limit {
			break
		}
	}
	if err := l.touchIndexedAt(ctx, upToDate); err != nil {
		return nil, err
	}
	return pending, nil
}

func (l *Library) touchIndexedAt(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	args := []any{nowMs()}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: `UPDATE knowledge_embeddings SET updated_at = MAX(
         ?,
         COALESCE(
           (SELECT i.updated_at + 1 FROM knowledge_items i
             WHERE i.id = knowledge_embeddings.item_id),
           0
         )
       )
       WHERE item_id IN (` + strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ") + `)`, Args: args}})
	return err
}

func (l *Library) semanticStatus(ctx context.Context, model string) (any, error) {
	row, err := l.db.Query(ctx, `SELECT COUNT(*) AS c FROM knowledge_items i WHERE `+semanticEligible)
	if err != nil {
		return nil, err
	}
	eligible := asInt(row.Values[0][0])
	pending, err := l.resolvePending(ctx, model, -1)
	if err != nil {
		return nil, err
	}
	stats, err := l.db.Query(ctx, `SELECT COUNT(DISTINCT e.item_id) AS items, COUNT(*) AS chunks
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id AND i.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	indexed := eligible - int64(len(pending))
	if indexed < 0 {
		indexed = 0
	}
	l.sem.mu.Lock()
	defer l.sem.mu.Unlock()
	return map[string]any{
		"indexedItems": indexed, "eligibleItems": eligible, "totalChunks": asInt(stats.Values[0][1]),
		"lastSearchMs": l.sem.lastMs, "lastScannedChunks": l.sem.lastScanned, "lastSearchCacheHit": l.sem.lastHit,
	}, nil
}

func float32Blob(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(f))
	}
	return out
}

func (l *Library) semanticApply(ctx context.Context, input any) (bool, error) {
	invalid := errors.New("semantic:applyEmbeddings 载荷不合法")
	if !vTruthy(input) {
		return false, invalid
	}
	itemID, ok1 := jsGet(input, "itemId").(string)
	hash, ok2 := jsGet(input, "contentHash").(string)
	model, ok3 := jsGet(input, "model").(string)
	dimsF, ok4 := jsGet(input, "dims").(float64)
	chunks, ok5 := jsGet(input, "chunks").(jsArr)
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || dimsF != math.Trunc(dimsF) || math.IsInf(dimsF, 0) || dimsF <= 0 || len(chunks) == 0 {
		return false, invalid
	}
	type chunk struct {
		text   string
		vector []float32
	}
	var parsed []chunk
	for _, c := range chunks {
		if isNullish(c) {
			return false, errors.New("Cannot read properties of null (reading 'text')")
		}
		text, okText := jsGet(c, "text").(string)
		vec, okVec := jsGet(c, "vector").(jsArr)
		if !okText || !okVec || float64(len(vec)) != dimsF {
			return false, invalid
		}
		f32 := make([]float32, len(vec))
		for i, e := range vec {
			f32[i] = float32(jsToNumber(e))
		}
		parsed = append(parsed, chunk{text, f32})
	}
	rows, err := l.db.Query(ctx, "SELECT title,content,transcript FROM knowledge_items WHERE id=? AND deleted_at IS NULL", itemID)
	if err != nil || len(rows.Values) == 0 {
		return false, nil
	}
	r := rows.Values[0]
	title, _ := r[0].(string)
	content, _ := r[1].(string)
	if computeContentHash(buildSemanticSourceText(title, content, nullableString(r[2]))) != hash {
		return false, nil
	}
	l.writeMu.Lock()
	stmts := []sqlite.Statement{{SQL: "DELETE FROM knowledge_embeddings WHERE item_id = ?", Args: []any{itemID}}}
	now := nowMs()
	for i, c := range parsed {
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO knowledge_embeddings
             (item_id, chunk_index, chunk_text, content_hash, model, dims, vector, updated_at)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{itemID, int64(i), c.text, hash, model, int64(dimsF), float32Blob(c.vector), now},
		})
	}
	_, err = l.db.Transaction(ctx, stmts)
	l.writeMu.Unlock()
	if err != nil {
		// 条目在嵌入期间被彻底删除等（外键失败）：跳过
		return false, nil
	}
	l.sem.invalidate(model, false)
	return true, nil
}

// loadSemanticCache 对应 iterateSemanticVectorCache：按 rowid 顺序读全部分块，维度不一致的跳过。
func (l *Library) loadSemanticCache(ctx context.Context, model string) (*semanticCache, error) {
	rows, err := l.db.Query(ctx, `SELECT e.rowid AS rowid, e.item_id, e.chunk_index, e.dims, e.vector
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id AND i.deleted_at IS NULL
       WHERE e.model = ? AND e.rowid > ?
       ORDER BY e.rowid`, model, int64(0))
	if err != nil {
		return nil, err
	}
	c := &semanticCache{}
	for _, r := range rows.Values {
		dims := int(asInt(r[3]))
		blob, _ := r[4].([]byte)
		if dims < 0 || len(blob) < dims*4 {
			return nil, errors.New("Invalid typed array length: " + jsNumberString(float64(dims)))
		}
		if dims == 0 {
			continue
		}
		if c.dims == 0 {
			c.dims = dims
		}
		if dims != c.dims {
			continue
		}
		for d := 0; d < dims; d++ {
			c.vectors = append(c.vectors, math.Float32frombits(binary.LittleEndian.Uint32(blob[4*d:])))
		}
		c.itemIDs = append(c.itemIDs, r[1].(string))
		c.chunkIndexs = append(c.chunkIndexs, asInt(r[2]))
	}
	return c, nil
}

// utf16Prefix 按 UTF-16 码元取前 n 个，孤立代理项原样保留（WTF-8）。
func utf16Prefix(s string, n int) string {
	u := toUTF16(s)
	if len(u) <= n {
		return s
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		c := u[i]
		if c >= 0xD800 && c <= 0xDBFF && i+1 < n && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			b.WriteRune(0x10000 + (rune(c)-0xD800)<<10 + (rune(u[i+1]) - 0xDC00))
			i++
			continue
		}
		b.WriteString(unitToString(c))
	}
	return b.String()
}

func (l *Library) semanticSearch(ctx context.Context, model string, query []float32, limit float64) (any, error) {
	started := time.Now()
	l.sem.mu.Lock()
	cache := l.sem.caches[model]
	hit := cache != nil
	l.sem.mu.Unlock()
	if cache == nil {
		built, err := l.loadSemanticCache(ctx, model)
		if err != nil {
			return nil, err
		}
		cache = built
		l.sem.mu.Lock()
		if l.sem.caches == nil {
			l.sem.caches = map[string]*semanticCache{}
		}
		l.sem.caches[model] = cache
		l.sem.mu.Unlock()
	}
	scanned := len(cache.itemIDs)

	type scored struct {
		id    string
		chunk int64
		score float64
	}
	var top []scored
	if dims := len(query); dims != 0 && cache.dims == dims {
		best := map[string]*scored{}
		var order []*scored
		for i := range cache.itemIDs {
			dot := 0.0
			off := i * dims
			for d := 0; d < dims; d++ {
				dot += float64(query[d]) * float64(cache.vectors[off+d])
			}
			id := cache.itemIDs[i]
			if cur, ok := best[id]; !ok {
				s := &scored{id, cache.chunkIndexs[i], dot}
				best[id] = s
				order = append(order, s)
			} else if dot > cur.score {
				cur.chunk, cur.score = cache.chunkIndexs[i], dot
			}
		}
		for _, s := range order {
			top = append(top, *s)
		}
		sort.SliceStable(top, func(i, j int) bool {
			if top[i].score != top[j].score {
				return top[i].score > top[j].score
			}
			return top[i].id < top[j].id
		})
		// 堆容量按「长度 < limit」填充，limit 为小数时会多留一个
		keep := int(math.Ceil(math.Max(1, limit)))
		if len(top) > keep {
			top = top[:keep]
		}
	}

	type snippet struct {
		text, title string
		status      any
		reasons     any
	}
	snippets := map[string]snippet{}
	if len(top) > 0 {
		var args []any
		holders := make([]string, len(top))
		for i, t := range top {
			holders[i] = "(?, ?)"
			args = append(args, t.id, t.chunk)
		}
		rows, err := l.db.Query(ctx, `SELECT e.item_id, e.chunk_index, e.chunk_text, i.title AS title,i.review_status,i.review_reasons
       FROM knowledge_embeddings e
       JOIN knowledge_items i ON i.id = e.item_id
       WHERE i.deleted_at IS NULL AND (e.item_id, e.chunk_index) IN (VALUES `+strings.Join(holders, ", ")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for _, r := range rows.Values {
			reasonsText, _ := r[5].(string)
			if reasonsText == "" {
				reasonsText = "[]"
			}
			parsedReasons, err := jsParse(reasonsText)
			if err != nil {
				return nil, errors.New("Unexpected token in JSON: " + err.Error())
			}
			text, _ := r[2].(string)
			title, _ := r[3].(string)
			snippets[r[0].(string)+":"+jsNumberString(float64(asInt(r[1])))] = snippet{text, title, r[4], parsedReasons}
		}
	}
	hits := jsArr{}
	for _, t := range top {
		sn, ok := snippets[t.id+":"+jsNumberString(float64(t.chunk))]
		if !ok {
			continue
		}
		text := jsTrim(jsCollapseSpaces(sn.text))
		if utf16Len(text) > semanticSnippetMax {
			text = utf16Prefix(text, semanticSnippetMax) + "…"
		}
		o := newObj()
		o.set("itemId", t.id)
		o.set("title", sn.title)
		if sn.status == nil {
			o.set("reviewStatus", jsNull)
		} else {
			o.set("reviewStatus", sn.status)
		}
		o.set("reviewReasons", sn.reasons)
		o.set("snippet", text)
		o.set("score", t.score)
		hits = append(hits, o)
	}
	l.sem.mu.Lock()
	l.sem.lastMs, l.sem.lastScanned, l.sem.lastHit = time.Since(started).Milliseconds(), int64(scanned), hit
	l.sem.mu.Unlock()
	// 经 jsStringify 输出：孤立代理项写成 \udxxx、NaN/Infinity 写成 null，与 JSON.stringify 一致
	s, _ := jsStringify(hits)
	return json.RawMessage(s), nil
}

func jsArg(a []json.RawMessage, i int) any {
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

// numOr 对应 `Number(v) || fallback`。
func numOr(v any, fallback float64) float64 {
	n := jsToNumber(v)
	if math.IsNaN(n) || n == 0 {
		return fallback
	}
	return n
}

func (l *Library) registerSemantic(b *Bridge) {
	b.handlers["semantic:status"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		model, _ := jsArg(a, 0).(string)
		return l.semanticStatus(ctx, model)
	}
	b.handlers["semantic:listPending"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		p := jsArg(a, 0)
		model, _ := jsOptGet(p, "model").(string)
		limit := math.Min(math.Max(1, numOr(jsOptGet(p, "limit"), 10)), semanticPendingBatchMax)
		items, err := l.resolvePending(ctx, model, limit)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			var transcript any
			if it.Transcript != nil {
				transcript = *it.Transcript
			}
			out = append(out, map[string]any{"id": it.ID, "title": it.Title, "content": it.Content, "transcript": transcript, "contentHash": it.ContentHash})
		}
		return out, nil
	}
	b.handlers["semantic:applyEmbeddings"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.semanticApply(ctx, jsArg(a, 0))
	}
	b.handlers["semantic:search"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		p := jsArg(a, 0)
		model, _ := jsOptGet(p, "model").(string)
		vec, _ := jsOptGet(p, "vector").(jsArr)
		if model == "" || len(vec) == 0 {
			return []any{}, nil
		}
		limit := math.Min(math.Max(1, numOr(jsOptGet(p, "limit"), 5)), semanticSearchLimitMax)
		q := make([]float32, len(vec))
		for i, e := range vec {
			q[i] = float32(jsToNumber(e))
		}
		return l.semanticSearch(ctx, model, q, limit)
	}
	b.handlers["semantic:clear"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		l.writeMu.Lock()
		res, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "DELETE FROM knowledge_embeddings"}})
		l.writeMu.Unlock()
		if err != nil {
			return nil, err
		}
		l.sem.invalidate("", true)
		return res[0].Changes, nil
	}
}
