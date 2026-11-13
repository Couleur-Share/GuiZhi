package main

// 对应 packages/db/src/knowledge.ts 的 KnowledgeItemDB.list 与 knowledge:list 频道：
// 范围/集合/标签/平台/可访问范围/排除/全文检索（bm25）、置顶优先的排序、基于游标的翻页、摘要。
// 与 TS 的差别（有意为之）：不做 5 秒总数缓存——那是 TS 侧为减少重复 COUNT 加的优化，不影响结果。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ListQuery 对应 KnowledgeItemQuery。可选字段用指针，以区分「未传」与「空串」（指纹要区分 null 与 ""）。
type ListQuery struct {
	ExcludedItemIDs []string `json:"excludedItemIds"`
	Scope           *string  `json:"scope"`
	CollectionID    *string  `json:"collectionId"`
	TagID           *string  `json:"tagId"`
	Platform        *string  `json:"platform"`
	CollectionScope *struct {
		IDs                  []string `json:"ids"`
		IncludeUncategorized bool     `json:"includeUncategorized"`
	} `json:"collectionScope"`
	Search          string   `json:"search"`
	SearchMode      *string  `json:"searchMode"`
	IncludeArchived bool     `json:"includeArchived"`
	SortBy          *string  `json:"sortBy"`
	SortOrder       *string  `json:"sortOrder"`
	Limit           *float64 `json:"limit"`
	Cursor          *string  `json:"cursor"`
	Offset          float64  `json:"offset"`
}

func deref(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// jsonString 与 JSON.stringify 对字符串的输出一致（不转义 <>& 与 U+2028/2029）。
func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	out := strings.TrimSuffix(buf.String(), "\n")
	out = strings.ReplaceAll(out, `\u2028`, "\u2028")
	return strings.ReplaceAll(out, `\u2029`, "\u2029")
}

func jsonNullable(p *string) string {
	if p == nil {
		return "null"
	}
	return jsonString(*p)
}

// fingerprint 复刻 queryFingerprint 的 JSON.stringify（键序固定，undefined 省略）。
func (q ListQuery) fingerprint() string {
	var b strings.Builder
	b.WriteString("{")
	first := true
	add := func(k, v string) {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(jsonString(k) + ":" + v)
	}
	if q.Scope != nil {
		add("scope", jsonString(*q.Scope))
	}
	add("collectionId", jsonNullable(q.CollectionID))
	add("tagId", jsonNullable(q.TagID))
	add("platform", jsonNullable(q.Platform))
	if q.ExcludedItemIDs != nil {
		parts := make([]string, len(q.ExcludedItemIDs))
		for i, id := range q.ExcludedItemIDs {
			parts[i] = jsonString(id)
		}
		add("excludedItemIds", "["+strings.Join(parts, ",")+"]")
	}
	if q.CollectionScope == nil {
		add("collectionScope", "null")
	} else {
		ids := append([]string{}, q.CollectionScope.IDs...)
		sort.Strings(ids) // JS 默认按 UTF-16 码元排序；uuid 都是 ASCII，与字节序一致
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = jsonString(id)
		}
		add("collectionScope", fmt.Sprintf(`{"ids":[%s],"includeUncategorized":%t}`, strings.Join(parts, ","), q.CollectionScope.IncludeUncategorized))
	}
	add("search", jsonString(strings.TrimFunc(q.Search, jsSpace)))
	add("searchMode", jsonString(deref(q.SearchMode, "phrase")))
	add("includeArchived", fmt.Sprintf("%t", q.IncludeArchived))
	add("sortBy", jsonString(deref(q.SortBy, "updatedAt")))
	add("sortOrder", jsonString(deref(q.SortOrder, "desc")))
	b.WriteString("}")
	return b.String()
}

type listCursor struct {
	Fingerprint string   `json:"fingerprint"`
	Pinned      *int64   `json:"pinned,omitempty"`
	SortValue   any      `json:"sortValue,omitempty"`
	Rank        *float64 `json:"rank,omitempty"`
	UpdatedAt   int64    `json:"updatedAt"`
	ID          string   `json:"id"`
}

