package main

// 对应 packages/db/src/{collection,tag}.ts 的写入，及 knowledge.ipc.ts 里的
// collection:create/update/delete、tag:create/update/delete 六个频道。
// 由 write_test.go 用 TS DAO 生成的基准逐表对拍（见 spikes/mygo-db/make-golden.mts 的 collection.* / tag.* 场景）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

var tagColorKeys = map[string]bool{
	"red": true, "orange": true, "amber": true, "green": true, "teal": true,
	"blue": true, "indigo": true, "purple": true, "pink": true, "gray": true,
}

// normalizeColorKey 对应 tag.ts：不在白名单（含 null、非字符串）一律退回 gray。
func normalizeColorKey(o optString) string {
	if o.Set && !o.Null && tagColorKeys[o.Val] {
		return o.Val
	}
	return "gray"
}

// reqString 对应 TS 里直接调用 `input.name.trim()` 的必填字符串：缺失、null、非字符串都会抛错。
type reqString struct {
	Valid bool
	Val   string
}

func (r *reqString) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		r.Valid, r.Val = true, s
	}
	return nil // 非字符串不在这里报错，由调用方按 TS 的行为抛
}

// optNumber 区分未传 / null / 数值。
type optNumber struct {
	Set, Null bool
	Val       any // int64 或 float64
}

func (o *optNumber) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		// 字符串等非数值：TS 会原样写进 SQLite；这里同样保持原值
		var s string
		if json.Unmarshal(b, &s) == nil {
			o.Val = s
			return nil
		}
		return err
	}
	if f == float64(int64(f)) {
		o.Val = int64(f)
	} else {
		o.Val = f
	}
	return nil
}

func (o optNumber) sql(existing any) any {
	if !o.Set {
		return existing
	}
	if o.Null {
		return nil
	}
	return o.Val
}

type CollectionCreateInput struct {
	Name reqString `json:"name"`
	Icon optString `json:"icon"`
}

type CollectionUpdateInput struct {
	Name      optString `json:"name"`
	Icon      optString `json:"icon"`
	SortOrder optNumber `json:"sortOrder"`
}

type TagCreateInput struct {
	Name     reqString `json:"name"`
	ColorKey optString `json:"colorKey"`
}

type TagUpdateInput struct {
	Name     optString `json:"name"`
	ColorKey optString `json:"colorKey"`
}

func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

func (l *Library) collectionGet(ctx context.Context, id string) (any, error) {
	rows, err := l.db.Query(ctx, `SELECT id, name, icon, sort_order, created_at, updated_at FROM collections WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	r := rows.Values[0]
	// get() 不带 itemCount；JSON 里 undefined 会被丢掉，所以这里也不放该键
	return map[string]any{"id": r[0], "name": r[1], "icon": r[2], "sortOrder": r[3], "createdAt": r[4], "updatedAt": r[5]}, nil
}

func (l *Library) collectionCreate(ctx context.Context, in CollectionCreateInput) (any, error) {
	if !in.Name.Valid {
		return nil, errors.New("集合名称无效")
	}
	name := jsTrim(in.Name.Val)
	if name == "" {
		return nil, errors.New("集合名称不能为空")
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	now, id := nowMs(), newUUID()
	rows, err := l.db.Query(ctx, `SELECT COALESCE(MAX(sort_order), -1) AS max_order FROM collections`)
	if err != nil {
		return nil, err
	}
	maxOrder := int64(-1)
	if len(rows.Values) > 0 {
		maxOrder = asInt(rows.Values[0][0])
	}
	if _, err := l.db.Transaction(ctx, []sqlite.Statement{{
		SQL:  "INSERT INTO collections (id, name, icon, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		Args: []any{id, name, in.Icon.sql(nil), maxOrder + 1, now, now},
	}}); err != nil {
		return nil, err
	}
	return l.collectionGet(ctx, id)
}

func (l *Library) collectionUpdate(ctx context.Context, id string, in CollectionUpdateInput) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	rows, err := l.db.Query(ctx, `SELECT name, icon, sort_order FROM collections WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	cur := rows.Values[0]
	name := cur[0].(string)
	if in.Name.Set {
		if in.Name.Null {
			return nil, errors.New("集合名称无效") // null.trim()
		}
		name = jsTrim(in.Name.Val)
	}
	if name == "" {
		return nil, errors.New("集合名称不能为空")
	}
	if _, err := l.db.Transaction(ctx, []sqlite.Statement{{
		SQL:  "UPDATE collections SET name = ?, icon = ?, sort_order = ?, updated_at = ? WHERE id = ?",
		Args: []any{name, in.Icon.sql(cur[1]), in.SortOrder.sql(cur[2]), nowMs(), id},
	}}); err != nil {
		return nil, err
	}
	return l.collectionGet(ctx, id)
}

func (l *Library) collectionDelete(ctx context.Context, id string) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	res, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "DELETE FROM collections WHERE id = ?", Args: []any{id}}})
	if err != nil {
		return nil, err
	}
	return res[0].Changes > 0, nil
}