// encodeCursor 的键序与 TS 一致：搜索态 {fingerprint, rank, updatedAt, id}，其余 {fingerprint, pinned, sortValue, updatedAt, id}。
func encodeCursor(c listCursor, search bool) string {
	var b strings.Builder
	b.WriteString(`{"fingerprint":` + jsonString(c.Fingerprint))
	if search {
		rank, _ := json.Marshal(*c.Rank)
		b.WriteString(`,"rank":` + string(rank))
	} else {
		sv, _ := json.Marshal(c.SortValue)
		if s, ok := c.SortValue.(string); ok {
			sv = []byte(jsonString(s))
		}
		b.WriteString(fmt.Sprintf(`,"pinned":%d,"sortValue":%s`, *c.Pinned, sv))
	}
	b.WriteString(fmt.Sprintf(`,"updatedAt":%d,"id":%s}`, c.UpdatedAt, jsonString(c.ID)))
	return base64.RawURLEncoding.EncodeToString([]byte(b.String()))
}

var errBadCursor = errors.New("知识列表游标无效或已过期")

func decodeCursor(value, fingerprint string) (listCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, errBadCursor
	}
	var c listCursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Fingerprint != fingerprint || c.ID == "" {
		return listCursor{}, errBadCursor
	}
	return c, nil
}

var sortColumns = map[string]string{
	"updatedAt": "i.updated_at",
	"createdAt": "i.created_at",
	"title":     "i.title COLLATE NOCASE",
}

func placeholders(n int, sep string) string {
	return strings.TrimSuffix(strings.Repeat("?"+sep, n), sep)
}

func anySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func (l *Library) knowledgeList(ctx context.Context, q ListQuery) (any, error) {
	var cond []string
	var params []any

	switch deref(q.Scope, "all") {
	case "uncategorized":
		cond = append(cond, "i.deleted_at IS NULL", "i.collection_id IS NULL", "i.status != 'archived'")
	case "favorites":
		cond = append(cond, "i.deleted_at IS NULL", "i.is_favorite = 1")
	case "archived":
		cond = append(cond, "i.deleted_at IS NULL", "i.status = 'archived'")
	case "trash":
		cond = append(cond, "i.deleted_at IS NOT NULL")
	default:
		cond = append(cond, "i.deleted_at IS NULL")
		if !q.IncludeArchived {
			cond = append(cond, "i.status != 'archived'")
		}
	}
	if len(q.ExcludedItemIDs) > 0 {
		cond = append(cond, "i.id NOT IN ("+placeholders(len(q.ExcludedItemIDs), ",")+")")
		params = append(params, anySlice(q.ExcludedItemIDs)...)
	}
	if deref(q.CollectionID, "") != "" {
		cond = append(cond, "i.collection_id = ?")
		params = append(params, *q.CollectionID)
	}
	if q.CollectionScope != nil {
		var branches []string
		if len(q.CollectionScope.IDs) > 0 {
			branches = append(branches, "i.collection_id IN ("+placeholders(len(q.CollectionScope.IDs), ", ")+")")
			params = append(params, anySlice(q.CollectionScope.IDs)...)
		}
		if q.CollectionScope.IncludeUncategorized {
			branches = append(branches, "i.collection_id IS NULL")
		}
		if len(branches) > 0 {
			cond = append(cond, "("+strings.Join(branches, " OR ")+")")
		} else {
			cond = append(cond, "0") // 一个都没选就是「一条都不给看」
		}
	}
	if deref(q.TagID, "") != "" {
		cond = append(cond, "EXISTS (SELECT 1 FROM knowledge_item_tags kit WHERE kit.item_id = i.id AND kit.tag_id = ?)")
		params = append(params, *q.TagID)
	}
	platform := deref(q.Platform, "")
	if platform != "" {
		cond = append(cond, "EXISTS (SELECT 1 FROM source_records sr WHERE sr.item_id = i.id AND sr.platform = ?)")
		params = append(params, platform)
	}

	searchTerm := strings.TrimFunc(q.Search, jsSpace)
	var match string
	var hasMatch bool
	if searchTerm != "" {
		mode := FtsMatchMode(deref(q.SearchMode, "phrase"))
		if mode != FtsRecall {
			mode = FtsPhrase
		}
		match, hasMatch = BuildFtsMatchQuery(searchTerm, mode)
		if !hasMatch {
			// 编译不出可检索内容是「没有命中」，不是「没有搜索」
			return map[string]any{"entries": []any{}, "total": 0, "nextCursor": nil}, nil
		}
	}

	sortBy := deref(q.SortBy, "updatedAt")
	sortColumn, ok := sortColumns[sortBy]
	if !ok {
		sortColumn = sortColumns["updatedAt"]
	}
	direction := "DESC"
	comparator := "<"
	if deref(q.SortOrder, "desc") == "asc" {
		direction, comparator = "ASC", ">"
	}
	join := ""
	order := fmt.Sprintf("ORDER BY i.is_pinned DESC, %s %s, i.id %s", sortColumn, direction, direction)
	if hasMatch {
		join = `JOIN (
        SELECT item_id, bm25(knowledge_fts, 0.0, 10.0, 1.0, 5.0) AS fts_rank
        FROM knowledge_fts
        WHERE knowledge_fts MATCH ?
      ) f ON f.item_id = i.id`
		order = "ORDER BY f.fts_rank ASC, i.updated_at DESC, i.id DESC"
		params = append([]any{match}, params...)
	}
	where := "WHERE " + strings.Join(cond, " AND ")

	limit := 200.0
	if q.Limit != nil {
		limit = *q.Limit
	}
	if limit > 500 {
		limit = 500
	}
	if limit < 1 {
		limit = 1
	}
	offset := 0.0
	if q.Cursor == nil || *q.Cursor == "" {
		offset = q.Offset
		if offset < 0 {
			offset = 0
		}
	}
	fp := q.fingerprint()

	totalRows, err := l.db.Query(ctx, "SELECT COUNT(*) AS count FROM knowledge_items i "+join+" "+where, params...)
	if err != nil {
		return nil, err
	}
	total := asInt(totalRows.Values[0][0])

	projection := `(SELECT s.platform FROM source_records s
          WHERE s.item_id = i.id
          ORDER BY s.captured_at DESC LIMIT 1)`
	var projectionParams []any
	if platform != "" {
		projection = `(SELECT s.platform FROM source_records s
          WHERE s.item_id = i.id AND s.platform = ?
          ORDER BY s.captured_at DESC LIMIT 1)`
		projectionParams = []any{platform}
	}

	pageWhere := where
	pageParams := append([]any{}, params...)
	if q.Cursor != nil && *q.Cursor != "" {
		cur, err := decodeCursor(*q.Cursor, fp)
		if err != nil {
			return nil, err
		}
		if hasMatch {
			if cur.Rank == nil {
				return nil, errors.New("知识列表搜索游标无效")
			}
			pageWhere += ` AND (
          f.fts_rank > ? OR (f.fts_rank = ? AND (
            i.updated_at < ? OR (i.updated_at = ? AND i.id < ?)
          ))
        )`
			pageParams = append(pageParams, *cur.Rank, *cur.Rank, cur.UpdatedAt, cur.UpdatedAt, cur.ID)
		} else {
			if cur.Pinned == nil || cur.SortValue == nil {
				return nil, errors.New("知识列表排序游标无效")
			}
			pageWhere += fmt.Sprintf(` AND (
          i.is_pinned < ? OR (i.is_pinned = ? AND (
            %s %s ? OR (%s = ? AND i.id %s ?)
          ))
        )`, sortColumn, comparator, sortColumn, comparator)
			pageParams = append(pageParams, *cur.Pinned, *cur.Pinned, cur.SortValue, cur.SortValue, cur.ID)
		}
	}

	sourceLen := snippetSourceLength
	rankCol := "NULL"
	if hasMatch {
		sourceLen = searchSourceLength
		rankCol = "f.fts_rank"
	}
	sql := fmt.Sprintf(`SELECT i.id, i.title, i.item_type, i.status, i.collection_id,
              i.is_favorite, i.is_pinned, i.deleted_at, i.created_at, i.updated_at, i.review_status, i.review_reasons,
              substr(i.content, 1, %d) AS content,
              %s AS platform,
              %s AS fts_rank,
              %s AS sort_value
       FROM knowledge_items i %s %s %s LIMIT ? OFFSET ?`, sourceLen, projection, rankCol, sortColumn, join, pageWhere, order)
	args := append(append(append([]any{}, projectionParams...), pageParams...), int64(limit)+1, int64(offset))
	rows, err := l.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	values := rows.Values
	hasMore := len(values) > int(limit)
	if hasMore {
		values = values[:len(values)-1]
	}
	ids := make([]string, len(values))
	for i, r := range values {
		ids[i], _ = r[0].(string)
	}
	tagsByItem, err := l.loadTagsFor(ctx, ids)
	if err != nil {
		return nil, err
	}

	entries := make([]map[string]any, 0, len(values))
	for _, r := range values {
		id, _ := r[0].(string)
		content, _ := r[12].(string)
		snippet := makeSnippet(content)
		if hasMatch {
			snippet = makeSearchSnippet(content, searchTerm)
		}
		reviewStatus := r[10]
		if reviewStatus == nil {
			reviewStatus = "clear"
		}
		tags := tagsByItem[id]
		if tags == nil {
			tags = []map[string]any{}
		}
		entries = append(entries, map[string]any{
			"id": id, "title": r[1], "snippet": snippet,
			"reviewStatus": reviewStatus, "reviewReasons": parseReviewReasons(r[11]),
			"itemType": r[2], "status": r[3], "collectionId": r[4],
			"isFavorite": asInt(r[5]) == 1, "isPinned": asInt(r[6]) == 1,
			"platform": r[13], "deletedAt": r[7], "createdAt": r[8], "updatedAt": r[9],
			"tags": tags,
		})
	}

	var nextCursor any
	if hasMore && len(values) > 0 {
		last := values[len(values)-1]
		id, _ := last[0].(string)
		c := listCursor{Fingerprint: fp, UpdatedAt: asInt(last[9]), ID: id}
		if hasMatch {
			rank, _ := last[14].(float64)
			c.Rank = &rank
		} else {
			pinned := asInt(last[6])
			c.Pinned = &pinned
			c.SortValue = last[15]
		}
		nextCursor = encodeCursor(c, hasMatch)
	}
	return map[string]any{"entries": entries, "total": total, "nextCursor": nextCursor}, nil
}

func parseReviewReasons(v any) []string {
	raw, _ := v.(string)
	if raw == "" {
		return []string{}
	}
	var arr []any
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return []string{}
	}
	out := []string{}
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (l *Library) loadTagsFor(ctx context.Context, ids []string) (map[string][]map[string]any, error) {
	result := map[string][]map[string]any{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := l.db.Query(ctx, `SELECT kit.item_id AS item_id, t.id, t.name, t.color_key, t.created_at, t.updated_at
       FROM knowledge_item_tags kit
       JOIN tags t ON t.id = kit.tag_id
       WHERE kit.item_id IN (`+placeholders(len(ids), ", ")+`)
       ORDER BY t.name`, anySlice(ids)...)
	if err != nil {
		return nil, err
	}
	for _, r := range rows.Values {
		itemID, _ := r[0].(string)
		result[itemID] = append(result[itemID], map[string]any{
			"id": r[1], "name": r[2], "colorKey": r[3], "createdAt": r[4], "updatedAt": r[5],
		})
	}
	return result, nil
}

func (l *Library) registerList(b *Bridge) {
	b.handlers["knowledge:list"] = func(ctx context.Context, args []json.RawMessage) (any, error) {
		var q ListQuery
		if len(args) > 0 {
			if err := json.Unmarshal(args[0], &q); err != nil {
				return nil, err
			}
		}
		return l.knowledgeList(ctx, q)
	}
}