func (l *Library) tagGet(ctx context.Context, id string) (any, error) {
	rows, err := l.db.Query(ctx, `SELECT id, name, color_key, created_at, updated_at FROM tags WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	r := rows.Values[0]
	return map[string]any{"id": r[0], "name": r[1], "colorKey": r[2], "createdAt": r[3], "updatedAt": r[4]}, nil
}

func (l *Library) tagCreate(ctx context.Context, in TagCreateInput) (any, error) {
	if !in.Name.Valid {
		return nil, errors.New("标签名称无效")
	}
	name := jsTrim(in.Name.Val)
	if name == "" {
		return nil, errors.New("标签名称不能为空")
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	rows, err := l.db.Query(ctx, `SELECT id FROM tags WHERE LOWER(name) = LOWER(?)`, name)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) > 0 {
		return l.tagGet(ctx, rows.Values[0][0].(string))
	}
	now, id := nowMs(), newUUID()
	if _, err := l.db.Transaction(ctx, []sqlite.Statement{{
		SQL:  "INSERT INTO tags (id, name, color_key, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		Args: []any{id, name, normalizeColorKey(in.ColorKey), now, now},
	}}); err != nil {
		return nil, err
	}
	return l.tagGet(ctx, id)
}

func (l *Library) tagUpdate(ctx context.Context, id string, in TagUpdateInput) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	rows, err := l.db.Query(ctx, `SELECT name, color_key FROM tags WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows.Values) == 0 {
		return nil, nil
	}
	cur := rows.Values[0]
	name := cur[0].(string)
	if in.Name.Set {
		if in.Name.Null {
			return nil, errors.New("标签名称无效")
		}
		name = jsTrim(in.Name.Val)
	}
	if name == "" {
		return nil, errors.New("标签名称不能为空")
	}
	conflict, err := l.db.Query(ctx, `SELECT id FROM tags WHERE LOWER(name) = LOWER(?) AND id != ?`, name, id)
	if err != nil {
		return nil, err
	}
	if len(conflict.Values) > 0 {
		return nil, errors.New("标签「" + name + "」已存在")
	}
	color := cur[1]
	if in.ColorKey.Set {
		color = normalizeColorKey(in.ColorKey)
	}
	if _, err := l.db.Transaction(ctx, []sqlite.Statement{{
		SQL:  "UPDATE tags SET name = ?, color_key = ?, updated_at = ? WHERE id = ?",
		Args: []any{name, color, nowMs(), id},
	}}); err != nil {
		return nil, err
	}
	return l.tagGet(ctx, id)
}

func (l *Library) tagDelete(ctx context.Context, id string) (any, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	res, err := l.db.Transaction(ctx, []sqlite.Statement{{SQL: "DELETE FROM tags WHERE id = ?", Args: []any{id}}})
	if err != nil {
		return nil, err
	}
	return res[0].Changes > 0, nil
}

func (l *Library) registerCatalogWrite(b *Bridge) {
	decode := func(raw json.RawMessage, v any) error {
		if string(raw) == "null" || len(raw) == 0 {
			return nil
		}
		return json.Unmarshal(raw, v)
	}
	idArg := func(a []json.RawMessage) string {
		var id string
		_ = json.Unmarshal(argAt(a, 0), &id)
		return id
	}
	b.handlers["collection:create"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var in CollectionCreateInput
		if err := decode(argAt(a, 0), &in); err != nil {
			return nil, err
		}
		return l.collectionCreate(ctx, in)
	}
	b.handlers["collection:update"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var in CollectionUpdateInput
		if err := decode(argAt(a, 1), &in); err != nil {
			return nil, err
		}
		return l.collectionUpdate(ctx, idArg(a), in)
	}
	b.handlers["collection:delete"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.collectionDelete(ctx, idArg(a))
	}
	b.handlers["tag:create"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var in TagCreateInput
		if err := decode(argAt(a, 0), &in); err != nil {
			return nil, err
		}
		return l.tagCreate(ctx, in)
	}
	b.handlers["tag:update"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		var in TagUpdateInput
		if err := decode(argAt(a, 1), &in); err != nil {
			return nil, err
		}
		return l.tagUpdate(ctx, idArg(a), in)
	}
	b.handlers["tag:delete"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.tagDelete(ctx, idArg(a))
	}
}
